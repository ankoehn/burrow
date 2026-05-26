package main

// e2e_security_test.go — cross-cutting security middleware tests.
// Uses bootSecurityStack (e2e_helpers_test.go) to spin up the API
// server alone, without the proxy/tunnel/client. Each test toggles
// the relevant Deps field via securityOpt.

import (
	"context"
	"encoding/json"
	"io"
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

// silence unused-import lints when later sub-tests are added in subsequent
// commits — io/json/context/time are imported up-front so this file stays
// edit-friendly for the next four TestSec_* tasks.
var (
	_ = context.Background
	_ = json.Unmarshal
	_ = io.ReadAll
	_ = time.Second
)
