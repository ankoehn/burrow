package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ankoehn/burrow/internal/db"
)

type sessionResp struct {
	ID        string    `json:"id"`
	IP        string    `json:"ip"`
	UserAgent string    `json:"user_agent"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Current   bool      `json:"current"`
}

// ListSessions returns the caller's active sessions. GET /api/v1/sessions
func (d Deps) ListSessions(w http.ResponseWriter, r *http.Request) {
	uid := userID(r.Context())
	ss, err := d.Sessions.ListSessions(r.Context(), uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "list sessions failed")
		return
	}
	cur := ""
	if c, err := r.Cookie(sessionCookieName); err == nil {
		cur = c.Value
	}
	out := make([]sessionResp, 0, len(ss))
	for _, s := range ss {
		out = append(out, sessionResp{
			ID: s.ID, IP: s.IP, UserAgent: s.UserAgent,
			CreatedAt: s.CreatedAt, ExpiresAt: s.ExpiresAt,
			Current: s.ID == cur,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// RevokeSession deletes one of the caller's sessions. DELETE /api/v1/sessions/{id}
func (d Deps) RevokeSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	err := d.Sessions.RevokeSession(r.Context(), id, userID(r.Context()))
	if errors.Is(err, db.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "session not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "revoke session failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RevokeAllSessions signs the caller out of every other session (keeps the
// current one). POST /api/v1/sessions/revoke-all
func (d Deps) RevokeAllSessions(w http.ResponseWriter, r *http.Request) {
	keep := ""
	if c, err := r.Cookie(sessionCookieName); err == nil {
		keep = c.Value
	}
	n, err := d.Sessions.RevokeOtherSessions(r.Context(), userID(r.Context()), keep)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "revoke all failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"revoked": n})
}
