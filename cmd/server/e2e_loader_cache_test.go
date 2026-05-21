package main

// e2e_loader_cache_test.go — Deferral D1 closure: proves that wiring
// aigw.Chain.Loader from the service_ai_config DB row produces the
// expected cache HIT short-circuit on the second identical request.
//
// Test shape:
//   - boot full e2e stack (server + client + proxy + AIChain w/ Loader),
//   - seed service into api_key mode with a freshly minted plaintext key,
//   - seed a service_ai_config row enabling cache (per_endpoint, 5min TTL),
//   - stand up a tiny counting upstream,
//   - send two identical POST /v1/chat/completions requests with the same
//     Authorization: Bearer header,
//   - assert: first → upstream count 1, 200 OK;
//             second → upstream count STILL 1, 200 OK,
//                      response carries Burrow-Cache: HIT.
//
// If Loader is nil (the pre-fix state) the chain pass-throughs and the
// upstream sees BOTH requests → upstream count == 2 → test fails.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestE2ELoader_CacheHitOnSecondRequest(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e in -short")
	}
	s := bootE2EStack(t)

	// Counting upstream — every request increments hits; body echoes a known
	// JSON payload with an explicit Content-Length (cacheable shape per
	// aigw.Chain — streamed responses must NOT be cached).
	var upstreamHits atomic.Int32
	respBody := []byte(`{"id":"chatcmpl-abc","choices":[{"message":{"role":"assistant","content":"cached"}}]}`)
	s.setUpstreamHandler(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		// Drain so the request body counter is exercised (the chain reads
		// the body before forwarding; without this drain the chain still
		// works, but draining makes the test mirror real-world behaviour).
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", itoa(len(respBody)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(respBody)
	})

	// Switch service to api_key mode so the proxy enforces a Bearer.
	must(t, s.store.SetServiceAccessMode(
		context.Background(), s.userID, "admin", s.serviceID, "api_key", "Authorization", nil),
		"SetServiceAccessMode(api_key)")
	_, plaintext, err := s.store.CreateAPIKey(
		context.Background(), s.userID, "admin", s.serviceID, "ci-cache")
	must(t, err, "CreateAPIKey")
	if !strings.HasPrefix(plaintext, "buk_") {
		t.Fatalf("plaintext key shape: want buk_*, got %q", plaintext)
	}

	// Seed service_ai_config row: cache enabled, per_endpoint scope.
	// The JSON shape mirrors what the cache_handlers + per_service handlers
	// write (outer object with a .cache sub-object whose body is the
	// exact.Settings JSON wire form).
	cfgBlob := `{
	  "cache": {
	    "enabled": true,
	    "applies_per": "per_endpoint",
	    "ttl_seconds": 300,
	    "max_entries": 100,
	    "max_per_entry_kb": 64
	  }
	}`
	if _, err := s.db.ExecContext(context.Background(),
		`INSERT INTO service_ai_config(service_id, config) VALUES(?, ?)`,
		s.serviceID, cfgBlob,
	); err != nil {
		t.Fatalf("seed service_ai_config: %v", err)
	}

	hc := s.visitorClient(t)
	target := "https://" + s.hostWithPort() + "/v1/chat/completions"

	mkReq := func() *http.Request {
		body := bytes.NewReader([]byte(`{"model":"gpt-4","messages":[{"role":"user","content":"hello"}]}`))
		req, err := http.NewRequest(http.MethodPost, target, body)
		if err != nil {
			t.Fatalf("new req: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+plaintext)
		return req
	}

	// --- First request: must reach upstream (cache MISS → store) -------
	r1, err := hc.Do(mkReq())
	must(t, err, "first request")
	b1 := readAllString(t, r1)
	if r1.StatusCode != http.StatusOK {
		t.Fatalf("first: want 200, got %d body=%s", r1.StatusCode, b1)
	}
	if !validJSON(b1) {
		t.Errorf("first response body must be valid JSON, got %q", b1)
	}
	if got := upstreamHits.Load(); got != 1 {
		t.Fatalf("first: upstream hits = %d, want 1", got)
	}
	// First request is a MISS — Burrow-Cache header is either absent
	// or explicitly "MISS"-equivalent (the chain does not stamp HIT on
	// the upstream's response). We do NOT assert the absence here; the
	// definitive check is the next request.

	// Small pause to ensure the cache store completed before the next
	// lookup. The cache.Store call is synchronous, but the DB roundtrip
	// shares the single sqlite conn — give scheduler a tick.
	time.Sleep(50 * time.Millisecond)

	// --- Second request: must hit cache, NOT upstream ------------------
	r2, err := hc.Do(mkReq())
	must(t, err, "second request")
	b2 := readAllString(t, r2)
	if r2.StatusCode != http.StatusOK {
		t.Fatalf("second: want 200, got %d body=%s", r2.StatusCode, b2)
	}
	if got := upstreamHits.Load(); got != 1 {
		t.Fatalf("second: upstream hits = %d, want 1 (cache should have HIT and short-circuited)", got)
	}
	if got := r2.Header.Get("Burrow-Cache"); got != "HIT" {
		t.Fatalf("second: Burrow-Cache header = %q, want HIT", got)
	}
	if b2 != string(respBody) {
		t.Errorf("second: body = %q, want %q (cached body must replay byte-for-byte)", b2, respBody)
	}
}

// itoa is a tiny base-10 strconv.Itoa avoidance (the rest of the file
// avoids strconv to keep imports minimal — the helper is small enough
// to inline).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	neg := n < 0
	if neg {
		n = -n
	}
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// validJSON returns true when b parses as JSON. Used to sanity-check the
// proxied body is not garbled — the body content is deliberately not
// asserted byte-for-byte on the first request (the upstream may add
// transport headers); the second request DOES assert byte-equality.
func validJSON(b string) bool {
	var v any
	return json.Unmarshal([]byte(b), &v) == nil
}
