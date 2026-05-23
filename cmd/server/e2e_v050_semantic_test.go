//go:build semantic_cache

package main

// e2e_v050_semantic_test.go — v0.5.0 INTEGRATION plan Task 3:
// real-stack e2e smoke for the opt-in semantic_cache build.
//
// Boots the v0.3.0 real-stack chain (bootE2EStack) with a chromem-backed
// semantic.Cache wired into aigw.Chain via the withSemanticCache option,
// seeds service_ai_config with cache + cache.semantic enabled, and runs a
// two-request scenario:
//
//  1. POST /v1/chat/completions with content "hello world"
//     → exact MISS, semantic MISS (index empty), upstream serves a small
//     OpenAI-shaped JSON body, chain stores into exact cache + Promote()s
//     into the semantic index.
//  2. POST /v1/chat/completions with content "hello, world!"
//     → exact MISS (different bytes), semantic HIT (the stub embedding
//     server returns an identical vector regardless of input, so cosine
//     similarity is 1.0 ≥ min_similarity=0.85), chain serves the cached
//     response with Burrow-Cache: similar + Burrow-Cache-Similarity headers
//     and does NOT hit the upstream a second time.
//
// Stats verification: GET /api/v1/cache/stats is called on a lightweight
// management API httptest.Server backed by the same DB, with the
// semanticEngineAdapter wired as SemanticEngine. This exercises the full
// P2.2 path: adapter.AggregateStats() sums per-service Stats() across all
// services and the JSON response contains semantic_entries >= 1 and
// semantic_similar_returned_24h >= 1.
//
// Why the stub embedding server returns a constant vector: this is a smoke
// test for the chain + Lookup/Promote round-trip plumbing, not for the
// quality of the embeddings. An identical vector trivially exceeds any
// min_similarity in [0, 1] — which is exactly the property we want so the
// test is deterministic across re-runs and across the chromem library's
// internal scoring choices. The two prompts ("hello world" vs "hello,
// world!") differ at the bytes level → the exact-cache key differs → so
// the semantic tier is the one being exercised on request #2.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ankoehn/burrow/internal/api"
	"github.com/ankoehn/burrow/internal/cache/semantic"
	"github.com/ankoehn/burrow/internal/db"
	"github.com/ankoehn/burrow/internal/store"
)

// TestV050SemanticCacheE2E is the v0.5.0 INTEGRATION Task 3 smoke test.
// It exercises the chain's semantic cache tier end-to-end against a real
// chromem-backed engine + a local stub embedding server.
func TestV050SemanticCacheE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e in -short")
	}

	// --- Stub embedding server --------------------------------------------
	// Responds with an OpenAI-shaped /v1/embeddings JSON body. Dispatches by
	// prompt-content substring:
	//
	//   - any input containing "hello" → constant 8-dim vector aligned along
	//     all axes (the "similar" cluster). Requests #1 and #2 land here, so
	//     cosine similarity between them is 1.0 ≥ min_similarity → HIT.
	//   - any input containing "orthogonal" → an 8-dim unit vector on a
	//     single dimension that is ZERO in the "hello" cluster vector. Cosine
	//     similarity to the hello cluster is 0.0 < min_similarity=0.85 → MISS.
	//   - anything else → the "hello" cluster vector (preserves existing
	//     behaviour for unrelated callers, defensive).
	//
	// The negative case (request #3) lands here as a MISS, exercising the
	// min_similarity threshold path. See file comment for the "constant vector
	// is fine for a smoke test" rationale of requests #1+#2.
	var embedCalls atomic.Int32
	const helloVec = `[0.5,0.5,0.5,0.5,0.5,0.5,0.5,0.5]`
	const orthoVec = `[0,0,0,0,0,0,0,1]` // unit vector on dim 7; dim 7 of helloVec = 0.5 → not orthogonal in the strict sense BUT cosine = 0.5/sqrt(8*0.25)/sqrt(1) ≈ 0.354 < 0.85
	embed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		embedCalls.Add(1)
		if r.URL.Path != "/v1/embeddings" {
			http.NotFound(w, r)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var parsed struct {
			Input string `json:"input"`
		}
		_ = json.Unmarshal(raw, &parsed)
		vec := helloVec
		if strings.Contains(parsed.Input, "orthogonal") {
			vec = orthoVec
		}
		body := `{"data":[{"embedding":` + vec + `,"index":0,"object":"embedding"}],"model":"stub","object":"list","usage":{"prompt_tokens":1,"total_tokens":1}}`
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(embed.Close)

	// --- Boot the real stack with a chromem-backed semantic.Cache ---------
	// We construct the cache here so the test can directly query its
	// Stats(ctx, serviceID) after the round-trip — see file comment for why
	// the /cache/stats API path is not exercised in this version.
	//
	// NOTE: the chromem cache needs the *same* *db.DB the chain uses. We
	// build it BEFORE bootE2EStack so we can pass the option, then re-wrap
	// the stack's db (which is the same underlying *sql.DB).
	//
	// bootE2EStack opens its own DB; we cannot inject ours. Instead, we
	// construct the cache against a tiny wrapper that delegates to s.db
	// AFTER boot completes — but the Chain captures the cache reference at
	// construction. To square that, we make the cache aware of the stack's
	// db by using a deferred handle: construct a placeholder Cache adapter
	// that proxies to a *semantic.Cache field set just after boot.
	//
	// Simpler path: bootE2EStack uses db.Open() + the same DB for both the
	// chain and the test. We can pre-allocate the cache by capturing the
	// stack's *db.DB after boot. But the option needs the value at the time
	// of construction. Solution: a small lazy adapter that holds a pointer
	// and forwards every call. After boot we set the pointer; the chain's
	// only access is via lookups during request handling, which happen AFTER
	// the visitor sends the first request below.
	holder := &lazySemanticCache{}
	s := bootE2EStack(t, withSemanticCache(holder))
	// Now wrap the same DB the stack opened and construct the real cache.
	wrapped := db.Wrap(s.db)
	realCache := semantic.New(wrapped, s.log)
	holder.set(realCache)

	// --- Switch the service to api_key mode + mint a key ------------------
	// Mirrors the pattern used by e2e_cache_redact_test.go so the visitor
	// can authenticate the upstream-bound request without a session cookie.
	must(t, s.store.SetServiceAccessMode(
		context.Background(), s.userID, "admin", s.serviceID, "api_key", "Authorization", nil),
		"SetServiceAccessMode(api_key)")
	_, plaintext, err := s.store.CreateAPIKey(
		context.Background(), s.userID, "admin", s.serviceID, "ci-semantic")
	must(t, err, "CreateAPIKey")

	// --- Seed service_ai_config with both cache + cache.semantic ----------
	// The chain's loader (chainConfigLoader.decodeServiceAIConfig) decodes
	// .cache and .cache.semantic into ServiceAIConfig.{Cache,Semantic}; the
	// chain's run() loop then exercises Lookup + Promote per spec A.3.
	cfgBlob := `{
	  "cache": {
	    "enabled": true,
	    "applies_per": "per_endpoint",
	    "ttl_seconds": 600,
	    "max_entries": 100,
	    "max_per_entry_kb": 64,
	    "semantic": {
	      "enabled": true,
	      "min_similarity": 0.85,
	      "embedding_mode": "local",
	      "embedding_url": "` + embed.URL + `/v1/embeddings",
	      "embedding_model": "stub",
	      "fallback_policy": "return_cached_marked",
	      "promote_on_miss": true,
	      "max_index_entries": 100
	    }
	  }
	}`
	if _, err := s.db.ExecContext(context.Background(),
		`INSERT INTO service_ai_config(service_id, config) VALUES(?, ?)`,
		s.serviceID, cfgBlob,
	); err != nil {
		t.Fatalf("seed service_ai_config: %v", err)
	}

	// --- Upstream handler: a small deterministic OpenAI body --------------
	// Must include Content-Length so chain caches the response (chain.go:655).
	var upstreamHits atomic.Int32
	respBody := []byte(`{"id":"chatcmpl-stub","choices":[{"message":{"role":"assistant","content":"hello from upstream"}}]}`)
	s.setUpstreamHandler(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", strconv.Itoa(len(respBody)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(respBody)
	})

	hc := s.visitorClient(t)
	target := "https://" + s.hostWithPort() + "/v1/chat/completions"

	mkReq := func(content string) *http.Request {
		body := `{"model":"gpt-4o","messages":[{"role":"user","content":"` + content + `"}]}`
		r, _ := http.NewRequest(http.MethodPost, target, bytes.NewReader([]byte(body)))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+plaintext)
		return r
	}

	// --- Request #1 — exact MISS, semantic MISS (empty index) -------------
	// Chain: cache.Lookup MISS → semantic.Lookup MISS (count==0) → upstream
	// → cache.Store → semantic.Promote (synchronous, post-Store).
	r1, err := hc.Do(mkReq("hello world"))
	must(t, err, "first request")
	body1 := readAllString(t, r1)
	if r1.StatusCode != http.StatusOK {
		t.Fatalf("first: want 200, got %d body=%s", r1.StatusCode, body1)
	}
	if upstreamHits.Load() != 1 {
		t.Fatalf("first: upstream hits = %d, want 1", upstreamHits.Load())
	}
	// MISS path: chain emits neither Burrow-Cache: HIT nor "similar".
	if got := r1.Header.Get("Burrow-Cache"); got == "HIT" || got == "similar" {
		t.Fatalf("first: Burrow-Cache=%q, want neither HIT nor similar", got)
	}

	// Give the chain a brief grace to land the semantic_index row. Promote
	// is synchronous in chain.go but the test queries the DB ourselves.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var n int
		if err := s.db.QueryRowContext(context.Background(),
			`SELECT count(*) FROM semantic_index WHERE service_id = ?`, s.serviceID,
		).Scan(&n); err != nil {
			t.Fatalf("count semantic_index: %v", err)
		}
		if n >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// --- Request #2 — exact MISS, semantic HIT ----------------------------
	// Different request bytes ("hello, world!") → different exact key → MISS.
	// But the stub embedding server returns the same vector → cosine 1.0 ≥
	// min_similarity=0.85 → semantic HIT → Burrow-Cache: similar + Similarity.
	r2, err := hc.Do(mkReq("hello, world!"))
	must(t, err, "second request")
	_ = readAllString(t, r2)
	if r2.StatusCode != http.StatusOK {
		t.Fatalf("second: want 200, got %d", r2.StatusCode)
	}
	if upstreamHits.Load() != 1 {
		t.Fatalf("second: upstream hits = %d, want 1 (semantic tier MUST have served)",
			upstreamHits.Load())
	}
	if got := r2.Header.Get("Burrow-Cache"); got != "similar" {
		t.Fatalf("second: Burrow-Cache=%q, want %q", got, "similar")
	}
	simStr := r2.Header.Get("Burrow-Cache-Similarity")
	if simStr == "" {
		t.Fatal("second: Burrow-Cache-Similarity header missing")
	}
	sim, perr := strconv.ParseFloat(simStr, 64)
	if perr != nil {
		t.Fatalf("second: cannot parse Burrow-Cache-Similarity=%q: %v", simStr, perr)
	}
	if sim < 0.85 {
		t.Errorf("second: Burrow-Cache-Similarity=%v, want >= 0.85", sim)
	}
	// Burrow-Cache-Age must be a non-negative integer string. The first
	// request landed milliseconds ago so age will typically be "0".
	ageStr := r2.Header.Get("Burrow-Cache-Age")
	if ageStr == "" {
		t.Error("second: Burrow-Cache-Age header missing")
	} else if _, perr := strconv.Atoi(ageStr); perr != nil {
		t.Errorf("second: Burrow-Cache-Age=%q is not an integer: %v", ageStr, perr)
	}

	// --- Request #3 — exact MISS, semantic MISS (orthogonal vector) -------
	// The embed stub maps any input containing "orthogonal" to a unit vector
	// on dim 7; the "hello" cluster vector is [0.5]*8. Cosine similarity is
	// ~0.354, well below min_similarity=0.85, so the chain MUST treat this
	// as a semantic MISS and serve from the upstream. This is the negative
	// case for the min_similarity threshold path — without it, request #1's
	// cached response would shadow ANY future request, defeating the point
	// of having a similarity threshold at all.
	//
	// Capture the semantic_similar_returned_24h counter just before and after
	// this request to assert it did NOT increment (the chain only counts a
	// similar hit when the candidate exceeds the threshold).
	before := callCacheStatsAPI(t, s, wrapped, realCache).SemanticSimilarReturned24h
	upstreamBefore := upstreamHits.Load()
	r3, err := hc.Do(mkReq("orthogonal vector"))
	must(t, err, "third request")
	_ = readAllString(t, r3)
	if r3.StatusCode != http.StatusOK {
		t.Fatalf("third: want 200, got %d", r3.StatusCode)
	}
	if got := upstreamHits.Load(); got != upstreamBefore+1 {
		t.Fatalf("third: upstream hits delta = %d, want 1 (semantic tier must MISS on orthogonal prompt)",
			got-upstreamBefore)
	}
	if got := r3.Header.Get("Burrow-Cache"); got == "similar" || got == "HIT" {
		t.Fatalf("third: Burrow-Cache=%q, want neither similar nor HIT (orthogonal prompt below min_similarity)", got)
	}
	// Threshold enforcement: similar_returned counter must NOT have moved.
	after := callCacheStatsAPI(t, s, wrapped, realCache).SemanticSimilarReturned24h
	if after != before {
		t.Errorf("third: semantic_similar_returned_24h moved from %d to %d after orthogonal prompt; threshold not enforced",
			before, after)
	}

	// --- Stats check via GET /api/v1/cache/stats (P2.2) -------------------
	// Spin up a minimal management API httptest.Server backed by the SAME
	// DB the chain used, then call GET /api/v1/cache/stats and assert the
	// semantic_entries and semantic_similar_returned_24h fields are non-zero.
	//
	// The SemanticEngine is wired as semanticEngineAdapter{cache: realCache,
	// svc: serviceListerAdapter{wrapped}} — the production adapter from P2.2.
	// This exercises the full pipeline: adapter.AggregateStats() iterates
	// all service IDs, calls realCache.Stats(ctx, id), and sums the fields.
	statsResp := callCacheStatsAPI(t, s, wrapped, realCache)
	if statsResp.SemanticEntries < 1 || statsResp.SemanticSimilarReturned24h < 1 {
		t.Errorf("GET /api/v1/cache/stats: semantic_entries=%d semantic_similar_returned_24h=%d; want both >= 1",
			statsResp.SemanticEntries, statsResp.SemanticSimilarReturned24h)
	}

	// Sanity: the embedding server was called at least twice (Promote on
	// req #1, Lookup on req #2).
	if got := embedCalls.Load(); got < 2 {
		t.Errorf("embedding server calls = %d, want >= 2 (Promote + Lookup)", got)
	}

	// Decode the stub upstream response so static-check sees we used it.
	var parsed struct {
		ID string `json:"id"`
	}
	if perr := json.Unmarshal([]byte(body1), &parsed); perr == nil && parsed.ID != "" {
		// no-op: parsing succeeded, body shape is the OpenAI envelope.
	}
}

// cacheStatsWire is the subset of GET /api/v1/cache/stats we assert on.
type cacheStatsWire struct {
	SemanticEntries            int `json:"semantic_entries"`
	SemanticSimilarReturned24h int `json:"semantic_similar_returned_24h"`
}

// callCacheStatsAPI boots a minimal management API httptest.Server backed by
// the same DB, logs in as the e2eStack admin, and returns the parsed response
// of GET /api/v1/cache/stats. The SemanticEngine field is wired to the
// production semanticEngineAdapter (P2.2) so this is the full stack test.
func callCacheStatsAPI(t *testing.T, s *e2eStack, wrapped *db.DB, realCache semantic.Cache) cacheStatsWire {
	t.Helper()

	st := store.New(s.db)
	deps := api.Deps{
		Log:      s.log,
		Users:    st,
		Roles:    st,
		Sessions: st,
		Settings: st,
		DB:       s.db,
		// SemanticEngine: wired to the production adapter so AggregateStats
		// returns real per-service sums from the chromem backend.
		SemanticEngine: newSemanticEngine(realCache, wrapped),
	}
	mgmtSrv := httptest.NewServer(api.NewRouter(deps))
	t.Cleanup(mgmtSrv.Close)

	// Log in as the same admin the e2e stack uses.
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	mgmtHC := &http.Client{Jar: jar}
	loginBody, _ := json.Marshal(map[string]string{
		"email":    e2eAdminEmail,
		"password": e2eAdminPassword,
	})
	resp, err := mgmtHC.Post(mgmtSrv.URL+"/api/v1/auth/login", "application/json",
		bytes.NewReader(loginBody))
	if err != nil {
		t.Fatalf("mgmt login: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("mgmt login status=%d body=%s", resp.StatusCode, b)
	}
	_ = resp.Body.Close()

	// Extract CSRF token from cookie jar.
	u, _ := url.Parse(mgmtSrv.URL)
	var csrf string
	for _, ck := range jar.Cookies(u) {
		if ck.Name == "burrow_csrf" {
			csrf = ck.Value
		}
	}
	if csrf == "" {
		t.Fatal("callCacheStatsAPI: no CSRF cookie after mgmt login")
	}

	req, err := http.NewRequest(http.MethodGet, mgmtSrv.URL+"/api/v1/cache/stats", nil)
	if err != nil {
		t.Fatalf("new stats request: %v", err)
	}
	statsResp, err := mgmtHC.Do(req)
	if err != nil {
		t.Fatalf("GET /api/v1/cache/stats: %v", err)
	}
	defer statsResp.Body.Close()
	if statsResp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(statsResp.Body)
		t.Fatalf("GET /api/v1/cache/stats status=%d body=%s", statsResp.StatusCode, b)
	}
	var out cacheStatsWire
	if err := json.NewDecoder(statsResp.Body).Decode(&out); err != nil {
		t.Fatalf("decode /api/v1/cache/stats: %v", err)
	}
	return out
}

// lazySemanticCache is a deferred-wiring adapter that satisfies semantic.Cache
// by forwarding every method to an inner cache set after construction. Used
// when the test cannot construct the real cache before bootE2EStack returns
// because the cache needs the stack's DB handle. Set must be called before
// any request fires; thereafter the adapter is effectively read-only.
type lazySemanticCache struct {
	inner atomic.Value // semantic.Cache
}

func (l *lazySemanticCache) set(c semantic.Cache) { l.inner.Store(c) }

func (l *lazySemanticCache) Lookup(ctx context.Context, serviceID string, prompt []byte, s semantic.Settings) (semantic.Candidate, bool, error) {
	c, ok := l.inner.Load().(semantic.Cache)
	if !ok || c == nil {
		return semantic.Candidate{}, false, nil
	}
	return c.Lookup(ctx, serviceID, prompt, s)
}

func (l *lazySemanticCache) Promote(ctx context.Context, serviceID, exactKeyHash string, prompt []byte, s semantic.Settings) error {
	c, ok := l.inner.Load().(semantic.Cache)
	if !ok || c == nil {
		return nil
	}
	return c.Promote(ctx, serviceID, exactKeyHash, prompt, s)
}

func (l *lazySemanticCache) ClearService(ctx context.Context, serviceID string) error {
	c, ok := l.inner.Load().(semantic.Cache)
	if !ok || c == nil {
		return nil
	}
	return c.ClearService(ctx, serviceID)
}

func (l *lazySemanticCache) Stats(ctx context.Context, serviceID string) (semantic.Stats, error) {
	c, ok := l.inner.Load().(semantic.Cache)
	if !ok || c == nil {
		return semantic.Stats{}, nil
	}
	return c.Stats(ctx, serviceID)
}
