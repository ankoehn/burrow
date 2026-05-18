package api

import (
	"net/http"
	"strings"
	"testing"
)

// csrfClient logs in and returns (client, csrfToken) ready for mutation tests.
func csrfClient(t *testing.T, ts *httptestServer) (*http.Client, string) {
	t.Helper()
	return loggedInClientWithCSRF(t, ts)
}

// TestCSRFMissingTokenBlocksCreateToken asserts that POST /api/v1/tokens with
// a valid session cookie but NO X-CSRF-Token header returns 403.
func TestCSRFMissingTokenBlocksCreateToken(t *testing.T) {
	u := &tokUsers{}
	u.verify = func(_, _ string) (bool, error) { return true, nil }
	ts := newTestServer(Deps{Users: u, Log: discardLog()})
	defer ts.Close()
	cl, _ := csrfClient(t, &httptestServer{ts}) // csrf token NOT sent

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/tokens", strings.NewReader(`{"name":"t"}`))
	req.Header.Set("Content-Type", "application/json")
	// deliberately omit X-CSRF-Token
	resp, err := cl.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("POST /tokens without CSRF header: want 403, got %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, `"error"`) {
		t.Fatalf("403 body must contain JSON error field, got %q", body)
	}
}

// TestCSRFWrongTokenBlocksCreateToken asserts that POST /api/v1/tokens with a
// valid session cookie but an INCORRECT X-CSRF-Token header returns 403.
func TestCSRFWrongTokenBlocksCreateToken(t *testing.T) {
	u := &tokUsers{}
	u.verify = func(_, _ string) (bool, error) { return true, nil }
	ts := newTestServer(Deps{Users: u, Log: discardLog()})
	defer ts.Close()
	cl, _ := csrfClient(t, &httptestServer{ts})

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/tokens", strings.NewReader(`{"name":"t"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", "definitely-wrong-token")
	resp, err := cl.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("POST /tokens with wrong CSRF header: want 403, got %d", resp.StatusCode)
	}
}

// TestCSRFCorrectTokenAllowsCreateToken asserts that POST /api/v1/tokens with
// a valid session cookie AND correct X-CSRF-Token header succeeds (201).
func TestCSRFCorrectTokenAllowsCreateToken(t *testing.T) {
	u := &tokUsers{}
	u.verify = func(_, _ string) (bool, error) { return true, nil }
	ts := newTestServer(Deps{Users: u, Log: discardLog()})
	defer ts.Close()
	cl, csrf := csrfClient(t, &httptestServer{ts})

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/tokens", strings.NewReader(`{"name":"t"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrf)
	resp, err := cl.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /tokens with correct CSRF: want 201, got %d", resp.StatusCode)
	}
}

// TestCSRFMissingTokenBlocksRevokeToken asserts that DELETE /api/v1/tokens/{id}
// with a valid session but no CSRF header returns 403.
func TestCSRFMissingTokenBlocksRevokeToken(t *testing.T) {
	u := &tokUsers{}
	u.verify = func(_, _ string) (bool, error) { return true, nil }
	ts := newTestServer(Deps{Users: u, Log: discardLog()})
	defer ts.Close()
	cl, _ := csrfClient(t, &httptestServer{ts})

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/tokens/t1", nil)
	// deliberately omit X-CSRF-Token
	resp, err := cl.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("DELETE /tokens/{id} without CSRF header: want 403, got %d", resp.StatusCode)
	}
}

// TestCSRFMissingTokenBlocksLogout asserts that POST /auth/logout with a
// valid session but no CSRF header returns 403.
func TestCSRFMissingTokenBlocksLogout(t *testing.T) {
	au := &authUsers{verify: func(_, _ string) (bool, error) { return true, nil }}
	ts := newTestServer(Deps{Users: au, Log: discardLog()})
	defer ts.Close()
	jar, _ := cookiejarNew()
	cl := &http.Client{Jar: jar}
	lr, _ := cl.Post(ts.URL+"/api/v1/auth/login", "application/json",
		strings.NewReader(`{"email":"a@x","password":"pw"}`))
	lr.Body.Close()

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/logout", nil)
	// deliberately omit X-CSRF-Token
	resp, err := cl.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("POST /auth/logout without CSRF header: want 403, got %d", resp.StatusCode)
	}
}

// TestCSRFGetEndpointsNotBlocked asserts that GET endpoints within the
// session-protected group (GET /me, GET /tokens, GET /tunnels) are NOT blocked
// by CSRF even without any X-CSRF-Token header.
func TestCSRFGetEndpointsNotBlocked(t *testing.T) {
	u := &tokUsers{}
	u.verify = func(_, _ string) (bool, error) { return true, nil }
	ts := newTestServer(Deps{Users: u, Log: discardLog()})
	defer ts.Close()
	// GET does not need the CSRF token — use the convenience helper.
	cl := loggedInClient(t, &httptestServer{ts})

	for _, path := range []string{"/api/v1/me", "/api/v1/tokens", "/api/v1/tunnels"} {
		resp, err := cl.Get(ts.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusForbidden {
			t.Fatalf("GET %s must NOT be blocked by CSRF (got 403)", path)
		}
	}
}

// TestCSRFSSENotBlocked asserts that the SSE GET /events endpoint is not
// blocked by CSRF (GET is a safe method; SSE clients cannot send custom headers
// after the connection upgrade).
func TestCSRFSSENotBlocked(t *testing.T) {
	u := &tokUsers{}
	u.verify = func(_, _ string) (bool, error) { return true, nil }
	ts := newTestServer(Deps{Users: u, Log: discardLog()})
	defer ts.Close()
	cl := loggedInClient(t, &httptestServer{ts})

	resp, err := cl.Get(ts.URL + "/api/v1/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// Without an Events bus wired the handler returns 500 (nil bus — see
	// TestEventsStreamNilEventsIs500), but it must never be 403.
	if resp.StatusCode == http.StatusForbidden {
		t.Fatalf("GET /api/v1/events must NOT be blocked by CSRF (got 403)")
	}
}

// TestCSRFUnauthenticatedGets401NotForbidden asserts that a request without a
// session cookie gets 401 (from RequireSession) rather than 403 (CSRF).
// This confirms RequireSession runs before RequireCSRF.
func TestCSRFUnauthenticatedGets401NotForbidden(t *testing.T) {
	u := &tokUsers{}
	u.verify = func(_, _ string) (bool, error) { return true, nil }
	ts := newTestServer(Deps{Users: u, Log: discardLog()})
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/tokens", strings.NewReader(`{"name":"t"}`))
	req.Header.Set("Content-Type", "application/json")
	// No session cookie, no CSRF header.
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated POST /tokens: want 401, got %d", resp.StatusCode)
	}
}
