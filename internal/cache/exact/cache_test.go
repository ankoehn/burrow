package exact

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ankoehn/burrow/internal/db"
)

// testCache opens a fresh sqlite DB, applies all migrations (so the
// v0.4.0 cache_entries table from 0004 exists), and returns the wired
// Cache engine.
func testCache(t *testing.T) *Cache {
	t.Helper()
	raw, err := db.Open(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Migrate(raw); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	d := db.Wrap(raw)
	t.Cleanup(func() { _ = d.Close() })
	return New(d, nil)
}

// TestCanonicaliseDeterministic asserts that two semantically-equal request
// bodies (whitespace differences + key reordering) collapse to the same
// canonical byte string. This is the property the cache key relies on.
func TestCanonicaliseDeterministic(t *testing.T) {
	in1 := CanonicaliseInput{
		Method:       "POST",
		Scheme:       "HTTPS",
		Host:         "api.example.com",
		Path:         "/v1/chat",
		APIKeyHeader: "Authorization",
		Headers: map[string][]string{
			"Content-Type":      {"application/json"},
			"Accept":            {"application/json"},
			"Anthropic-Version": {"2023-06-01"},
			"Authorization":     {"Bearer secret-1"},
			"X-Request-ID":      {"abc"}, // not allowlisted — dropped
		},
		Body: []byte(`{"model":"sonnet","messages":[{"role":"user","content":"hi"}],"stream":false,"n":1}`),
	}
	in2 := CanonicaliseInput{
		Method:       "post", // lower-cased
		Scheme:       "https",
		Host:         "API.example.com", // host casing differs
		Path:         "/v1/chat",
		APIKeyHeader: "Authorization",
		Headers: map[string][]string{
			"anthropic-version": {"2023-06-01"}, // header-name casing differs
			"accept":            {"application/json"},
			"content-type":      {"application/json"},
			"authorization":     {"Bearer secret-2"}, // different key — still excluded
			"x-trace":           {"xyz"},
		},
		// Same body but with: whitespace, key reordering, the excluded keys
		// dropped (`stream`, `n`), and the api-key-header name dropped as a
		// body key just in case.
		Body: []byte(`  {  "messages" : [ {"content":"hi","role":"user"} ] , "model":"sonnet"  }  `),
	}
	c1 := Canonicalise(in1)
	c2 := Canonicalise(in2)
	if string(c1) != string(c2) {
		t.Fatalf("Canonicalise not deterministic:\n  in1 → %q\n  in2 → %q", c1, c2)
	}

	// And: hashing yields the same hex.
	if HashKey(c1) != HashKey(c2) {
		t.Fatalf("HashKey mismatch: %s vs %s", HashKey(c1), HashKey(c2))
	}

	// Different body → different key.
	in3 := in1
	in3.Body = []byte(`{"model":"sonnet","messages":[{"role":"user","content":"different"}]}`)
	if HashKey(Canonicalise(in1)) == HashKey(Canonicalise(in3)) {
		t.Fatal("different payloads must yield different cache keys")
	}

	// The Authorization (api-key) header MUST NOT appear anywhere in the
	// canonical bytes — that's what lets applies_per: global share entries
	// across keys.
	if strings.Contains(string(c1), "secret-1") || strings.Contains(string(c1), "authorization") {
		t.Fatalf("api-key header leaked into canonical bytes:\n%s", c1)
	}
}

// TestLookupMissThenStoreHit walks the basic cache lifecycle: miss → store
// → hit (returns the same body) → ttl-expired → miss again.
func TestLookupMissThenStoreHit(t *testing.T) {
	ctx := context.Background()
	c := testCache(t)

	key := "global:abcd1234"
	// Miss on empty cache.
	if _, hit, err := c.Lookup(ctx, key); err != nil || hit {
		t.Fatalf("empty Lookup: hit=%v err=%v want (false, nil)", hit, err)
	}

	// Store a 200 response.
	ent := Entry{
		Body:       []byte(`{"id":"resp-1"}`),
		Status:     200,
		Headers:    map[string]string{"Content-Type": "application/json"},
		CreatedAt:  time.Now().UTC(),
		TTLSeconds: 3600,
	}
	if err := c.Store(ctx, key, ent); err != nil {
		t.Fatalf("Store: %v", err)
	}

	// Hit returns the same body + status.
	got, hit, err := c.Lookup(ctx, key)
	if err != nil || !hit {
		t.Fatalf("after Store: hit=%v err=%v want (true, nil)", hit, err)
	}
	if string(got.Body) != string(ent.Body) || got.Status != ent.Status {
		t.Fatalf("Lookup roundtrip mismatch: got %+v want %+v", got, ent)
	}
	if got.Headers["Content-Type"] != "application/json" {
		t.Fatalf("Lookup headers: %+v", got.Headers)
	}

	// Hit counter incremented.
	if c.hits.Load() != 1 || c.misses.Load() != 1 {
		t.Fatalf("counters: hits=%d misses=%d want 1,1", c.hits.Load(), c.misses.Load())
	}
}

// TestTTLExpiry asserts that an entry whose created_at + ttl < now is not
// returned (Lookup → miss). SQLite-side datetime arithmetic is what we're
// validating end-to-end.
func TestTTLExpiry(t *testing.T) {
	ctx := context.Background()
	c := testCache(t)

	key := "global:expiring"
	// Store with a created_at well in the past + tiny TTL so it is
	// guaranteed-expired on the next Lookup, deterministically.
	if err := c.Store(ctx, key, Entry{
		Body:       []byte(`{"old":"resp"}`),
		Status:     200,
		Headers:    map[string]string{"Content-Type": "application/json"},
		CreatedAt:  time.Now().UTC().Add(-2 * time.Hour),
		TTLSeconds: 60, // 1 minute → expired ~119 minutes ago
	}); err != nil {
		t.Fatalf("Store: %v", err)
	}

	if _, hit, err := c.Lookup(ctx, key); err != nil || hit {
		t.Fatalf("expired Lookup: hit=%v err=%v want (false, nil)", hit, err)
	}
}

// TestClearScope asserts that Clear("") wipes everything and Clear("scope")
// only deletes rows whose scope_key matches the prefix.
func TestClearScope(t *testing.T) {
	ctx := context.Background()
	c := testCache(t)
	mustStore := func(key string) {
		if err := c.Store(ctx, key, Entry{
			Body: []byte("x"), Status: 200,
			Headers:   map[string]string{"Content-Type": "application/json"},
			CreatedAt: time.Now().UTC(), TTLSeconds: 3600,
		}); err != nil {
			t.Fatalf("Store %s: %v", key, err)
		}
	}
	mustStore("global:a")
	mustStore("endpoint:svc1:/foo:b")
	mustStore("endpoint:svc1:/bar:c")
	mustStore("endpoint:svc2:/baz:d")

	// Targeted clear on one service's endpoint scope.
	if err := c.Clear(ctx, "endpoint:svc1:"); err != nil {
		t.Fatalf("Clear scope: %v", err)
	}
	// global:a survives.
	if _, hit, _ := c.Lookup(ctx, "global:a"); !hit {
		t.Fatal("global:a wrongly cleared")
	}
	// endpoint:svc2 survives.
	if _, hit, _ := c.Lookup(ctx, "endpoint:svc2:/baz:d"); !hit {
		t.Fatal("endpoint:svc2 wrongly cleared")
	}
	// endpoint:svc1 entries gone.
	if _, hit, _ := c.Lookup(ctx, "endpoint:svc1:/foo:b"); hit {
		t.Fatal("endpoint:svc1:/foo not cleared")
	}

	// Now full clear.
	if err := c.Clear(ctx, ""); err != nil {
		t.Fatalf("Clear all: %v", err)
	}
	entries, _, _, err := c.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if entries != 0 {
		t.Fatalf("after Clear(\"\"): entries=%d want 0", entries)
	}
}

// TestSettingsFromJSONRejectsUnknownAppliesPer asserts the engine-level
// validation the handler reuses.
func TestSettingsFromJSONRejectsUnknownAppliesPer(t *testing.T) {
	_, err := SettingsFromJSON([]byte(`{"enabled":true,"applies_per":"per_user","ttl_seconds":60,"max_entries":10,"max_per_entry_kb":10}`))
	if err == nil {
		t.Fatal("SettingsFromJSON accepted unknown applies_per")
	}
	if !strings.Contains(err.Error(), "applies_per") {
		t.Fatalf("error should mention applies_per: %v", err)
	}
	// And valid values pass.
	for _, v := range []string{"global", "per_endpoint", "per_api_key"} {
		body := []byte(`{"enabled":true,"applies_per":"` + v + `","ttl_seconds":60,"max_entries":10,"max_per_entry_kb":10}`)
		if _, err := SettingsFromJSON(body); err != nil {
			t.Fatalf("SettingsFromJSON(%q): %v", v, err)
		}
	}
}

// TestStats reports the entries + on_disk_bytes from the DB and computes
// hit_rate from in-process counters.
func TestStats(t *testing.T) {
	ctx := context.Background()
	c := testCache(t)

	// Empty: 0 entries, 0 bytes, 0.0 rate.
	entries, bytes, rate, err := c.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats empty: %v", err)
	}
	if entries != 0 || bytes != 0 || rate != 0 {
		t.Fatalf("empty stats: entries=%d bytes=%d rate=%f", entries, bytes, rate)
	}

	body := []byte(`{"data":"some response"}`)
	if err := c.Store(ctx, "global:s1", Entry{
		Body: body, Status: 200,
		Headers:   map[string]string{"Content-Type": "application/json"},
		CreatedAt: time.Now().UTC(), TTLSeconds: 3600,
	}); err != nil {
		t.Fatal(err)
	}
	// Two lookups → one hit, one miss → 0.5
	_, _, _ = c.Lookup(ctx, "global:s1")
	_, _, _ = c.Lookup(ctx, "global:missing")

	entries, bytes, rate, err = c.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if entries != 1 {
		t.Fatalf("entries=%d want 1", entries)
	}
	if bytes != int64(len(body)) {
		t.Fatalf("bytes=%d want %d", bytes, len(body))
	}
	if rate < 0.49 || rate > 0.51 {
		t.Fatalf("rate=%f want ~0.5", rate)
	}
}
