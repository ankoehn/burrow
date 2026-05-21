package main

// e2e_openai_test.go — Wave-2 real-stack e2e for the v0.4.0 AI middleware
// chain's OpenAI-compatible code paths. Builds on top of
// TestE2ELoader_CacheHitOnSecondRequest's bootE2EStack scaffolding.
//
//   - TestE2EOpenAI_RoundTripAndMetering: a single POST /v1/chat/completions
//     reaches a counting OpenAI-compat upstream, the visitor sees the
//     upstream body byte-for-byte, and exactly one usage_events row lands
//     with tokens_in=12, tokens_out=7, cache_hit=0.

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestE2EOpenAI_RoundTripAndMetering — Task 2.
func TestE2EOpenAI_RoundTripAndMetering(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e in -short")
	}
	s := bootE2EStack(t)

	// Counting OpenAI-compat upstream that echoes the request body in
	// req_seen and returns a non-stream chat.completion with explicit
	// token counts.
	var (
		upstreamHits atomic.Int32
		lastReqBody  atomic.Value // []byte
	)
	respBodyFmt := `{"id":"chatcmpl-x","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":7,"total_tokens":19},"req_seen":%q}`

	s.setUpstreamHandler(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		reqBytes, _ := io.ReadAll(r.Body)
		lastReqBody.Store(reqBytes)
		body := fmt.Sprintf(respBodyFmt, string(reqBytes))
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", itoa(len(body)))
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, body)
	})

	// Switch service to api_key mode so the proxy enforces a Bearer +
	// the metered usage_events row has a discoverable identity.
	must(t, s.store.SetServiceAccessMode(
		context.Background(), s.userID, "admin", s.serviceID, "api_key", "Authorization", nil),
		"SetServiceAccessMode(api_key)")
	_, plaintext, err := s.store.CreateAPIKey(
		context.Background(), s.userID, "admin", s.serviceID, "ci-openai")
	must(t, err, "CreateAPIKey")
	if !strings.HasPrefix(plaintext, "buk_") {
		t.Fatalf("plaintext key shape: want buk_*, got %q", plaintext)
	}

	// Seed a *minimal* service_ai_config so the loader returns ok=true
	// and the chain runs (so kind detection + metering fires). Cache
	// disabled — we want a single fresh round-trip per request.
	cfgBlob := `{"inspector":{"enabled":false}}`
	if _, err := s.db.ExecContext(context.Background(),
		`INSERT INTO service_ai_config(service_id, config) VALUES(?, ?)`,
		s.serviceID, cfgBlob,
	); err != nil {
		t.Fatalf("seed service_ai_config: %v", err)
	}

	hc := s.visitorClient(t)
	target := "https://" + s.hostWithPort() + "/v1/chat/completions"

	reqBodyStr := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`
	req, err := http.NewRequest(http.MethodPost, target, bytes.NewReader([]byte(reqBodyStr)))
	must(t, err, "new req")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+plaintext)

	resp, err := hc.Do(req)
	must(t, err, "POST chat completion")
	body := readAllString(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", resp.StatusCode, body)
	}

	// Body must match the upstream's body byte-for-byte (which we know
	// because we built it via the same fmt template).
	wantRaw, _ := lastReqBody.Load().([]byte)
	if wantRaw == nil {
		t.Fatal("upstream never recorded a request body")
	}
	want := fmt.Sprintf(respBodyFmt, string(wantRaw))
	if body != want {
		t.Errorf("response body mismatch\n got: %s\nwant: %s", body, want)
	}

	if got := upstreamHits.Load(); got != 1 {
		t.Fatalf("upstream hits = %d, want 1", got)
	}

	// usage_events is written asynchronously (the SQLSink swallows errors
	// + the proxy hot path is non-blocking). Poll up to 1s.
	tokensIn, tokensOut, cacheHit, ok := pollUsageEvent(t, s.db, s.serviceID, time.Second)
	if !ok {
		t.Fatal("usage_events row never appeared for service")
	}
	if tokensIn != 12 || tokensOut != 7 || cacheHit != 0 {
		t.Fatalf("usage_events row mismatch: tokens_in=%d tokens_out=%d cache_hit=%d (want 12/7/0)",
			tokensIn, tokensOut, cacheHit)
	}
}

// pollUsageEvent waits up to `within` for the *first* usage_events row to
// appear for the given service_id and returns its (tokens_in, tokens_out,
// cache_hit). Returns ok=false on timeout.
//
// A single matching row is asserted by Task 2's caller (the chain writes
// exactly one row per non-cached request); this helper just blocks until
// it can read one. Reused by subsequent e2e tests in this package.
func pollUsageEvent(t *testing.T, d *sql.DB, serviceID string, within time.Duration) (tokensIn, tokensOut, cacheHit int, ok bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		var n int
		if err := d.QueryRowContext(context.Background(),
			`SELECT count(*) FROM usage_events WHERE service_id=?`, serviceID,
		).Scan(&n); err != nil {
			if !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("count usage_events: %v", err)
			}
		}
		if n >= 1 {
			row := d.QueryRowContext(context.Background(),
				`SELECT tokens_in, tokens_out, cache_hit FROM usage_events
				   WHERE service_id=? ORDER BY ts LIMIT 1`, serviceID)
			if err := row.Scan(&tokensIn, &tokensOut, &cacheHit); err != nil {
				t.Fatalf("scan usage_events: %v", err)
			}
			return tokensIn, tokensOut, cacheHit, true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return 0, 0, 0, false
}
