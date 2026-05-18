package api

import (
	"net/http"
	"strings"
	"testing"
)

// TestLoginRateLimitPerIP verifies that the per-IP rate limiter on
// POST /api/v1/auth/login returns HTTP 429 after the configured limit and
// that the first N requests pass through to the handler (receiving 401 or
// 200, not 429).
func TestLoginRateLimitPerIP(t *testing.T) {
	const perIP = 3 // inject a tiny per-IP limit for the test
	au := &authUsers{verify: func(_, _ string) (bool, error) { return false, nil }}
	ts := newTestServer(Deps{
		Users:                        au,
		Log:                          discardLog(),
		LoginRateLimitPerIPOverride:  perIP,
		LoginRateLimitGlobalOverride: 1000, // don't let global fire
	})
	defer ts.Close()

	body := strings.NewReader(`{"email":"a@x","password":"bad"}`)

	// First perIP requests must reach the handler (expect 401 invalid-creds).
	for i := 0; i < perIP; i++ {
		body = strings.NewReader(`{"email":"a@x","password":"bad"}`)
		resp, err := http.Post(ts.URL+"/api/v1/auth/login", "application/json", body)
		if err != nil {
			t.Fatalf("request %d: %v", i+1, err)
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests {
			t.Fatalf("request %d: got 429 before limit exhausted (limit=%d)", i+1, perIP)
		}
	}

	// Request perIP+1 must be rate-limited (429).
	body = strings.NewReader(`{"email":"a@x","password":"bad"}`)
	resp, err := http.Post(ts.URL+"/api/v1/auth/login", "application/json", body)
	if err != nil {
		t.Fatalf("over-limit request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("want 429 on over-limit login, got %d", resp.StatusCode)
	}
	b := readBody(t, resp)
	if !strings.Contains(b, `"error"`) {
		t.Fatalf("429 body must contain JSON error field, got %q", b)
	}
}

// TestLoginRateLimitGlobal verifies that the global endpoint cap fires when
// the per-IP limit is high but the global limit is crossed.
func TestLoginRateLimitGlobal(t *testing.T) {
	const global = 3
	au := &authUsers{verify: func(_, _ string) (bool, error) { return false, nil }}
	ts := newTestServer(Deps{
		Users:                        au,
		Log:                          discardLog(),
		LoginRateLimitPerIPOverride:  1000, // don't let per-IP fire
		LoginRateLimitGlobalOverride: global,
	})
	defer ts.Close()

	for i := 0; i < global; i++ {
		body := strings.NewReader(`{"email":"a@x","password":"bad"}`)
		resp, err := http.Post(ts.URL+"/api/v1/auth/login", "application/json", body)
		if err != nil {
			t.Fatalf("request %d: %v", i+1, err)
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests {
			t.Fatalf("request %d: got 429 before global limit exhausted (global=%d)", i+1, global)
		}
	}

	body := strings.NewReader(`{"email":"a@x","password":"bad"}`)
	resp, err := http.Post(ts.URL+"/api/v1/auth/login", "application/json", body)
	if err != nil {
		t.Fatalf("over-limit request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("want 429 on over-global-limit login, got %d", resp.StatusCode)
	}
}

// TestLoginRateLimitDoesNotAffectOtherEndpoints asserts that the login rate
// limiter is scoped only to POST /auth/login and has no effect on other
// routes. We send many requests to GET /api/v1/me (a session-protected route)
// and confirm none receive 429.
func TestLoginRateLimitDoesNotAffectOtherEndpoints(t *testing.T) {
	const perIP = 2 // very low login limit to prove scoping
	au := &authUsers{verify: func(_, _ string) (bool, error) { return true, nil }}
	ts := newTestServer(Deps{
		Users:                        au,
		Log:                          discardLog(),
		LoginRateLimitPerIPOverride:  perIP,
		LoginRateLimitGlobalOverride: perIP,
	})
	defer ts.Close()

	// Log in once to get a session cookie.
	jar, _ := cookiejarNew()
	cl := &http.Client{Jar: jar}
	loginResp, err := cl.Post(ts.URL+"/api/v1/auth/login", "application/json",
		strings.NewReader(`{"email":"a@x","password":"pw"}`))
	if err != nil {
		t.Fatal(err)
	}
	loginResp.Body.Close()
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("login: want 200, got %d", loginResp.StatusCode)
	}

	// Now call GET /api/v1/me many more times than perIP — none should be 429.
	const attempts = 10
	for i := 0; i < attempts; i++ {
		resp, err := cl.Get(ts.URL + "/api/v1/me")
		if err != nil {
			t.Fatalf("me request %d: %v", i+1, err)
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests {
			t.Fatalf("GET /api/v1/me request %d returned 429 — login limiter must NOT affect other endpoints", i+1)
		}
	}
}
