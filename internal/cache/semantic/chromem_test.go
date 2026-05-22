//go:build semantic_cache

package semantic_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/ankoehn/burrow/internal/cache/semantic"
	"github.com/ankoehn/burrow/internal/db"
)

// testSemanticCache opens a fresh SQLite DB, applies all migrations (so the
// semantic_index table from 0011 exists), seeds the required FK parent rows,
// and returns the wired Cache engine.
func testSemanticCache(t *testing.T, serviceIDs ...string) (semantic.Cache, *db.DB) {
	t.Helper()
	raw, err := db.Open(filepath.Join(t.TempDir(), "semantic.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Migrate(raw); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	d := db.Wrap(raw)
	t.Cleanup(func() { _ = d.Close() })

	ctx := context.Background()
	for _, svcID := range serviceIDs {
		if _, err := d.DB().ExecContext(ctx,
			`INSERT OR IGNORE INTO users(id,email,password_hash,role) VALUES('u1','test@test.invalid','h','user')`); err != nil {
			t.Fatalf("seed user: %v", err)
		}
		if _, err := d.DB().ExecContext(ctx,
			`INSERT OR IGNORE INTO services(id,user_id,name,type,subdomain,access_mode,api_key_header)
			   VALUES(?,  'u1', ?, 'http', ?, 'open', 'Authorization')`,
			svcID, svcID, svcID); err != nil {
			t.Fatalf("seed service %s: %v", svcID, err)
		}
	}

	return semantic.New(d, nil), d
}

// makeEmbedStub returns an httptest.Server that responds with a deterministic
// embedding vector for prompts keyed by their exact input string.
// Unit-normalised vectors: cat=[1,0,0], dog=[0,1,0], ortho=[0,0,1].
func makeEmbedStub(t *testing.T, assignments map[string][]float32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		emb, ok := assignments[req.Input]
		if !ok {
			// Default: return [0,0,0] (zero vector — triggers silent miss)
			emb = []float32{0, 0, 0}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []map[string]interface{}{
				{"embedding": emb},
			},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestChromemRoundTrip promotes two entries (cat, dog), then:
//   - Lookup with cat-vector → hit on cat entry
//   - Lookup with orthogonal vector → miss
func TestChromemRoundTrip(t *testing.T) {
	catPrompt := `{"prompt": "the cat sat"}`
	dogPrompt := `{"prompt": "the dog ran"}`
	catLikePrompt := `{"prompt": "a cat is sitting"}`
	orthoPrompt := `{"prompt": "something completely different"}`

	// Unit vectors: cat=[1,0,0], dog=[0,1,0], ortho=[0,0,1]
	catVec := []float32{1, 0, 0}
	dogVec := []float32{0, 1, 0}
	orthoVec := []float32{0, 0, 1}
	// cat-like is ≈cat (high similarity) — slightly rotated but much closer to cat
	catLikeVec := []float32{0.99, 0.141, 0}

	srv := makeEmbedStub(t, map[string][]float32{
		catPrompt:     catVec,
		dogPrompt:     dogVec,
		catLikePrompt: catLikeVec,
		orthoPrompt:   orthoVec,
	})

	const svcID = "svc-roundtrip"
	s := semantic.Settings{
		Enabled:         true,
		MinSimilarity:   0.8,
		EmbeddingURL:    srv.URL,
		EmbeddingModel:  "test-model",
		MaxIndexEntries: 100,
	}

	ctx := context.Background()
	c, _ := testSemanticCache(t, svcID)

	// Promote cat entry.
	if err := c.Promote(ctx, svcID, "catkey", []byte(catPrompt), s); err != nil {
		t.Fatalf("Promote cat: %v", err)
	}
	// Promote dog entry.
	if err := c.Promote(ctx, svcID, "dogkey", []byte(dogPrompt), s); err != nil {
		t.Fatalf("Promote dog: %v", err)
	}

	// Lookup with cat-like prompt → should hit cat entry.
	cand, hit, err := c.Lookup(ctx, svcID, []byte(catLikePrompt), s)
	if err != nil {
		t.Fatalf("Lookup cat-like: %v", err)
	}
	if !hit {
		t.Error("expected hit for cat-like prompt, got miss")
	}
	if hit && cand.ExactKeyHash != "catkey" {
		t.Errorf("expected catkey, got %q", cand.ExactKeyHash)
	}
	if hit && cand.Similarity < s.MinSimilarity {
		t.Errorf("similarity %f below MinSimilarity %f", cand.Similarity, s.MinSimilarity)
	}

	// Lookup with orthogonal prompt → should miss.
	_, hit2, err2 := c.Lookup(ctx, svcID, []byte(orthoPrompt), s)
	if err2 != nil {
		t.Fatalf("Lookup ortho: %v", err2)
	}
	if hit2 {
		t.Error("expected miss for orthogonal prompt, got hit")
	}
}

// TestChromemTimeout asserts that when the stub is slow (> 250 ms), the
// Lookup returns a silent miss (false, nil) — no error propagated.
func TestChromemTimeout(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Sleep longer than the 250 ms hard timeout.
		time.Sleep(400 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(slow.Close)

	const svcID = "svc-timeout"
	s := semantic.Settings{
		Enabled:        true,
		MinSimilarity:  0.8,
		EmbeddingURL:   slow.URL,
		EmbeddingModel: "test-model",
	}

	ctx := context.Background()
	c, d := testSemanticCache(t, svcID)

	// Seed a DB row directly (bypassing embed) so the collection is non-empty
	// and the Lookup path actually calls embed (triggering the timeout).
	// Embedding: [1,0,0] little-endian float32 = 00 00 80 3F 00 00 00 00 00 00 00 00
	_, dbErr := d.DB().ExecContext(ctx, `
		INSERT INTO semantic_index
		  (service_id, exact_key_hash, prompt_fingerprint, embedding_dim, embedding_blob, created_at)
		VALUES (?, 'dummykey', 'dummyfp', 3, ?, CURRENT_TIMESTAMP)`,
		svcID,
		[]byte{0x00, 0x00, 0x80, 0x3F, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
	)
	if dbErr != nil {
		t.Fatalf("seed DB: %v", dbErr)
	}

	start := time.Now()
	_, hit, err := c.Lookup(ctx, svcID, []byte(`{"prompt":"test"}`), s)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Lookup (timeout): expected nil error, got %v", err)
	}
	if hit {
		t.Error("Lookup (timeout): expected miss (silent timeout), got hit")
	}
	// Should have returned within ~350 ms (250ms timeout + slack).
	if elapsed > 1*time.Second {
		t.Errorf("Lookup took %v, expected < 1s", elapsed)
	}
}

// TestChromemClearService asserts that ClearService wipes the DB rows and
// drops the in-memory collection (subsequent Stats show 0 entries).
func TestChromemClearService(t *testing.T) {
	catVec := []float32{1, 0, 0}
	const svcID = "svc-clear"
	srv := makeEmbedStub(t, map[string][]float32{
		`{"prompt": "the cat"}`: catVec,
	})

	s := semantic.Settings{
		Enabled:        true,
		MinSimilarity:  0.8,
		EmbeddingURL:   srv.URL,
		EmbeddingModel: "test-model",
	}

	ctx := context.Background()
	c, _ := testSemanticCache(t, svcID)

	if err := c.Promote(ctx, svcID, "k1", []byte(`{"prompt": "the cat"}`), s); err != nil {
		t.Fatalf("Promote: %v", err)
	}

	// Verify entry exists in stats.
	stats, err := c.Stats(ctx, svcID)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.Entries != 1 {
		t.Fatalf("expected 1 entry after Promote, got %d", stats.Entries)
	}

	// Clear the service.
	if err := c.ClearService(ctx, svcID); err != nil {
		t.Fatalf("ClearService: %v", err)
	}

	// Stats should show 0 entries.
	stats2, err := c.Stats(ctx, svcID)
	if err != nil {
		t.Fatalf("Stats after clear: %v", err)
	}
	if stats2.Entries != 0 {
		t.Fatalf("expected 0 entries after ClearService, got %d", stats2.Entries)
	}
}

// TestChromemDBRebuild asserts that a new Cache instance over the same DB
// rebuilds its in-memory collection from DB rows on the first Lookup.
func TestChromemDBRebuild(t *testing.T) {
	catVec := []float32{1, 0, 0}
	const svcID = "svc-rebuild"
	srv := makeEmbedStub(t, map[string][]float32{
		`{"prompt": "rebuild cat"}`: catVec,
	})

	s := semantic.Settings{
		Enabled:        true,
		MinSimilarity:  0.8,
		EmbeddingURL:   srv.URL,
		EmbeddingModel: "test-model",
	}

	ctx := context.Background()
	_, d := testSemanticCache(t, svcID)

	// Create a first cache instance and promote an entry.
	c1 := semantic.New(d, nil)
	if err := c1.Promote(ctx, svcID, "rk1", []byte(`{"prompt": "rebuild cat"}`), s); err != nil {
		t.Fatalf("Promote: %v", err)
	}

	// Create a second cache instance over the same DB — simulates process restart.
	// The new instance must rebuild from DB rows on first Lookup.
	c2 := semantic.New(d, nil)

	cand, hit, err := c2.Lookup(ctx, svcID, []byte(`{"prompt": "rebuild cat"}`), s)
	if err != nil {
		t.Fatalf("Lookup after rebuild: %v", err)
	}
	if !hit {
		t.Error("expected hit after DB rebuild, got miss")
	}
	if hit && cand.ExactKeyHash != "rk1" {
		t.Errorf("expected rk1, got %q", cand.ExactKeyHash)
	}
}
