package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestIsValidHTTPHeaderName pins the RFC 7230 token grammar enforced for
// service.api_key_header. The Playwright e2e suite surfaced "Authorization:
// Bearer" persisted via the UI — the colon and space made it an invalid
// HTTP header name and the proxy then never matched the request header.
func TestIsValidHTTPHeaderName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"empty", "", false},
		{"Authorization", "Authorization", true},
		{"X-Api-Key", "X-Api-Key", true},
		{"all-tchar", "!#$%&'*+-.^_`|~Aa0", true},
		{"colon-space-Bearer", "Authorization: Bearer", false},
		{"trailing-colon", "X-Api-Key:", false},
		{"with-space", "X Api Key", false},
		{"with-tab", "X-Api\tKey", false},
		{"with-newline", "X-Api-Key\n", false},
		{"with-equals", "X=Y", false},
		{"with-quote", `X-"key"`, false},
		{"with-semicolon", "X;Y", false},
		{"single-letter", "A", true},
		{"high-bit", "X-Föo", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isValidHTTPHeaderName(c.in); got != c.want {
				t.Fatalf("isValidHTTPHeaderName(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

// TestSetServiceAccessMode_APIKeyHeader_Validates verifies the handler
// rejects an api_key_header that isn't an RFC 7230 token (e.g. the
// "Authorization: Bearer" value previously stored via the dashboard).
func TestSetServiceAccessMode_APIKeyHeader_Validates(t *testing.T) {
	ss := &fakeServiceStore{}
	d := Deps{
		Users:    &fakeUserStore{role: "user"},
		Services: ss,
		Log:      discardLog(),
	}
	srv := httptest.NewServer(NewRouter(d))
	defer srv.Close()
	c := authedClient(t, srv)

	// Bad: contains colon + space.
	bad := map[string]string{
		"access_mode":    "api_key",
		"api_key_header": "Authorization: Bearer",
	}
	r := c.put(t, "/api/v1/services/svc-1/access-mode", bad)
	if r.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad header: status=%d body=%s", r.StatusCode, readBody(t, r))
	}
	if ss.lastMode != "" {
		t.Errorf("store was called despite invalid header: lastMode=%q", ss.lastMode)
	}

	// Good: plain token.
	good := map[string]string{
		"access_mode":    "api_key",
		"api_key_header": "X-Api-Key",
	}
	r2 := c.put(t, "/api/v1/services/svc-1/access-mode", good)
	if r2.StatusCode != http.StatusNoContent {
		t.Fatalf("good header: status=%d body=%s", r2.StatusCode, readBody(t, r2))
	}
	if ss.lastMode != "api_key" {
		t.Errorf("store not called with api_key: lastMode=%q", ss.lastMode)
	}

	// Empty: also OK (handler doesn't pass it through; store defaults).
	empty := map[string]string{
		"access_mode":    "api_key",
		"api_key_header": "",
	}
	r3 := c.put(t, "/api/v1/services/svc-1/access-mode", empty)
	if r3.StatusCode != http.StatusNoContent {
		t.Fatalf("empty header: status=%d body=%s", r3.StatusCode, readBody(t, r3))
	}
}
