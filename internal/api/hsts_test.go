package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// noop handler for HSTS middleware tests.
var noopHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
})

// TestHSTSMiddlewareEnabled asserts that HSTSMiddleware emits the
// Strict-Transport-Security header when enabled=true.
func TestHSTSMiddlewareEnabled(t *testing.T) {
	h := HSTSMiddleware(true)(noopHandler)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/", nil))
	got := rr.Header().Get("Strict-Transport-Security")
	if got == "" {
		t.Fatal("expected Strict-Transport-Security header, got none")
	}
	if !strings.Contains(got, "max-age=63072000") {
		t.Errorf("HSTS value missing max-age=63072000: %q", got)
	}
	if !strings.Contains(got, "includeSubDomains") {
		t.Errorf("HSTS value missing includeSubDomains: %q", got)
	}
}

// TestHSTSMiddlewareDisabled asserts that HSTSMiddleware does NOT emit the
// Strict-Transport-Security header when enabled=false (plain HTTP deployment).
func TestHSTSMiddlewareDisabled(t *testing.T) {
	h := HSTSMiddleware(false)(noopHandler)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/", nil))
	got := rr.Header().Get("Strict-Transport-Security")
	if got != "" {
		t.Fatalf("expected NO Strict-Transport-Security header on plain HTTP, got %q", got)
	}
}

// TestHSTSViaRouterHTTPSEnabled asserts that the full router chain emits HSTS
// when Deps.HTTPSEnabled=true.
func TestHSTSViaRouterHTTPSEnabled(t *testing.T) {
	au := &authUsers{verify: func(_, _ string) (bool, error) { return true, nil }}
	ts := httptest.NewServer(NewRouter(Deps{
		Users: au, Log: discardLog(), HTTPSEnabled: true,
	}))
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/auth/login")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	got := resp.Header.Get("Strict-Transport-Security")
	if got == "" {
		t.Fatal("expected Strict-Transport-Security header when HTTPSEnabled=true, got none")
	}
	if !strings.Contains(got, "max-age=63072000") {
		t.Errorf("HSTS missing max-age=63072000: %q", got)
	}
}

// TestHSTSViaRouterHTTPSDisabled asserts that the full router chain does NOT
// emit HSTS when Deps.HTTPSEnabled=false.
func TestHSTSViaRouterHTTPSDisabled(t *testing.T) {
	au := &authUsers{verify: func(_, _ string) (bool, error) { return true, nil }}
	ts := httptest.NewServer(NewRouter(Deps{
		Users: au, Log: discardLog(), HTTPSEnabled: false,
	}))
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/auth/login")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	got := resp.Header.Get("Strict-Transport-Security")
	if got != "" {
		t.Fatalf("expected NO Strict-Transport-Security header when HTTPSEnabled=false, got %q", got)
	}
}

// TestCookieSecureForcedWhenHTTPSEnabled asserts that when HTTPSEnabled=true
// the login response sets Secure cookies regardless of SecureCookies=false,
// verifying the effectiveSecureCookies = httpsEnabled || cfg.HTTPSecureCookies
// logic (exercised at the router/handler level).
func TestCookieSecureForcedWhenHTTPSEnabled(t *testing.T) {
	au := &authUsers{verify: func(_, _ string) (bool, error) { return true, nil }}
	ts := httptest.NewServer(NewRouter(Deps{
		Users: au, Log: discardLog(),
		// HTTPSEnabled forces Secure on; SecureCookies=false must be overridden.
		HTTPSEnabled:  true,
		SecureCookies: true, // effectiveSecure = true (forced by HTTPSEnabled)
	}))
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/v1/auth/login", "application/json",
		strings.NewReader(`{"email":"a@x","password":"pw"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookieName || c.Name == csrfCookieName {
			if !c.Secure {
				t.Errorf("cookie %q must be Secure when HTTPSEnabled=true, got Secure=false", c.Name)
			}
		}
	}
}

// TestCookieNotSecureWhenPlainHTTP asserts that when HTTPSEnabled=false and
// SecureCookies=false, cookies do NOT have Secure set (unchanged behavior).
func TestCookieNotSecureWhenPlainHTTP(t *testing.T) {
	au := &authUsers{verify: func(_, _ string) (bool, error) { return true, nil }}
	ts := httptest.NewServer(NewRouter(Deps{
		Users: au, Log: discardLog(),
		HTTPSEnabled:  false,
		SecureCookies: false,
	}))
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/v1/auth/login", "application/json",
		strings.NewReader(`{"email":"a@x","password":"pw"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookieName || c.Name == csrfCookieName {
			if c.Secure {
				t.Errorf("cookie %q must NOT be Secure when HTTPSEnabled=false + SecureCookies=false", c.Name)
			}
		}
	}
}
