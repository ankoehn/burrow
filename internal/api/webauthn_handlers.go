// webauthn_handlers.go — passkey dashboard login JSON API (spec Part K.2).
//
//	register/begin   POST /api/v1/auth/webauthn/register/begin   (session-authed)
//	register/finish  POST /api/v1/auth/webauthn/register/finish  (session-authed)
//	credentials      GET    /api/v1/auth/webauthn/credentials    (session-authed)
//	credentials/{id} DELETE /api/v1/auth/webauthn/credentials/{id}
//	login/begin      POST /api/v1/auth/webauthn/login/begin      (public)
//	login/finish     POST /api/v1/auth/webauthn/login/finish     (public, sets cookie)
//
// The provider in internal/webauthn owns the in-process challenge map and
// the wraps around the go-webauthn library; this file is the thin HTTP
// adapter. The login/finish handler sets the burrow_session cookie via the
// same code path as password login so the dashboard treats both identically.

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ankoehn/burrow/internal/db"
	bwebauthn "github.com/ankoehn/burrow/internal/webauthn"
)

// WebAuthnProvider is the surface api/webauthn_handlers.go consumes. The
// concrete *webauthn.Provider in internal/webauthn satisfies it. Keeping
// this an interface (rather than importing the concrete type) means the
// api package compiles even when WebAuthn is not wired (Deps.WebAuthn nil →
// the four routes degrade to 503).
type WebAuthnProvider interface {
	BeginRegister(ctx context.Context, userID string) (*bwebauthn.BeginRegisterResult, error)
	FinishRegister(ctx context.Context, userID, sessionID, label string, r *http.Request) (*db.WebAuthnCredential, error)
	BeginLogin(ctx context.Context, email string) (*bwebauthn.BeginLoginResult, error)
	FinishLogin(ctx context.Context, sessionID string, r *http.Request) (string, error)
	ListCredentialsForUser(ctx context.Context, userID string) ([]db.WebAuthnCredential, error)
	DeleteCredential(ctx context.Context, callerID, credID string) error
}

// --- Wire shapes ---------------------------------------------------------

type webauthnBeginResp struct {
	SessionID string `json:"session_id"`
	Options   any    `json:"options"`
}

type webauthnRegisterFinishReq struct {
	SessionID string          `json:"session_id"`
	Label     string          `json:"label"`
	Response  json.RawMessage `json:"response"`
}

type webauthnLoginBeginReq struct {
	Email string `json:"email"`
}

type webauthnLoginFinishReq struct {
	SessionID string          `json:"session_id"`
	Response  json.RawMessage `json:"response"`
}

type webauthnCredentialResp struct {
	ID        string     `json:"id"`
	Label     string     `json:"label"`
	CreatedAt time.Time  `json:"created_at"`
	LastUsed  *time.Time `json:"last_used,omitempty"`
}

// --- Helpers --------------------------------------------------------------

// rewriteBodyForFinish replaces r.Body with a fresh reader over the inner
// "response" field of the request body. The go-webauthn library's
// FinishRegister/FinishLogin both call protocol.ParseCredentialCreation/
// AssertionResponse(r), which reads r.Body — so the body needs to be the
// raw attestation/assertion bytes the browser produced.
func rewriteBodyForFinish(r *http.Request, raw json.RawMessage) {
	r.Body = http.NoBody
	if len(raw) == 0 {
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	r.ContentLength = int64(len(raw))
}

// --- Handlers -------------------------------------------------------------

// PostWebAuthnRegisterBegin handles POST /api/v1/auth/webauthn/register/begin.
func (d Deps) PostWebAuthnRegisterBegin(w http.ResponseWriter, r *http.Request) {
	if d.WebAuthn == nil {
		writeErr(w, http.StatusServiceUnavailable, "webauthn unavailable")
		return
	}
	uid := userID(r.Context())
	if uid == "" {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	res, err := d.WebAuthn.BeginRegister(r.Context(), uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "register begin failed")
		return
	}
	writeJSON(w, http.StatusOK, webauthnBeginResp{
		SessionID: res.SessionID,
		Options:   res.Options,
	})
}

// PostWebAuthnRegisterFinish handles POST /api/v1/auth/webauthn/register/finish.
func (d Deps) PostWebAuthnRegisterFinish(w http.ResponseWriter, r *http.Request) {
	if d.WebAuthn == nil {
		writeErr(w, http.StatusServiceUnavailable, "webauthn unavailable")
		return
	}
	uid := userID(r.Context())
	if uid == "" {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32*1024)
	var in webauthnRegisterFinishReq
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if in.SessionID == "" || len(in.Response) == 0 {
		writeErr(w, http.StatusBadRequest, "session_id and response are required")
		return
	}
	label := in.Label
	if label == "" {
		label = "passkey"
	}
	rewriteBodyForFinish(r, in.Response)
	if _, err := d.WebAuthn.FinishRegister(r.Context(), uid, in.SessionID, label, r); err != nil {
		if errors.Is(err, bwebauthn.ErrUnknownSession) {
			writeErr(w, http.StatusBadRequest, "session expired or unknown")
			return
		}
		writeErr(w, http.StatusBadRequest, "register finish failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetWebAuthnCredentials handles GET /api/v1/auth/webauthn/credentials.
func (d Deps) GetWebAuthnCredentials(w http.ResponseWriter, r *http.Request) {
	if d.WebAuthn == nil {
		writeErr(w, http.StatusServiceUnavailable, "webauthn unavailable")
		return
	}
	uid := userID(r.Context())
	rows, err := d.WebAuthn.ListCredentialsForUser(r.Context(), uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "list failed")
		return
	}
	out := make([]webauthnCredentialResp, 0, len(rows))
	for _, c := range rows {
		out = append(out, webauthnCredentialResp{
			ID:        c.ID,
			Label:     c.Label,
			CreatedAt: c.CreatedAt,
			LastUsed:  c.LastUsed,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// DeleteWebAuthnCredential handles DELETE /api/v1/auth/webauthn/credentials/{id}.
func (d Deps) DeleteWebAuthnCredential(w http.ResponseWriter, r *http.Request) {
	if d.WebAuthn == nil {
		writeErr(w, http.StatusServiceUnavailable, "webauthn unavailable")
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		writeErr(w, http.StatusBadRequest, "id is required")
		return
	}
	uid := userID(r.Context())
	err := d.WebAuthn.DeleteCredential(r.Context(), uid, id)
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, bwebauthn.ErrForbidden):
		writeErr(w, http.StatusForbidden, "forbidden")
	case errors.Is(err, db.ErrNotFound):
		writeErr(w, http.StatusNotFound, "credential not found")
	default:
		writeErr(w, http.StatusInternalServerError, "delete failed")
	}
}

// PostWebAuthnLoginBegin handles POST /api/v1/auth/webauthn/login/begin.
// Public (no session) — the email selects the user.
func (d Deps) PostWebAuthnLoginBegin(w http.ResponseWriter, r *http.Request) {
	if d.WebAuthn == nil {
		writeErr(w, http.StatusServiceUnavailable, "webauthn unavailable")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var in webauthnLoginBeginReq
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Email == "" {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	res, err := d.WebAuthn.BeginLogin(r.Context(), in.Email)
	if err != nil {
		if errors.Is(err, bwebauthn.ErrUnknownUser) {
			// Never reveal whether the email exists.
			writeErr(w, http.StatusUnauthorized, "no passkey for this account")
			return
		}
		writeErr(w, http.StatusInternalServerError, "login begin failed")
		return
	}
	writeJSON(w, http.StatusOK, webauthnBeginResp{
		SessionID: res.SessionID,
		Options:   res.Options,
	})
}

// PostWebAuthnLoginFinish handles POST /api/v1/auth/webauthn/login/finish.
// Public (no session) — sets burrow_session + burrow_csrf on success in the
// same shape as the password Login handler.
func (d Deps) PostWebAuthnLoginFinish(w http.ResponseWriter, r *http.Request) {
	if d.WebAuthn == nil {
		writeErr(w, http.StatusServiceUnavailable, "webauthn unavailable")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32*1024)
	var in webauthnLoginFinishReq
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if in.SessionID == "" || len(in.Response) == 0 {
		writeErr(w, http.StatusBadRequest, "session_id and response are required")
		return
	}
	rewriteBodyForFinish(r, in.Response)
	uid, err := d.WebAuthn.FinishLogin(r.Context(), in.SessionID, r)
	if err != nil {
		if errors.Is(err, bwebauthn.ErrUnknownSession) {
			writeErr(w, http.StatusBadRequest, "session expired or unknown")
			return
		}
		writeErr(w, http.StatusUnauthorized, "passkey verification failed")
		return
	}
	// Mirror the password Login handler: validate user is not suspended,
	// then issue session + CSRF cookie.
	u, err := d.Users.GetUserByID(r.Context(), uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "login failed")
		return
	}
	if u.Status == "suspended" {
		writeErr(w, http.StatusForbidden, "account suspended")
		return
	}
	clientHost, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		clientHost = r.RemoteAddr
	}
	sid, err := d.Users.CreateSession(r.Context(), u.ID, r.UserAgent(), clientHost)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "login failed")
		return
	}
	csrfToken, err := generateCSRFToken()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "login failed")
		return
	}
	setSessionCookie(w, sid, d.SecureCookies)
	setCSRFCookie(w, csrfToken, d.SecureCookies)
	_ = d.Users.TouchUserLastLogin(r.Context(), u.ID)
	w.WriteHeader(http.StatusNoContent)
}
