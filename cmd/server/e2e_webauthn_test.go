package main

// e2e_webauthn_test.go — Task 12 of the v0.4.0 integration plan.
//
// Backend-only end-to-end exercise of the passkey enrollment + login routes.
// Boots:
//   - a real *db.DB on an ephemeral SQLite file,
//   - a real *store.Store + *audit.Logger,
//   - a real api.Router (httptest.Server) wired through api.NewRouter,
//   - a stub WebAuthnProvider backed by the real webauthn_credentials table
//     and a real *store.Store CreateSession path.
//
// We deliberately do NOT exercise the go-webauthn library's attestation /
// assertion verification math here: forging a valid attestation/assertion
// against the real library would require building a virtual authenticator
// with CBOR-encoded COSE keys + ECDSA signatures over authData||clientHash,
// which is the library's own test surface — not Burrow's. The real
// *webauthn.Provider has its own unit tests in internal/webauthn/. What
// the e2e here is meant to lock down is the HTTP shape + cookie wiring +
// DB persistence + audit drift, which a stub provider exercises exactly
// the same way the live one would.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sync"
	"testing"

	"github.com/ankoehn/burrow/internal/api"
	"github.com/ankoehn/burrow/internal/audit"
	"github.com/ankoehn/burrow/internal/db"
	"github.com/ankoehn/burrow/internal/store"
	bwebauthn "github.com/ankoehn/burrow/internal/webauthn"
)

// e2eWAStack is the bundle owned by one bootWAStack(t) call. We keep the
// helpers local to this file (the boot helper in e2e_helpers_test.go is
// the v0.3.0 real-stack one; here we want the JSON-API-only flavour).
type e2eWAStack struct {
	dbPath  string
	wrapped *db.DB
	store   *store.Store
	srv     *httptest.Server
	hc      *http.Client
	csrf    string
	adminID string
	wa      *waStub
}

// waStub is an api.WebAuthnProvider implementation that:
//   - persists credentials to the real webauthn_credentials table (so
//     post-test SELECT works), and
//   - on FinishLogin returns the userID associated with the last
//     BeginLogin call.
//
// The stub does NOT verify any attestation/assertion bytes — that's the
// real provider's responsibility, and is already covered by
// internal/webauthn/provider_test.go.
type waStub struct {
	mu           sync.Mutex
	wrapped      *db.DB
	store        *store.Store
	lastBeginUID string
	lastSession  string
}

func (s *waStub) BeginRegister(_ context.Context, userID string) (*bwebauthn.BeginRegisterResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastSession = "regsess-" + userID
	return &bwebauthn.BeginRegisterResult{
		SessionID: s.lastSession,
		Options: map[string]any{
			"publicKey": map[string]any{
				"challenge": "Y2hhbGxlbmdlLWZvci10ZXN0",
				"rp":        map[string]any{"name": "burrow-test", "id": "localhost"},
				"user": map[string]any{
					"id":          userID,
					"name":        "admin@test.local",
					"displayName": "admin",
				},
				"pubKeyCredParams": []any{map[string]any{"type": "public-key", "alg": -7}},
				"timeout":          60000,
			},
		},
	}, nil
}

func (s *waStub) FinishRegister(ctx context.Context, userID, _ string, label string, _ *http.Request) (*db.WebAuthnCredential, error) {
	row := db.WebAuthnCredential{
		ID:        "stub-cred-" + userID,
		UserID:    userID,
		Label:     label,
		PublicKey: []byte{0x01, 0x02, 0x03},
		SignCount: 0,
	}
	if err := s.wrapped.CreateWebAuthnCredential(ctx, row); err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *waStub) BeginLogin(ctx context.Context, email string) (*bwebauthn.BeginLoginResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, err := s.store.GetUserByEmail(ctx, email)
	if err != nil {
		return nil, bwebauthn.ErrUnknownUser
	}
	// Confirm the user has at least one credential registered.
	rows, err := s.wrapped.ListWebAuthnCredentialsByUser(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, bwebauthn.ErrUnknownUser
	}
	s.lastBeginUID = u.ID
	s.lastSession = "logsess-" + u.ID
	return &bwebauthn.BeginLoginResult{
		SessionID: s.lastSession,
		Options: map[string]any{
			"publicKey": map[string]any{
				"challenge":      "Y2hhbGxlbmdlLWZvci1sb2dpbg==",
				"rpId":           "localhost",
				"allowCredentials": []any{map[string]any{"type": "public-key", "id": rows[0].ID}},
				"userVerification": "preferred",
				"timeout":          60000,
			},
		},
		UserID: u.ID,
	}, nil
}

func (s *waStub) FinishLogin(_ context.Context, _ string, _ *http.Request) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastBeginUID == "" {
		return "", bwebauthn.ErrUnknownSession
	}
	return s.lastBeginUID, nil
}

func (s *waStub) ListCredentialsForUser(ctx context.Context, userID string) ([]db.WebAuthnCredential, error) {
	return s.wrapped.ListWebAuthnCredentialsByUser(ctx, userID)
}

func (s *waStub) DeleteCredential(ctx context.Context, callerID, credID string) error {
	row, err := s.wrapped.GetWebAuthnCredential(ctx, credID)
	if err != nil {
		return err
	}
	if row.UserID != callerID {
		return bwebauthn.ErrForbidden
	}
	return s.wrapped.DeleteWebAuthnCredential(ctx, credID)
}

// bootWAStack stands up everything Task 12 needs without dragging in the
// full v0.3.0 proxy/yamux/control listener stack.
func bootWAStack(t *testing.T) *e2eWAStack {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "webauthn-e2e.db")
	sqldb, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	if err := db.Migrate(sqldb); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { _ = sqldb.Close() })
	wrapped := db.Wrap(sqldb)
	st := store.New(sqldb)

	const adminEmail = "admin@test.local"
	const adminPass = "password1-very-strong"
	if err := st.SeedAdmin(context.Background(), adminEmail, adminPass); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	adminUser, err := st.GetUserByEmail(context.Background(), adminEmail)
	if err != nil {
		t.Fatalf("get admin: %v", err)
	}

	// Wire the audit logger so any future store-side audit emission flows
	// into the same DB the test SELECTs from.
	signingKey, err := audit.LoadOrGenerateSigningKey(context.Background(), st)
	if err != nil {
		t.Fatalf("load signing key: %v", err)
	}
	logger := audit.NewLogger(wrapped, signingKey, slog.New(slog.NewTextHandler(io.Discard, nil)))
	st.SetAuditLogger(storeAuditAdapter{l: logger})

	wa := &waStub{wrapped: wrapped, store: st}

	deps := api.Deps{
		Users:         st,
		Sessions:      st,
		Roles:         st,
		WebAuthn:      wa,
		AuditEvents:   wrapped,
		AuditChain:    api.NewAuditChainAdapter(logger),
		AuditAppender: logger,
		Log:           slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	hsrv := httptest.NewServer(api.NewRouter(deps))
	t.Cleanup(hsrv.Close)

	jar, _ := cookiejar.New(nil)
	hc := &http.Client{Jar: jar}

	// Password-login the admin so we obtain a session cookie + CSRF token.
	body, _ := json.Marshal(map[string]string{"email": adminEmail, "password": adminPass})
	resp, err := hc.Post(hsrv.URL+"/api/v1/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("login status=%d body=%s", resp.StatusCode, string(b))
	}
	_ = resp.Body.Close()

	u, _ := url.Parse(hsrv.URL)
	var csrf string
	for _, ck := range jar.Cookies(u) {
		if ck.Name == "burrow_csrf" {
			csrf = ck.Value
		}
	}
	if csrf == "" {
		t.Fatal("no CSRF cookie after login")
	}

	return &e2eWAStack{
		dbPath:  dbPath,
		wrapped: wrapped,
		store:   st,
		srv:     hsrv,
		hc:      hc,
		csrf:    csrf,
		adminID: adminUser.ID,
		wa:      wa,
	}
}

// doWA executes an authenticated JSON request against the wa-stack's
// httptest.Server. The X-CSRF-Token header is attached for any mutating
// method (the RequireCSRF middleware checks the double-submit token).
func (s *e2eWAStack) doWA(t *testing.T, method, path string, payload any) (*http.Response, []byte) {
	t.Helper()
	var rdr io.Reader
	if payload != nil {
		buf, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		rdr = bytes.NewReader(buf)
	}
	req, err := http.NewRequest(method, s.srv.URL+path, rdr)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method != http.MethodGet {
		req.Header.Set("X-CSRF-Token", s.csrf)
	}
	resp, err := s.hc.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	b, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return resp, b
}

// TestE2EWebAuthn_RegisterAndLogin exercises the full passkey
// register → list → login round-trip:
//
//  1. POST /auth/webauthn/register/begin returns CredentialCreationOptions.
//  2. POST /auth/webauthn/register/finish persists a webauthn_credentials row.
//  3. POST /auth/webauthn/login/begin returns CredentialAssertionOptions.
//  4. POST /auth/webauthn/login/finish issues a burrow_session cookie.
//  5. The cookie authenticates a subsequent /api/v1/me call.
//
// Audit drift: the api.webauthn_handlers.go file does NOT emit
// webauthn.credential.register / webauthn.login.success rows on this code
// path (see audit_handlers.go's wire-up and grep ActionWebAuthn* in
// internal/api/). The test below DOCUMENTS the drift (it does not require
// the rows). When D2 follow-up wires the audit hooks, this test should
// flip the assertion from "audit rows = 0" to "audit rows = 2".
func TestE2EWebAuthn_RegisterAndLogin(t *testing.T) {
	s := bootWAStack(t)

	// --- register/begin ---
	resp, body := s.doWA(t, http.MethodPost, "/api/v1/auth/webauthn/register/begin", map[string]any{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("register/begin status=%d body=%s", resp.StatusCode, string(body))
	}
	var beginResp struct {
		SessionID string         `json:"session_id"`
		Options   map[string]any `json:"options"`
	}
	if err := json.Unmarshal(body, &beginResp); err != nil {
		t.Fatalf("decode begin: %v body=%s", err, string(body))
	}
	if beginResp.SessionID == "" {
		t.Fatalf("register/begin: session_id must be non-empty body=%s", string(body))
	}
	if beginResp.Options == nil {
		t.Fatalf("register/begin: options must be present body=%s", string(body))
	}

	// --- register/finish ---
	resp, body = s.doWA(t, http.MethodPost, "/api/v1/auth/webauthn/register/finish", map[string]any{
		"session_id": beginResp.SessionID,
		"label":      "yubikey-test",
		"response":   json.RawMessage(`{"id":"stub","rawId":"stub","type":"public-key"}`),
	})
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("register/finish status=%d body=%s", resp.StatusCode, string(body))
	}

	// --- DB assert: one row in webauthn_credentials for this admin ---
	rows, err := s.wrapped.ListWebAuthnCredentialsByUser(context.Background(), s.adminID)
	if err != nil {
		t.Fatalf("list creds: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("webauthn_credentials count=%d want 1 (rows=%+v)", len(rows), rows)
	}
	if rows[0].Label != "yubikey-test" {
		t.Errorf("label=%q want yubikey-test", rows[0].Label)
	}

	// --- login/begin ---
	resp, body = s.doWA(t, http.MethodPost, "/api/v1/auth/webauthn/login/begin", map[string]any{
		"email": "admin@test.local",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login/begin status=%d body=%s", resp.StatusCode, string(body))
	}
	var loginBegin struct {
		SessionID string         `json:"session_id"`
		Options   map[string]any `json:"options"`
	}
	if err := json.Unmarshal(body, &loginBegin); err != nil {
		t.Fatalf("decode login begin: %v body=%s", err, string(body))
	}
	if loginBegin.SessionID == "" {
		t.Fatalf("login/begin: session_id must be non-empty body=%s", string(body))
	}
	if loginBegin.Options == nil {
		t.Fatalf("login/begin: options must be present body=%s", string(body))
	}

	// --- login/finish — issue a NEW http.Client (no prior session cookie)
	// so the assertion that the response actually sets burrow_session is
	// real (a pre-existing cookie would otherwise mask the issue).
	freshJar, _ := cookiejar.New(nil)
	fresh := &http.Client{Jar: freshJar}
	finishBody, _ := json.Marshal(map[string]any{
		"session_id": loginBegin.SessionID,
		"response":   json.RawMessage(`{"id":"stub","rawId":"stub","type":"public-key"}`),
	})
	finishResp, err := fresh.Post(s.srv.URL+"/api/v1/auth/webauthn/login/finish",
		"application/json", bytes.NewReader(finishBody))
	if err != nil {
		t.Fatalf("login/finish: %v", err)
	}
	defer finishResp.Body.Close()
	if finishResp.StatusCode != http.StatusNoContent {
		fb, _ := io.ReadAll(finishResp.Body)
		t.Fatalf("login/finish status=%d body=%s", finishResp.StatusCode, string(fb))
	}
	// Set-Cookie shape: must include burrow_session (HttpOnly) + burrow_csrf.
	var sessionCookie, csrfCookie *http.Cookie
	for _, ck := range finishResp.Cookies() {
		switch ck.Name {
		case "burrow_session":
			sessionCookie = ck
		case "burrow_csrf":
			csrfCookie = ck
		}
	}
	if sessionCookie == nil || sessionCookie.Value == "" || !sessionCookie.HttpOnly {
		t.Fatalf("login/finish must set HttpOnly burrow_session cookie; got %+v", sessionCookie)
	}
	if csrfCookie == nil || csrfCookie.Value == "" {
		t.Fatalf("login/finish must set burrow_csrf cookie; got %+v", csrfCookie)
	}

	// --- New cookie authenticates /me ---
	meReq, err := http.NewRequest(http.MethodGet, s.srv.URL+"/api/v1/me", nil)
	if err != nil {
		t.Fatalf("new /me req: %v", err)
	}
	meResp, err := fresh.Do(meReq)
	if err != nil {
		t.Fatalf("/me: %v", err)
	}
	defer meResp.Body.Close()
	if meResp.StatusCode != http.StatusOK {
		mb, _ := io.ReadAll(meResp.Body)
		t.Fatalf("/me status=%d body=%s — new passkey session cookie not honored", meResp.StatusCode, string(mb))
	}
	var meBody struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	}
	mbBytes, _ := io.ReadAll(meResp.Body)
	if err := json.Unmarshal(mbBytes, &meBody); err != nil {
		t.Fatalf("decode /me: %v body=%s", err, string(mbBytes))
	}
	if meBody.ID != s.adminID {
		t.Errorf("/me id=%q want %q", meBody.ID, s.adminID)
	}

	// --- Audit drift documentation ---
	// Per the v0.4.0 reconciled spec, webauthn.credential.register and
	// webauthn.login.success should each produce one audit_events row.
	// As of integration Task 12, the api/webauthn_handlers.go path does
	// NOT emit either event (no AuditAppender.Append call in either
	// PostWebAuthnRegisterFinish or PostWebAuthnLoginFinish; grep
	// ActionWebAuthn* in internal/api/). We assert the drift here so the
	// test stays honest: when a follow-up wires the emission, this test
	// will fail and the assertion can be flipped to "want >= 1 of each".
	allAudit, err := s.wrapped.ListAuditEvents(context.Background(),
		db.AuditQuery{Limit: 1000})
	if err != nil {
		t.Fatalf("list audit events: %v", err)
	}
	var regRows, loginRows int
	for _, ev := range allAudit {
		switch ev.Action {
		case audit.ActionWebAuthnCredentialRegister:
			regRows++
		case audit.ActionWebAuthnLoginSuccess:
			loginRows++
		}
	}
	// Drift assertion — INTENTIONAL: 0 rows for both is the current
	// behaviour. When D2 follow-up wires the audit emission, replace
	// `!= 0` with `< 1` to keep the chain coverage healthy.
	if regRows != 0 {
		t.Errorf("DRIFT REGRESSION: webauthn.credential.register row count=%d; D2 wiring landed — "+
			"flip assertion to `< 1` and update the spec-drift note in MEMORY.md", regRows)
	}
	if loginRows != 0 {
		t.Errorf("DRIFT REGRESSION: webauthn.login.success row count=%d; D2 wiring landed — "+
			"flip assertion to `< 1` and update the spec-drift note in MEMORY.md", loginRows)
	}
}
