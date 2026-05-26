package main

// e2e_security_test.go — cross-cutting security middleware tests.
// Uses bootSecurityStack (e2e_helpers_test.go) to spin up the API
// server alone, without the proxy/tunnel/client. Each test toggles
// the relevant Deps field via securityOpt.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestSec_HSTSHeader(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e in -short")
	}

	t.Run("HTTPS_on_emits_HSTS", func(t *testing.T) {
		s := bootSecurityStack(t, withSecHTTPS())
		resp, err := s.client().Get(s.apiURL + "/healthz")
		if err != nil {
			t.Fatalf("GET /healthz: %v", err)
		}
		defer resp.Body.Close()
		if got := resp.Header.Get("Strict-Transport-Security"); got == "" {
			t.Fatal("HSTS header missing on HTTPS server")
		} else if !strings.Contains(got, "max-age=") {
			t.Errorf("HSTS missing max-age: %q", got)
		}
	})

	t.Run("HTTPS_off_no_HSTS", func(t *testing.T) {
		s := bootSecurityStack(t) // no withSecHTTPS
		resp, err := s.client().Get(s.apiURL + "/healthz")
		if err != nil {
			t.Fatalf("GET /healthz: %v", err)
		}
		defer resp.Body.Close()
		if got := resp.Header.Get("Strict-Transport-Security"); got != "" {
			t.Errorf("HSTS leaked on plaintext server: %q", got)
		}
	})
}

func TestSec_CookieFlags(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e in -short")
	}

	type cookieCheck struct {
		secureCookies bool
	}
	cases := []cookieCheck{
		{secureCookies: true},
		{secureCookies: false},
	}
	for _, tc := range cases {
		name := "secureCookies=false"
		if tc.secureCookies {
			name = "secureCookies=true"
		}
		t.Run(name, func(t *testing.T) {
			s := bootSecurityStack(t, withSecCookies(tc.secureCookies))
			c := s.client()
			body := strings.NewReader(`{"email":"` + e2eAdminEmail + `","password":"` + e2eAdminPassword + `"}`)
			resp, err := c.Post(s.apiURL+"/api/v1/auth/login", "application/json", body)
			if err != nil {
				t.Fatalf("POST /auth/login: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
				rb, _ := io.ReadAll(resp.Body)
				t.Fatalf("login status: want 204/200, got %d body=%s", resp.StatusCode, rb)
			}

			var session, csrf *http.Cookie
			for _, ck := range resp.Cookies() {
				switch ck.Name {
				case "burrow_session":
					session = ck
				case "burrow_csrf":
					csrf = ck
				}
			}
			if session == nil {
				t.Fatal("burrow_session cookie missing")
			}
			if csrf == nil {
				t.Fatal("burrow_csrf cookie missing")
			}

			if !session.HttpOnly {
				t.Error("burrow_session: HttpOnly missing")
			}
			if session.SameSite != http.SameSiteLaxMode {
				t.Errorf("burrow_session: SameSite want Lax, got %v", session.SameSite)
			}
			if session.Secure != tc.secureCookies {
				t.Errorf("burrow_session: Secure want %v, got %v", tc.secureCookies, session.Secure)
			}

			if csrf.HttpOnly {
				t.Error("burrow_csrf: HttpOnly must NOT be set (SPA needs to read)")
			}
			if csrf.SameSite != http.SameSiteLaxMode {
				t.Errorf("burrow_csrf: SameSite want Lax, got %v", csrf.SameSite)
			}
			if csrf.Secure != tc.secureCookies {
				t.Errorf("burrow_csrf: Secure want %v, got %v", tc.secureCookies, csrf.Secure)
			}
		})
	}
}

func TestSec_CSRFRejection(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e in -short")
	}
	s := bootSecurityStack(t)
	c := s.client()

	// 1. Log in to obtain both cookies.
	body := strings.NewReader(`{"email":"` + e2eAdminEmail + `","password":"` + e2eAdminPassword + `"}`)
	loginResp, err := c.Post(s.apiURL+"/api/v1/auth/login", "application/json", body)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	_ = loginResp.Body.Close()

	var csrfVal string
	for _, ck := range loginResp.Cookies() {
		if ck.Name == "burrow_csrf" {
			csrfVal = ck.Value
		}
	}
	if csrfVal == "" {
		t.Fatal("burrow_csrf not set on login response")
	}

	// 2. POST /auth/logout WITHOUT the X-CSRF-Token header → 403.
	req, _ := http.NewRequest("POST", s.apiURL+"/api/v1/auth/logout", nil)
	r, err := c.Do(req)
	if err != nil {
		t.Fatalf("logout (no csrf): %v", err)
	}
	rb, _ := io.ReadAll(r.Body)
	_ = r.Body.Close()
	if r.StatusCode != http.StatusForbidden {
		t.Fatalf("POST logout no-csrf: want 403, got %d body=%s", r.StatusCode, rb)
	}
	if !strings.Contains(strings.ToLower(string(rb)), "csrf") {
		t.Errorf("403 body should mention csrf, got %q", rb)
	}

	// 3. POST same with mismatched header → 403.
	req2, _ := http.NewRequest("POST", s.apiURL+"/api/v1/auth/logout", nil)
	req2.Header.Set("X-CSRF-Token", "wrong-value")
	r2, err := c.Do(req2)
	if err != nil {
		t.Fatalf("logout (wrong csrf): %v", err)
	}
	_ = r2.Body.Close()
	if r2.StatusCode != http.StatusForbidden {
		t.Errorf("POST logout wrong-csrf: want 403, got %d", r2.StatusCode)
	}

	// 4. GET /me WITHOUT csrf → 200 (safe method bypass).
	getReq, _ := http.NewRequest("GET", s.apiURL+"/api/v1/me", nil)
	rg, err := c.Do(getReq)
	if err != nil {
		t.Fatalf("GET /me: %v", err)
	}
	_ = rg.Body.Close()
	if rg.StatusCode != http.StatusOK {
		t.Errorf("GET /me: want 200, got %d", rg.StatusCode)
	}

	// 5. POST same with matching csrf → 204.
	req3, _ := http.NewRequest("POST", s.apiURL+"/api/v1/auth/logout", nil)
	req3.Header.Set("X-CSRF-Token", csrfVal)
	r3, err := c.Do(req3)
	if err != nil {
		t.Fatalf("logout (matched csrf): %v", err)
	}
	_ = r3.Body.Close()
	if r3.StatusCode != http.StatusNoContent {
		t.Errorf("POST logout matched-csrf: want 204, got %d", r3.StatusCode)
	}
}

func TestSec_LoginRateLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e in -short")
	}
	// Use limit=3 to keep the test fast.
	s := bootSecurityStack(t, withSecLoginRateLimit(3, 100))
	c := s.client()

	post := func() (status int, body string) {
		r, err := c.Post(s.apiURL+"/api/v1/auth/login", "application/json",
			strings.NewReader(`{"email":"nobody@x","password":"wrong"}`))
		if err != nil {
			t.Fatalf("post login: %v", err)
		}
		rb, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		return r.StatusCode, string(rb)
	}

	// First 3 attempts should be 401 (rate-limit allows them through).
	for i := 0; i < 3; i++ {
		s, b := post()
		if s != http.StatusUnauthorized {
			t.Fatalf("attempt %d: want 401, got %d body=%s", i+1, s, b)
		}
	}

	// 4th attempt should be 429.
	s4, b4 := post()
	if s4 != http.StatusTooManyRequests {
		t.Fatalf("attempt 4: want 429, got %d body=%s", s4, b4)
	}
	if !strings.Contains(strings.ToLower(b4), "too many") {
		t.Errorf("429 body should say 'too many', got %q", b4)
	}
}

// silence unused-import lints when later sub-tests are added in subsequent
// commits — io/json/context/time are imported up-front so this file stays
// edit-friendly for the next four TestSec_* tasks.
var (
	_ = context.Background
	_ = json.Unmarshal
	_ = io.ReadAll
	_ = time.Second
)
