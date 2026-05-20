package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/ankoehn/burrow/internal/db"
)

// fakeCacheEngine is the in-memory CacheEngine stand-in for the handler
// tests. It tracks Clear calls and reports configurable stats.
type fakeCacheEngine struct {
	mu          sync.Mutex
	clearCalls  []string // scope arg of each Clear()
	entries     int
	onDiskBytes int64
	hitRate     float64
	statsErr    error
	clearErr    error
}

func (f *fakeCacheEngine) Clear(_ context.Context, scope string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clearCalls = append(f.clearCalls, scope)
	return f.clearErr
}
func (f *fakeCacheEngine) Stats(_ context.Context) (int, int64, float64, error) {
	return f.entries, f.onDiskBytes, f.hitRate, f.statsErr
}

// fakeCacheServiceLookup is the CacheServiceLookup stand-in. Configurable
// per-test owner map + AI-config rows.
type fakeCacheServiceLookup struct {
	owners  map[string]string // serviceID → userID
	configs map[string][]byte // serviceID → raw json blob
	list    []CacheServiceConfigRow
}

func (f *fakeCacheServiceLookup) GetServiceOwner(_ context.Context, id string) (string, error) {
	u, ok := f.owners[id]
	if !ok {
		return "", db.ErrNotFound
	}
	return u, nil
}
func (f *fakeCacheServiceLookup) GetServiceAIConfig(_ context.Context, id string) ([]byte, error) {
	b, ok := f.configs[id]
	if !ok {
		return nil, db.ErrNotFound
	}
	return b, nil
}
func (f *fakeCacheServiceLookup) ListAllServiceAIConfigs(_ context.Context) ([]CacheServiceConfigRow, error) {
	return f.list, nil
}

// fakeCacheSettingsStore is a simple in-memory map[string]string the
// settings handlers read/write.
type fakeCacheSettingsStore struct {
	saved map[string]string
}

func (f *fakeCacheSettingsStore) GetSettings(context.Context) (map[string]string, error) {
	if f.saved == nil {
		return map[string]string{}, nil
	}
	return f.saved, nil
}
func (f *fakeCacheSettingsStore) SaveSettings(_ context.Context, kv map[string]string) error {
	if f.saved == nil {
		f.saved = map[string]string{}
	}
	for k, v := range kv {
		f.saved[k] = v
	}
	return nil
}
func (f *fakeCacheSettingsStore) SendTestEmail(_ context.Context, _ string) error { return nil }

// TestCachePutSettingsRejectsUnknownAppliesPer asserts the spec-mandated 400
// from PUT /api/v1/cache/settings when applies_per is not in the closed enum.
func TestCachePutSettingsRejectsUnknownAppliesPer(t *testing.T) {
	d := Deps{
		Log:      discardLog(),
		Users:    &fakeUserStore{role: "admin"},
		Settings: &fakeCacheSettingsStore{},
	}
	srv := httptest.NewServer(NewRouter(d))
	defer srv.Close()
	c := authedClient(t, srv)

	r := c.put(t, "/api/v1/cache/settings", map[string]any{
		"enabled":          true,
		"applies_per":      "per_user", // not in the enum
		"ttl_seconds":      3600,
		"max_entries":      1000,
		"max_per_entry_kb": 512,
	})
	if r.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d want 400", r.StatusCode)
	}
	body := readBody(t, r)
	if body == "" {
		t.Fatal("expected non-empty error body")
	}

	// And a valid applies_per saves successfully (204).
	r = c.put(t, "/api/v1/cache/settings", map[string]any{
		"enabled":          true,
		"applies_per":      "per_endpoint",
		"ttl_seconds":      60,
		"max_entries":      100,
		"max_per_entry_kb": 32,
	})
	if r.StatusCode != http.StatusNoContent {
		t.Fatalf("valid PUT status=%d want 204", r.StatusCode)
	}
	r.Body.Close()
}

// TestCacheDeleteEntriesRequiresAdminOrAIConfigureAny asserts the spec
// authorization for the global clear endpoint: a non-admin role without
// ai:configure:any is forbidden; admin succeeds and the engine sees a
// Clear("") call.
func TestCacheDeleteEntriesRequiresAdminOrAIConfigureAny(t *testing.T) {
	// Non-admin caller → 403.
	{
		fce := &fakeCacheEngine{}
		d := Deps{
			Log:         discardLog(),
			Users:       &fakeUserStore{role: "user"},
			Settings:    &fakeCacheSettingsStore{},
			CacheEngine: fce,
		}
		srv := httptest.NewServer(NewRouter(d))
		defer srv.Close()
		c := authedClient(t, srv)

		r := c.delete(t, "/api/v1/cache/entries")
		if r.StatusCode != http.StatusForbidden {
			t.Fatalf("non-admin DELETE status=%d want 403", r.StatusCode)
		}
		r.Body.Close()
		if len(fce.clearCalls) != 0 {
			t.Fatalf("Clear unexpectedly called: %v", fce.clearCalls)
		}
	}

	// Admin caller → 204, engine.Clear("") called.
	{
		fce := &fakeCacheEngine{}
		d := Deps{
			Log:         discardLog(),
			Users:       &fakeUserStore{role: "admin"},
			Settings:    &fakeCacheSettingsStore{},
			CacheEngine: fce,
		}
		srv := httptest.NewServer(NewRouter(d))
		defer srv.Close()
		c := authedClient(t, srv)

		r := c.delete(t, "/api/v1/cache/entries")
		if r.StatusCode != http.StatusNoContent {
			t.Fatalf("admin DELETE status=%d want 204", r.StatusCode)
		}
		r.Body.Close()
		if len(fce.clearCalls) != 1 || fce.clearCalls[0] != "" {
			t.Fatalf("Clear calls=%v want [\"\"]", fce.clearCalls)
		}
	}
}

// TestCacheGetSettingsShape asserts the top-level shape of GET
// /api/v1/cache/settings: global is a populated object; per_service is
// always an array (even when empty).
func TestCacheGetSettingsShape(t *testing.T) {
	ss := &fakeCacheSettingsStore{
		saved: map[string]string{
			"cache.settings": `{"enabled":true,"applies_per":"global","ttl_seconds":120,"max_entries":50,"max_per_entry_kb":64}`,
		},
	}
	svc := &fakeCacheServiceLookup{
		list: []CacheServiceConfigRow{
			{ServiceID: "svc-1", Config: []byte(`{"cache":{"enabled":true,"applies_per":"per_endpoint","ttl_seconds":30,"max_entries":10,"max_per_entry_kb":8}}`)},
			{ServiceID: "svc-2", Config: []byte(`{}`)}, // no cache block — override=false
		},
	}
	d := Deps{
		Log:           discardLog(),
		Users:         &fakeUserStore{role: "user"},
		Settings:      ss,
		CacheServices: svc,
	}
	srv := httptest.NewServer(NewRouter(d))
	defer srv.Close()
	c := authedClient(t, srv)

	r := c.get(t, "/api/v1/cache/settings")
	if r.StatusCode != http.StatusOK {
		t.Fatalf("GET status=%d want 200", r.StatusCode)
	}
	var got cacheSettingsResp
	if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	r.Body.Close()
	if got.Global.AppliesPer != "global" || got.Global.TTLSeconds != 120 || !got.Global.Enabled {
		t.Fatalf("global shape: %+v", got.Global)
	}
	if len(got.PerService) != 2 {
		t.Fatalf("per_service len=%d want 2", len(got.PerService))
	}
	// svc-1 has an explicit cache block → override=true and matches values.
	if !got.PerService[0].Override || got.PerService[0].AppliesPer != "per_endpoint" || got.PerService[0].TTLSeconds != 30 {
		t.Fatalf("svc-1: %+v", got.PerService[0])
	}
	// svc-2 has no cache block → override=false, default values.
	if got.PerService[1].Override || got.PerService[1].AppliesPer != "global" {
		t.Fatalf("svc-2: %+v", got.PerService[1])
	}
}

// TestCacheGetStats covers the happy path and the nil-engine degradation
// (handler returns a zero-valued stats object so the UI never sees a 500
// before the proxy hot path is wired in Task 12).
func TestCacheGetStats(t *testing.T) {
	fce := &fakeCacheEngine{entries: 7, onDiskBytes: 1234, hitRate: 0.42}
	d := Deps{
		Log:         discardLog(),
		Users:       &fakeUserStore{role: "user"},
		Settings:    &fakeCacheSettingsStore{},
		CacheEngine: fce,
	}
	srv := httptest.NewServer(NewRouter(d))
	defer srv.Close()
	c := authedClient(t, srv)

	r := c.get(t, "/api/v1/cache/stats")
	if r.StatusCode != http.StatusOK {
		t.Fatalf("status=%d want 200", r.StatusCode)
	}
	var got cacheStatsResp
	if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	r.Body.Close()
	if got.Entries != 7 || got.OnDiskBytes != 1234 || got.HitRate24h < 0.41 || got.HitRate24h > 0.43 {
		t.Fatalf("stats: %+v", got)
	}
}

// TestCacheDeleteServiceEntriesAuthz asserts the per-service variant's
// owner-OR-ai:configure:own/any rule (spec Part B.3): owner succeeds, a
// non-owner non-admin fails with 403, a missing service returns 404.
func TestCacheDeleteServiceEntriesAuthz(t *testing.T) {
	const ownerUID = "u-self" // matches fakeUserStore default

	// Case 1: owner (user role, services owned by ownerUID) → 204.
	{
		fce := &fakeCacheEngine{}
		svc := &fakeCacheServiceLookup{owners: map[string]string{"svc-own": ownerUID}}
		d := Deps{
			Log: discardLog(), Users: &fakeUserStore{role: "user"},
			Settings: &fakeCacheSettingsStore{}, CacheEngine: fce, CacheServices: svc,
		}
		srv := httptest.NewServer(NewRouter(d))
		defer srv.Close()
		c := authedClient(t, srv)
		r := c.delete(t, "/api/v1/services/svc-own/cache/entries")
		if r.StatusCode != http.StatusNoContent {
			t.Fatalf("owner status=%d want 204", r.StatusCode)
		}
		r.Body.Close()
		if len(fce.clearCalls) != 1 || fce.clearCalls[0] != "endpoint:svc-own:" {
			t.Fatalf("clear calls=%v want [\"endpoint:svc-own:\"]", fce.clearCalls)
		}
	}

	// Case 2: non-owner (user role, service owned by someone else) → 403.
	{
		fce := &fakeCacheEngine{}
		svc := &fakeCacheServiceLookup{owners: map[string]string{"svc-other": "u-other"}}
		d := Deps{
			Log: discardLog(), Users: &fakeUserStore{role: "user"},
			Settings: &fakeCacheSettingsStore{}, CacheEngine: fce, CacheServices: svc,
		}
		srv := httptest.NewServer(NewRouter(d))
		defer srv.Close()
		c := authedClient(t, srv)
		r := c.delete(t, "/api/v1/services/svc-other/cache/entries")
		if r.StatusCode != http.StatusForbidden {
			t.Fatalf("non-owner status=%d want 403", r.StatusCode)
		}
		r.Body.Close()
		if len(fce.clearCalls) != 0 {
			t.Fatalf("clear unexpectedly called: %v", fce.clearCalls)
		}
	}

	// Case 3: admin :any → 204 even for someone else's service.
	{
		fce := &fakeCacheEngine{}
		svc := &fakeCacheServiceLookup{owners: map[string]string{"svc-other": "u-other"}}
		d := Deps{
			Log: discardLog(), Users: &fakeUserStore{role: "admin"},
			Settings: &fakeCacheSettingsStore{}, CacheEngine: fce, CacheServices: svc,
		}
		srv := httptest.NewServer(NewRouter(d))
		defer srv.Close()
		c := authedClient(t, srv)
		r := c.delete(t, "/api/v1/services/svc-other/cache/entries")
		if r.StatusCode != http.StatusNoContent {
			t.Fatalf("admin status=%d want 204", r.StatusCode)
		}
		r.Body.Close()
	}

	// Case 4: missing service → 404 (admin caller).
	{
		fce := &fakeCacheEngine{}
		svc := &fakeCacheServiceLookup{owners: map[string]string{}}
		d := Deps{
			Log: discardLog(), Users: &fakeUserStore{role: "admin"},
			Settings: &fakeCacheSettingsStore{}, CacheEngine: fce, CacheServices: svc,
		}
		srv := httptest.NewServer(NewRouter(d))
		defer srv.Close()
		c := authedClient(t, srv)
		r := c.delete(t, "/api/v1/services/svc-ghost/cache/entries")
		if r.StatusCode != http.StatusNotFound {
			t.Fatalf("missing-service status=%d want 404", r.StatusCode)
		}
		r.Body.Close()
	}
}
