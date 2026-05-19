package proxy_test

// gate_test.go — TDD tests for the burrow-login forward-auth gate (Task 9).
//
// Tests exercise the Gate handler standalone via httptest; they do NOT stand up
// the full Proxy (TestProxyGatePath in proxy_test.go already covers that wiring).

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ankoehn/burrow/internal/db"
	"github.com/ankoehn/burrow/internal/proxy"
)

// ---------------------------------------------------------------------------
// Fake GateStore implementation
// ---------------------------------------------------------------------------

type fakeGateStore struct {
	user       db.User
	sessionMap map[string]string // sessionID → userID
	serviceMap map[string]db.Service
	policyMap  map[string][]string // serviceID → allowed roles
	failVerify bool
	failCreate bool
}

func newFakeGateStore() *fakeGateStore {
	return &fakeGateStore{
		sessionMap: make(map[string]string),
		serviceMap: make(map[string]db.Service),
		policyMap:  make(map[string][]string),
	}
}

func (f *fakeGateStore) VerifyUserPassword(_ context.Context, email, password string) (bool, error) {
	if f.failVerify {
		return false, nil
	}
	return email == f.user.Email && password == "correct-password", nil
}

func (f *fakeGateStore) GetUserByEmail(_ context.Context, email string) (db.User, error) {
	if email == f.user.Email {
		return f.user, nil
	}
	return db.User{}, db.ErrNotFound
}

func (f *fakeGateStore) CreateSession(_ context.Context, userID, _, _ string) (string, error) {
	if f.failCreate {
		return "", db.ErrNotFound // any non-nil error
	}
	id := "sess-" + userID
	f.sessionMap[id] = userID
	return id, nil
}

func (f *fakeGateStore) ValidateSession(_ context.Context, id string) (string, error) {
	if uid, ok := f.sessionMap[id]; ok {
		return uid, nil
	}
	return "", db.ErrNotFound
}

func (f *fakeGateStore) DeleteSession(_ context.Context, id string) error {
	delete(f.sessionMap, id)
	return nil
}

func (f *fakeGateStore) GetUserByID(_ context.Context, id string) (db.User, error) {
	if id == f.user.ID {
		return f.user, nil
	}
	return db.User{}, db.ErrNotFound
}

func (f *fakeGateStore) ServiceForSubdomain(_ context.Context, sub string) (db.Service, error) {
	if svc, ok := f.serviceMap[sub]; ok {
		return svc, nil
	}
	return db.Service{}, db.ErrNotFound
}

func (f *fakeGateStore) RoleAllowed(_ context.Context, serviceID, role string) (bool, error) {
	roles, ok := f.policyMap[serviceID]
	if !ok {
		return false, nil
	}
	for _, r := range roles {
		if r == role {
			return true, nil
		}
	}
	return false, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const gateAuthDomain = "auth.example.com"

func newTestGate(st proxy.GateStore) http.Handler {
	return proxy.NewGate(st, gateAuthDomain, false, testLog())
}

func newTestGateWithUser(role, status string) (*fakeGateStore, http.Handler) {
	st := newFakeGateStore()
	st.user = db.User{
		ID:     "user-1",
		Email:  "alice@example.com",
		Role:   role,
		Status: status,
	}
	return st, newTestGate(st)
}

// postLoginForm issues a POST /__burrow/login with the given form values.
func postLoginForm(gate http.Handler, fields map[string]string) *httptest.ResponseRecorder {
	form := url.Values{}
	for k, v := range fields {
		form.Set(k, v)
	}
	req := httptest.NewRequest("POST", "/__burrow/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "1.2.3.4:5678"
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, req)
	return rec
}

// ---------------------------------------------------------------------------
// Test: GET /__burrow/login renders login form
// ---------------------------------------------------------------------------

func TestGateGetLogin_Renders(t *testing.T) {
	_, gate := newTestGateWithUser("user", "active")
	req := httptest.NewRequest("GET", "/__burrow/login", nil)
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Sign in to continue") {
		t.Errorf("body missing 'Sign in to continue': %q", body[:min(200, len(body))])
	}
	if !strings.Contains(body, `<form`) {
		t.Errorf("body missing <form element")
	}
	if !strings.Contains(body, `method`) {
		t.Errorf("body missing method attribute")
	}
}

func TestGateGetLogin_ServiceLabel_FromNext(t *testing.T) {
	st := newFakeGateStore()
	st.user = db.User{ID: "u1", Email: "a@example.com", Role: "user", Status: "active"}
	st.serviceMap["app"] = db.Service{ID: "svc1", Name: "My App", Subdomain: "app"}
	gate := newTestGate(st)

	nextURL := "https://app." + gateAuthDomain + "/dashboard"
	req := httptest.NewRequest("GET", "/__burrow/login?next="+url.QueryEscape(nextURL), nil)
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, req)

	body := rec.Body.String()
	// The first label of the next host should appear as service label
	if !strings.Contains(body, "app") {
		t.Errorf("body missing service label 'app': %q", body[:min(300, len(body))])
	}
}

// ---------------------------------------------------------------------------
// Test: next parameter validation (open-redirect guard)
// ---------------------------------------------------------------------------

func TestGateGetLogin_NextOffDomain_Dropped(t *testing.T) {
	_, gate := newTestGateWithUser("user", "active")

	// next pointing to a different domain → must be dropped
	next := url.QueryEscape("https://evil.com/steal")
	req := httptest.NewRequest("GET", "/__burrow/login?next="+next, nil)
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	// The form's action/hidden next must NOT contain evil.com
	body := rec.Body.String()
	if strings.Contains(body, "evil.com") {
		t.Errorf("body should not contain off-domain next URL: evil.com found in body")
	}
}

func TestGateGetLogin_NextMalformed_Dropped(t *testing.T) {
	_, gate := newTestGateWithUser("user", "active")

	// Malformed next (not parseable as URL)
	req := httptest.NewRequest("GET", "/__burrow/login?next=://noscheme", nil)
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
}

func TestGateGetLogin_NextHTTPScheme_Dropped(t *testing.T) {
	_, gate := newTestGateWithUser("user", "active")

	next := url.QueryEscape("http://app." + gateAuthDomain + "/path")
	req := httptest.NewRequest("GET", "/__burrow/login?next="+next, nil)
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, req)

	body := rec.Body.String()
	// http:// next must be dropped; body should not contain that insecure URL
	if strings.Contains(body, "http://app.") {
		t.Errorf("body should not carry http:// next URL")
	}
}

func TestGateGetLogin_NextSameAuthDomain_Accepted(t *testing.T) {
	_, gate := newTestGateWithUser("user", "active")

	next := "https://" + gateAuthDomain + "/"
	req := httptest.NewRequest("GET", "/__burrow/login?next="+url.QueryEscape(next), nil)
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
}

func TestGateGetLogin_NextSubdomain_Accepted(t *testing.T) {
	_, gate := newTestGateWithUser("user", "active")

	next := "https://app." + gateAuthDomain + "/path"
	req := httptest.NewRequest("GET", "/__burrow/login?next="+url.QueryEscape(next), nil)
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// Test: POST /__burrow/login valid credentials → set cookie + redirect
// ---------------------------------------------------------------------------

func TestGatePostLogin_ValidCreds_SetsSessionCookie(t *testing.T) {
	st, gate := newTestGateWithUser("user", "active")
	_ = st
	next := "https://app." + gateAuthDomain + "/after-login"

	rec := postLoginForm(gate, map[string]string{
		"email":    "alice@example.com",
		"password": "correct-password",
		"next":     next,
	})

	if rec.Code != http.StatusFound {
		t.Fatalf("want 302, got %d (body: %s)", rec.Code, rec.Body.String())
	}
	// Verify Location header points to validated next
	loc := rec.Header().Get("Location")
	if loc != next {
		t.Errorf("want Location=%q, got %q", next, loc)
	}
	// Check for burrow_session cookie
	cookies := rec.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "burrow_session" {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("want burrow_session cookie, got none")
	}
	if sessionCookie.Value == "" {
		t.Error("burrow_session cookie value is empty")
	}
	// Cookie MUST have Domain = authDomain (shared SSO cookie)
	if sessionCookie.Domain != gateAuthDomain {
		t.Errorf("burrow_session cookie Domain: want %q, got %q", gateAuthDomain, sessionCookie.Domain)
	}
	if !sessionCookie.HttpOnly {
		t.Error("burrow_session cookie must be HttpOnly")
	}
	if sessionCookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("burrow_session cookie SameSite: want Lax, got %v", sessionCookie.SameSite)
	}
}

func TestGatePostLogin_ValidCreds_NextOffDomain_Fallback(t *testing.T) {
	_, gate := newTestGateWithUser("user", "active")
	// next points off-domain → should redirect to default fallback
	rec := postLoginForm(gate, map[string]string{
		"email":    "alice@example.com",
		"password": "correct-password",
		"next":     "https://evil.com/steal",
	})
	if rec.Code != http.StatusFound {
		t.Fatalf("want 302, got %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if strings.Contains(loc, "evil.com") {
		t.Errorf("Location must not point to off-domain next; got %q", loc)
	}
	wantDefault := "https://" + gateAuthDomain + "/"
	if loc != wantDefault {
		t.Errorf("Location: want fallback %q, got %q", wantDefault, loc)
	}
}

// ---------------------------------------------------------------------------
// Test: POST /__burrow/login invalid credentials → re-render with alert
// ---------------------------------------------------------------------------

func TestGatePostLogin_InvalidCreds_ReRenderWithAlert(t *testing.T) {
	_, gate := newTestGateWithUser("user", "active")
	rec := postLoginForm(gate, map[string]string{
		"email":    "alice@example.com",
		"password": "wrong-password",
		"next":     "",
	})

	// Must be 200 (re-render), NOT 401
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200 (re-render), got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Invalid email or password") {
		t.Errorf("body missing alert 'Invalid email or password': %q", body[:min(400, len(body))])
	}
	// Must have role="alert" for accessibility
	if !strings.Contains(body, `role="alert"`) {
		t.Errorf("body missing role=\"alert\" attribute")
	}
	// Must NOT set a session cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "burrow_session" {
			t.Errorf("must not set burrow_session cookie on failed login")
		}
	}
}

func TestGatePostLogin_SuspendedUser_ReRenderWithAlert(t *testing.T) {
	_, gate := newTestGateWithUser("user", "suspended")
	rec := postLoginForm(gate, map[string]string{
		"email":    "alice@example.com",
		"password": "correct-password",
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200 (re-render for suspended), got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Invalid email or password") {
		t.Errorf("suspended user: body missing 'Invalid email or password' alert")
	}
}

// ---------------------------------------------------------------------------
// Test: Rate limiting — 11th bad POST from same IP gets 429
// ---------------------------------------------------------------------------

func TestGatePostLogin_RateLimit(t *testing.T) {
	_, gate := newTestGateWithUser("user", "active")

	var last *httptest.ResponseRecorder
	for i := 0; i < 11; i++ {
		form := url.Values{}
		form.Set("email", "alice@example.com")
		form.Set("password", "wrong")
		req := httptest.NewRequest("POST", "/__burrow/login", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.RemoteAddr = "9.9.9.9:1234" // same IP every time
		last = httptest.NewRecorder()
		gate.ServeHTTP(last, req)
	}
	if last.Code != http.StatusTooManyRequests {
		t.Errorf("11th bad POST: want 429, got %d", last.Code)
	}
}

// ---------------------------------------------------------------------------
// Test: Access-denied page (authenticated but role not in policy)
// ---------------------------------------------------------------------------

func TestGateGetLogin_AuthenticatedWrongRole_AccessDenied(t *testing.T) {
	st := newFakeGateStore()
	st.user = db.User{
		ID:     "user-2",
		Email:  "bob@example.com",
		Role:   "user",
		Status: "active",
	}
	// Register service with only "admin" in policy
	st.serviceMap["secure"] = db.Service{ID: "svc-secure", Name: "SecureApp", Subdomain: "secure"}
	st.policyMap["svc-secure"] = []string{"admin"}
	// Pre-seed a valid session
	sessionID := "sess-user-2"
	st.sessionMap[sessionID] = "user-2"

	gate := newTestGate(st)

	next := "https://secure." + gateAuthDomain + "/page"
	req := httptest.NewRequest("GET", "/__burrow/login?next="+url.QueryEscape(next), nil)
	// Attach session cookie
	req.AddCookie(&http.Cookie{Name: "burrow_session", Value: sessionID})
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("want 403 access-denied, got %d (body: %s)", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("access-denied must be HTML, got Content-Type %q", ct)
	}
	// Must mention user's email and role in the page
	if !strings.Contains(body, "bob@example.com") {
		t.Errorf("access-denied body should mention user email")
	}
	// Must include a sign-out link/form
	if !strings.Contains(body, "__burrow/logout") {
		t.Errorf("access-denied body should contain logout link/form")
	}
}

func TestGateGetLogin_AuthenticatedAllowedRole_RedirectsToNext(t *testing.T) {
	st := newFakeGateStore()
	st.user = db.User{
		ID:     "user-3",
		Email:  "carol@example.com",
		Role:   "user",
		Status: "active",
	}
	st.serviceMap["allowed"] = db.Service{ID: "svc-allowed", Name: "AllowedApp", Subdomain: "allowed"}
	st.policyMap["svc-allowed"] = []string{"user", "admin"}
	sessionID := "sess-user-3"
	st.sessionMap[sessionID] = "user-3"

	gate := newTestGate(st)

	next := "https://allowed." + gateAuthDomain + "/home"
	req := httptest.NewRequest("GET", "/__burrow/login?next="+url.QueryEscape(next), nil)
	req.AddCookie(&http.Cookie{Name: "burrow_session", Value: sessionID})
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, req)

	// Should redirect directly to next since user's role IS in policy
	if rec.Code != http.StatusFound {
		t.Fatalf("want 302 redirect to next, got %d (body: %s)", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if loc != next {
		t.Errorf("want Location=%q, got %q", next, loc)
	}
}

// ---------------------------------------------------------------------------
// Test: POST /__burrow/logout → clear cookie + redirect to gate
// ---------------------------------------------------------------------------

func TestGatePostLogout_ClearsCookieAndRedirects(t *testing.T) {
	st, gate := newTestGateWithUser("user", "active")
	// Seed a session
	st.sessionMap["sess-abc"] = "user-1"

	req := httptest.NewRequest("POST", "/__burrow/logout", nil)
	req.RemoteAddr = "1.2.3.4:5678"
	req.AddCookie(&http.Cookie{Name: "burrow_session", Value: "sess-abc"})
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("want 302, got %d", rec.Code)
	}
	// Location should point back to the gate login page
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "/__burrow/login") {
		t.Errorf("want redirect to /__burrow/login, got %q", loc)
	}
	// Session should be deleted from store
	_, err := st.ValidateSession(context.Background(), "sess-abc")
	if err == nil {
		t.Error("session should have been deleted from store after logout")
	}
	// The response should set the cookie with MaxAge=-1 to clear it
	var clearedCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "burrow_session" {
			clearedCookie = c
			break
		}
	}
	if clearedCookie == nil {
		t.Fatal("want burrow_session cookie in response to clear it")
	}
	if clearedCookie.MaxAge != -1 {
		t.Errorf("clearing cookie: want MaxAge=-1, got %d", clearedCookie.MaxAge)
	}
}

// ---------------------------------------------------------------------------
// Helper
// ---------------------------------------------------------------------------

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
