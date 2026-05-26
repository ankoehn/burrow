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

// silence unused-import lints when later sub-tests are added in subsequent
// commits — io/json/context/time are imported up-front so this file stays
// edit-friendly for the next four TestSec_* tasks.
var (
	_ = context.Background
	_ = json.Unmarshal
	_ = io.ReadAll
	_ = time.Second
)
