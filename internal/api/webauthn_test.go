package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ankoehn/burrow/internal/db"
	bwebauthn "github.com/ankoehn/burrow/internal/webauthn"
)

// fakeWebAuthnProvider is a doubles-only WebAuthnProvider used by every test
// in this file. The provider package has its own tests against the real
// library; here we exercise the HTTP shape, error mapping, and cookie wiring
// without forging valid attestation/assertion data.
type fakeWebAuthnProvider struct {
	mu sync.Mutex

	// Begin{Register,Login} return values.
	beginRegister *bwebauthn.BeginRegisterResult
	beginRegErr   error
	beginLogin    *bwebauthn.BeginLoginResult
	beginLogErr   error

	// Finish{Register,Login} outcomes.
	finishRegErr error
	finishLogUID string
	finishLogErr error

	// State exposed to assertions.
	lastRegSession string
	lastLogSession string
	lastBeginEmail string

	// Storage doubles.
	creds   map[string][]db.WebAuthnCredential // userID → rows
	delErr  error
	listErr error
}

func newFakeWebAuthnProvider() *fakeWebAuthnProvider {
	return &fakeWebAuthnProvider{
		creds: map[string][]db.WebAuthnCredential{},
	}
}

func (f *fakeWebAuthnProvider) BeginRegister(_ context.Context, userID string) (*bwebauthn.BeginRegisterResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.beginRegErr != nil {
		return nil, f.beginRegErr
	}
	res := f.beginRegister
	if res == nil {
		res = &bwebauthn.BeginRegisterResult{
			SessionID: "regsid-" + userID,
			Options:   map[string]string{"challenge": "AAAAAAAAAAAAAAAAAA"},
		}
	}
	return res, nil
}

func (f *fakeWebAuthnProvider) FinishRegister(_ context.Context, userID, sessionID, label string, _ *http.Request) (*db.WebAuthnCredential, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastRegSession = sessionID
	if f.finishRegErr != nil {
		return nil, f.finishRegErr
	}
	row := db.WebAuthnCredential{
		ID:        "cred-" + label + "-" + userID,
		UserID:    userID,
		Label:     label,
		PublicKey: []byte{0x01},
		CreatedAt: time.Now().UTC(),
	}
	f.creds[userID] = append(f.creds[userID], row)
	return &row, nil
}

func (f *fakeWebAuthnProvider) BeginLogin(_ context.Context, email string) (*bwebauthn.BeginLoginResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastBeginEmail = email
	if f.beginLogErr != nil {
		return nil, f.beginLogErr
	}
	res := f.beginLogin
	if res == nil {
		res = &bwebauthn.BeginLoginResult{
			SessionID: "logsid-" + email,
			Options:   map[string]string{"challenge": "BBBBBBBBBBBBBBBBBB"},
			UserID:    "u1",
		}
	}
	return res, nil
}

func (f *fakeWebAuthnProvider) FinishLogin(_ context.Context, sessionID string, _ *http.Request) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastLogSession = sessionID
	if f.finishLogErr != nil {
		return "", f.finishLogErr
	}
	if f.finishLogUID == "" {
		return "u1", nil
	}
	return f.finishLogUID, nil
}

func (f *fakeWebAuthnProvider) ListCredentialsForUser(_ context.Context, userID string) ([]db.WebAuthnCredential, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := append([]db.WebAuthnCredential(nil), f.creds[userID]...)
	return out, nil
}

func (f *fakeWebAuthnProvider) DeleteCredential(_ context.Context, callerID, credID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.delErr != nil {
		return f.delErr
	}
	rows := f.creds[callerID]
	for i, r := range rows {
		if r.ID == credID {
			f.creds[callerID] = append(rows[:i], rows[i+1:]...)
			return nil
		}
	}
	return db.ErrNotFound
}

// --- register/begin ------------------------------------------------------

func TestWebAuthnRegisterBeginReturnsSessionAndOptions(t *testing.T) {
	wa := newFakeWebAuthnProvider()
	users := &fakeUserStore{}
	srv := newTestServer(Deps{Users: users, WebAuthn: wa, Log: discardLog()})
	defer srv.Close()
	c := authedClient(t, srv)
	resp := c.post(t, "/api/v1/auth/webauthn/register/begin", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}
	var out webauthnBeginResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.SessionID == "" {
		t.Fatalf("session_id must be non-empty")
	}
	if out.Options == nil {
		t.Fatalf("options must be present")
	}
}

func TestWebAuthnRegisterBeginRequiresSession(t *testing.T) {
	wa := newFakeWebAuthnProvider()
	users := &fakeUserStore{}
	srv := newTestServer(Deps{Users: users, WebAuthn: wa, Log: discardLog()})
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/api/v1/auth/webauthn/register/begin", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", resp.StatusCode)
	}
}

func TestWebAuthnUnwiredReturns503(t *testing.T) {
	users := &fakeUserStore{}
	srv := newTestServer(Deps{Users: users, Log: discardLog()}) // WebAuthn nil
	defer srv.Close()
	c := authedClient(t, srv)
	resp := c.post(t, "/api/v1/auth/webauthn/register/begin", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("want 503 when WebAuthn nil, got %d", resp.StatusCode)
	}
}

// --- register/finish -----------------------------------------------------

func TestWebAuthnRegisterFinishRequiresBody(t *testing.T) {
	wa := newFakeWebAuthnProvider()
	users := &fakeUserStore{}
	srv := newTestServer(Deps{Users: users, WebAuthn: wa, Log: discardLog()})
	defer srv.Close()
	c := authedClient(t, srv)
	// Missing session_id.
	resp := c.post(t, "/api/v1/auth/webauthn/register/finish",
		map[string]any{"response": json.RawMessage(`{"x":1}`)})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400 missing session_id, got %d", resp.StatusCode)
	}
}

func TestWebAuthnRegisterFinishHappyPath(t *testing.T) {
	wa := newFakeWebAuthnProvider()
	users := &fakeUserStore{}
	srv := newTestServer(Deps{Users: users, WebAuthn: wa, Log: discardLog()})
	defer srv.Close()
	c := authedClient(t, srv)
	resp := c.post(t, "/api/v1/auth/webauthn/register/finish", map[string]any{
		"session_id": "sess-1",
		"label":      "yubikey",
		"response":   json.RawMessage(`{"id":"abc"}`),
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("want 204, got %d body=%s", resp.StatusCode, readBody(t, resp))
	}
	// The provider must have seen our session_id.
	if wa.lastRegSession != "sess-1" {
		t.Fatalf("provider saw session_id=%q", wa.lastRegSession)
	}
	// And a row landed in the fake store.
	rows, _ := wa.ListCredentialsForUser(context.Background(), users.id())
	if len(rows) != 1 || rows[0].Label != "yubikey" {
		t.Fatalf("expected one row labelled yubikey, got %+v", rows)
	}
}

func TestWebAuthnRegisterFinishMapsUnknownSession(t *testing.T) {
	wa := newFakeWebAuthnProvider()
	wa.finishRegErr = bwebauthn.ErrUnknownSession
	users := &fakeUserStore{}
	srv := newTestServer(Deps{Users: users, WebAuthn: wa, Log: discardLog()})
	defer srv.Close()
	c := authedClient(t, srv)
	resp := c.post(t, "/api/v1/auth/webauthn/register/finish", map[string]any{
		"session_id": "expired",
		"response":   json.RawMessage(`{"x":1}`),
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400 expired-session, got %d", resp.StatusCode)
	}
}

// --- credentials list / delete -----------------------------------------

func TestWebAuthnListCredentials(t *testing.T) {
	wa := newFakeWebAuthnProvider()
	users := &fakeUserStore{}
	wa.creds[users.id()] = []db.WebAuthnCredential{
		{ID: "a", Label: "a", CreatedAt: time.Now().UTC()},
		{ID: "b", Label: "b", CreatedAt: time.Now().UTC()},
	}
	srv := newTestServer(Deps{Users: users, WebAuthn: wa, Log: discardLog()})
	defer srv.Close()
	c := authedClient(t, srv)
	resp := c.get(t, "/api/v1/auth/webauthn/credentials")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	var out []webauthnCredentialResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("want 2 rows, got %d", len(out))
	}
}

func TestWebAuthnDeleteCredentialForbidsCrossUser(t *testing.T) {
	wa := newFakeWebAuthnProvider()
	wa.delErr = bwebauthn.ErrForbidden
	users := &fakeUserStore{}
	srv := newTestServer(Deps{Users: users, WebAuthn: wa, Log: discardLog()})
	defer srv.Close()
	c := authedClient(t, srv)
	resp := c.delete(t, "/api/v1/auth/webauthn/credentials/someone-elses")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("want 403, got %d", resp.StatusCode)
	}
}

func TestWebAuthnDeleteCredentialNotFound(t *testing.T) {
	wa := newFakeWebAuthnProvider()
	wa.delErr = db.ErrNotFound
	users := &fakeUserStore{}
	srv := newTestServer(Deps{Users: users, WebAuthn: wa, Log: discardLog()})
	defer srv.Close()
	c := authedClient(t, srv)
	resp := c.delete(t, "/api/v1/auth/webauthn/credentials/ghost")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404, got %d", resp.StatusCode)
	}
}

func TestWebAuthnDeleteCredentialSuccess(t *testing.T) {
	wa := newFakeWebAuthnProvider()
	users := &fakeUserStore{}
	wa.creds[users.id()] = []db.WebAuthnCredential{
		{ID: "kill-me", Label: "doomed"},
	}
	srv := newTestServer(Deps{Users: users, WebAuthn: wa, Log: discardLog()})
	defer srv.Close()
	c := authedClient(t, srv)
	resp := c.delete(t, "/api/v1/auth/webauthn/credentials/kill-me")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("want 204, got %d body=%s", resp.StatusCode, readBody(t, resp))
	}
	rows, _ := wa.ListCredentialsForUser(context.Background(), users.id())
	if len(rows) != 0 {
		t.Fatalf("credential must be deleted; got %+v", rows)
	}
}

// --- login/begin --------------------------------------------------------

func TestWebAuthnLoginBeginPublicAccess(t *testing.T) {
	wa := newFakeWebAuthnProvider()
	users := &fakeUserStore{}
	srv := newTestServer(Deps{Users: users, WebAuthn: wa, Log: discardLog()})
	defer srv.Close()
	// No session — explicitly an unauthenticated call.
	resp, err := http.Post(srv.URL+"/api/v1/auth/webauthn/login/begin", "application/json",
		strings.NewReader(`{"email":"alice@x"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", resp.StatusCode, readBody(t, resp))
	}
	if wa.lastBeginEmail != "alice@x" {
		t.Fatalf("provider saw email=%q", wa.lastBeginEmail)
	}
}

func TestWebAuthnLoginBeginUnknownUserIs401(t *testing.T) {
	wa := newFakeWebAuthnProvider()
	wa.beginLogErr = bwebauthn.ErrUnknownUser
	users := &fakeUserStore{}
	srv := newTestServer(Deps{Users: users, WebAuthn: wa, Log: discardLog()})
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/api/v1/auth/webauthn/login/begin", "application/json",
		strings.NewReader(`{"email":"ghost@x"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401 unknown user, got %d", resp.StatusCode)
	}
}

func TestWebAuthnLoginBeginRejectsMissingEmail(t *testing.T) {
	wa := newFakeWebAuthnProvider()
	users := &fakeUserStore{}
	srv := newTestServer(Deps{Users: users, WebAuthn: wa, Log: discardLog()})
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/api/v1/auth/webauthn/login/begin", "application/json",
		strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", resp.StatusCode)
	}
}

// --- login/finish -------------------------------------------------------

func TestWebAuthnLoginFinishSetsSessionCookieLikePasswordLogin(t *testing.T) {
	wa := newFakeWebAuthnProvider()
	users := &fakeUserStore{} // CreateSession returns "sid-u-self"
	srv := newTestServer(Deps{Users: users, WebAuthn: wa, Log: discardLog()})
	defer srv.Close()
	body := `{"session_id":"sess","response":{"id":"abc"}}`
	resp, err := http.Post(srv.URL+"/api/v1/auth/webauthn/login/finish", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("want 204, got %d body=%s", resp.StatusCode, readBody(t, resp))
	}
	// Cookie shape MUST mirror password-login: a burrow_session cookie AND
	// a burrow_csrf cookie, with HttpOnly only on the session cookie.
	cks := resp.Cookies()
	var session, csrf *http.Cookie
	for _, c := range cks {
		switch c.Name {
		case sessionCookieName:
			session = c
		case csrfCookieName:
			csrf = c
		}
	}
	if session == nil || !session.HttpOnly || session.Value == "" {
		t.Fatalf("burrow_session cookie missing or wrong shape: %+v", session)
	}
	if csrf == nil || csrf.HttpOnly || csrf.Value == "" {
		t.Fatalf("burrow_csrf cookie missing or wrong shape: %+v", csrf)
	}
	if !users.lastLoginTouched {
		t.Fatalf("TouchUserLastLogin must be called on successful passkey login")
	}
}

func TestWebAuthnLoginFinishVerificationFailureIs401(t *testing.T) {
	wa := newFakeWebAuthnProvider()
	wa.finishLogErr = errors.New("bad assertion")
	users := &fakeUserStore{}
	srv := newTestServer(Deps{Users: users, WebAuthn: wa, Log: discardLog()})
	defer srv.Close()
	body := `{"session_id":"sess","response":{"id":"abc"}}`
	resp, err := http.Post(srv.URL+"/api/v1/auth/webauthn/login/finish", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", resp.StatusCode)
	}
}

func TestWebAuthnLoginFinishExpiredSessionIs400(t *testing.T) {
	wa := newFakeWebAuthnProvider()
	wa.finishLogErr = bwebauthn.ErrUnknownSession
	users := &fakeUserStore{}
	srv := newTestServer(Deps{Users: users, WebAuthn: wa, Log: discardLog()})
	defer srv.Close()
	body := `{"session_id":"expired","response":{"id":"abc"}}`
	resp, err := http.Post(srv.URL+"/api/v1/auth/webauthn/login/finish", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", resp.StatusCode)
	}
}

func TestWebAuthnLoginFinishSuspendedAccount(t *testing.T) {
	wa := newFakeWebAuthnProvider()
	users := &fakeUserStore{suspended: true}
	srv := newTestServer(Deps{Users: users, WebAuthn: wa, Log: discardLog()})
	defer srv.Close()
	body := `{"session_id":"sess","response":{"id":"abc"}}`
	resp, err := http.Post(srv.URL+"/api/v1/auth/webauthn/login/finish", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("want 403 suspended, got %d", resp.StatusCode)
	}
}

// Note: newTestServer + authedClient are shared with the other api tests
// (see auth_test.go + testhelpers_test.go).
