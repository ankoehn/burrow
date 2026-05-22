//go:build semantic_cache

package semantic

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	chromem "github.com/philippgille/chromem-go"

	"github.com/ankoehn/burrow/internal/db"
)

// chromemCache is the chromem-go-backed semantic cache implementation.
// It is compiled in only when the semantic_cache build tag is set.
type chromemCache struct {
	d      *db.DB
	log    *slog.Logger
	client *http.Client

	mu          sync.RWMutex
	collections map[string]*serviceCollection
}

// serviceCollection holds the in-memory chromem collection and per-service
// in-process counters for a single service.
type serviceCollection struct {
	coll *chromem.Collection

	// In-process best-effort counters (reset on restart).
	hits       atomic.Int64
	lookups    atomic.Int64
	promoted   atomic.Int64
}

// New constructs the chromem-backed Cache when the semantic_cache build tag
// is active. Uses *db.DB directly (db.Backend lands in Task 14).
func New(d *db.DB, log *slog.Logger) Cache {
	if log == nil {
		log = slog.Default()
	}
	return &chromemCache{
		d:           d,
		log:         log,
		client:      &http.Client{Timeout: 250 * time.Millisecond},
		collections: make(map[string]*serviceCollection),
	}
}

// getOrCreate lazily initialises the per-service chromem collection,
// rebuilding it from the semantic_index rows in the DB (spec A.6 — DB is
// source of truth).
func (c *chromemCache) getOrCreate(ctx context.Context, serviceID string) (*serviceCollection, error) {
	c.mu.RLock()
	sc, ok := c.collections[serviceID]
	c.mu.RUnlock()
	if ok {
		return sc, nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	// Double-check under write lock.
	if sc, ok = c.collections[serviceID]; ok {
		return sc, nil
	}

	// Create an in-memory chromem DB and collection for this service.
	// We supply a nil EmbeddingFunc because we always pass pre-computed
	// embeddings via Add (we do the HTTP embedding call ourselves).
	cdb := chromem.NewDB()
	coll, err := cdb.CreateCollection(serviceID, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("chromem create collection %q: %w", serviceID, err)
	}

	sc = &serviceCollection{coll: coll}

	// Rebuild from DB.
	rows, err := c.d.DB().QueryContext(ctx,
		`SELECT exact_key_hash, prompt_fingerprint, embedding_dim, embedding_blob
		   FROM semantic_index
		  WHERE service_id = ?
		  ORDER BY created_at ASC`, serviceID)
	if err != nil {
		return nil, fmt.Errorf("chromem rebuild query: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			exactKeyHash      string
			promptFingerprint string
			dim               int
			blob              []byte
		)
		if err := rows.Scan(&exactKeyHash, &promptFingerprint, &dim, &blob); err != nil {
			c.log.Warn("chromem rebuild: row scan failed",
				slog.String("service", serviceID), slog.String("err", err.Error()))
			continue
		}
		emb, err := blobToFloat32(blob, dim)
		if err != nil {
			c.log.Warn("chromem rebuild: blob decode failed",
				slog.String("service", serviceID), slog.String("err", err.Error()))
			continue
		}
		doc := chromem.Document{
			ID:        exactKeyHash,
			Embedding: emb,
			Metadata:  map[string]string{"fp": promptFingerprint},
			Content:   promptFingerprint,
		}
		if err := coll.AddDocument(ctx, doc); err != nil {
			c.log.Warn("chromem rebuild: AddDocument failed",
				slog.String("service", serviceID), slog.String("err", err.Error()))
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("chromem rebuild rows: %w", err)
	}

	c.collections[serviceID] = sc
	return sc, nil
}

// Lookup returns the most similar cached prompt for serviceID.
// On embedding failure it degrades silently to a miss (spec A.1.6).
func (c *chromemCache) Lookup(ctx context.Context, serviceID string, prompt []byte, s Settings) (Candidate, bool, error) {
	if !s.Enabled {
		return Candidate{}, false, nil
	}

	sc, err := c.getOrCreate(ctx, serviceID)
	if err != nil {
		c.log.Warn("semantic Lookup: collection init failed",
			slog.String("service", serviceID), slog.String("err", err.Error()))
		return Candidate{}, false, nil
	}

	sc.lookups.Add(1)

	if sc.coll.Count() == 0 {
		return Candidate{}, false, nil
	}

	emb, err := c.embed(ctx, prompt, s)
	if err != nil {
		c.log.Warn("semantic Lookup: embed failed (silent miss)",
			slog.String("service", serviceID), slog.String("err", err.Error()))
		return Candidate{}, false, nil
	}

	results, err := sc.coll.QueryEmbedding(ctx, emb, 1, nil, nil)
	if err != nil {
		c.log.Warn("semantic Lookup: query failed",
			slog.String("service", serviceID), slog.String("err", err.Error()))
		return Candidate{}, false, nil
	}
	if len(results) == 0 {
		return Candidate{}, false, nil
	}

	top := results[0]
	sim := float64(top.Similarity)
	if sim < s.MinSimilarity {
		return Candidate{}, false, nil
	}

	sc.hits.Add(1)

	fp := ""
	if top.Metadata != nil {
		fp = top.Metadata["fp"]
	}
	return Candidate{
		ExactKeyHash:      top.ID,
		PromptFingerprint: fp,
		Similarity:        sim,
	}, true, nil
}

// Promote embeds the prompt and inserts the entry into semantic_index + the
// in-memory chromem collection.
func (c *chromemCache) Promote(ctx context.Context, serviceID, exactKeyHash string, prompt []byte, s Settings) error {
	if !s.Enabled {
		return nil
	}

	sc, err := c.getOrCreate(ctx, serviceID)
	if err != nil {
		return fmt.Errorf("semantic Promote: collection init: %w", err)
	}

	emb, err := c.embed(ctx, prompt, s)
	if err != nil {
		// Degraded: log and return nil (non-fatal per spec A.1.6).
		c.log.Warn("semantic Promote: embed failed",
			slog.String("service", serviceID), slog.String("err", err.Error()))
		return nil
	}

	fp := promptFingerprint(prompt)
	blob := float32ToBlob(emb)
	dim := len(emb)

	// Persist to DB.
	_, dbErr := c.d.DB().ExecContext(ctx, `
		INSERT INTO semantic_index
		  (service_id, exact_key_hash, prompt_fingerprint, embedding_dim, embedding_blob, created_at)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(service_id, exact_key_hash) DO UPDATE SET
		  prompt_fingerprint = excluded.prompt_fingerprint,
		  embedding_dim = excluded.embedding_dim,
		  embedding_blob = excluded.embedding_blob,
		  created_at = excluded.created_at`,
		serviceID, exactKeyHash, fp, dim, blob,
	)
	if dbErr != nil {
		return fmt.Errorf("semantic Promote: db insert: %w", dbErr)
	}

	// Add / update in-memory collection. Delete first if doc exists (chromem
	// does not support upsert natively — AddDocument returns an error on dup).
	_ = sc.coll.Delete(ctx, nil, nil, exactKeyHash)

	doc := chromem.Document{
		ID:        exactKeyHash,
		Embedding: emb,
		Metadata:  map[string]string{"fp": fp},
		Content:   fp,
	}
	if addErr := sc.coll.AddDocument(ctx, doc); addErr != nil {
		c.log.Warn("semantic Promote: AddDocument failed",
			slog.String("service", serviceID), slog.String("err", addErr.Error()))
	}

	sc.promoted.Add(1)

	// LRU eviction if configured.
	if s.MaxIndexEntries > 0 {
		c.evictIfOverflow(ctx, serviceID, sc, s.MaxIndexEntries)
	}

	return nil
}

// ClearService deletes all semantic_index rows for a service and drops the
// in-memory collection.
func (c *chromemCache) ClearService(ctx context.Context, serviceID string) error {
	_, err := c.d.DB().ExecContext(ctx,
		`DELETE FROM semantic_index WHERE service_id = ?`, serviceID)
	if err != nil {
		return fmt.Errorf("semantic ClearService: %w", err)
	}

	c.mu.Lock()
	delete(c.collections, serviceID)
	c.mu.Unlock()

	return nil
}

// Stats returns the occupancy/hit-rate snapshot for a service.
func (c *chromemCache) Stats(ctx context.Context, serviceID string) (Stats, error) {
	var count int
	var onDiskBytes int64
	row := c.d.DB().QueryRowContext(ctx,
		`SELECT count(*), COALESCE(sum(length(embedding_blob)), 0)
		   FROM semantic_index
		  WHERE service_id = ?`, serviceID)
	if err := row.Scan(&count, &onDiskBytes); err != nil {
		return Stats{}, fmt.Errorf("semantic Stats: %w", err)
	}

	// In-process counters (best-effort; reset on restart).
	c.mu.RLock()
	sc, ok := c.collections[serviceID]
	c.mu.RUnlock()

	var hitRate float64
	var similarReturned, promotions int
	if ok {
		hits := sc.hits.Load()
		lookups := sc.lookups.Load()
		if lookups > 0 {
			hitRate = float64(hits) / float64(lookups)
		}
		similarReturned = int(hits)
		promotions = int(sc.promoted.Load())
	}

	return Stats{
		Entries:            count,
		OnDiskBytes:        onDiskBytes,
		HitRate24h:         hitRate,
		SimilarReturned24h: similarReturned,
		Promotions24h:      promotions,
	}, nil
}

// embed calls the operator-supplied embedding endpoint and returns the
// float32 vector. Returns an error on timeout, non-2xx, or malformed JSON.
// The caller (Lookup/Promote) degrades silently on error per spec A.1.6.
func (c *chromemCache) embed(ctx context.Context, prompt []byte, s Settings) ([]float32, error) {
	if s.EmbeddingURL == "" {
		return nil, fmt.Errorf("embed: no EmbeddingURL configured")
	}

	// Build request body using OpenAI embeddings shape.
	reqBody, err := json.Marshal(struct {
		Input string `json:"input"`
		Model string `json:"model"`
	}{
		Input: string(prompt),
		Model: s.EmbeddingModel,
	})
	if err != nil {
		return nil, fmt.Errorf("embed: marshal request: %w", err)
	}

	// Hard 250 ms timeout applied to the context (spec: 250 ms hard timeout).
	embedCtx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(embedCtx, http.MethodPost, s.EmbeddingURL,
		bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("embed: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embed: http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("embed: upstream status %d", resp.StatusCode)
	}

	var result struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("embed: decode response: %w", err)
	}
	if len(result.Data) == 0 || len(result.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("embed: empty embedding in response")
	}

	emb := result.Data[0].Embedding
	// Normalise to unit vector so cosine similarity works correctly.
	normalise(emb)
	return emb, nil
}

// evictIfOverflow deletes the oldest entries when the count exceeds
// maxIndexEntries, both from the DB and from the in-memory collection.
func (c *chromemCache) evictIfOverflow(ctx context.Context, serviceID string, sc *serviceCollection, maxIndexEntries int) {
	go func() {
		evictCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		var count int
		if err := c.d.DB().QueryRowContext(evictCtx,
			`SELECT count(*) FROM semantic_index WHERE service_id = ?`, serviceID,
		).Scan(&count); err != nil {
			c.log.Warn("semantic evict: count failed",
				slog.String("service", serviceID), slog.String("err", err.Error()))
			return
		}
		if count <= maxIndexEntries {
			return
		}
		trim := count - maxIndexEntries

		// Fetch oldest IDs to evict.
		rows, err := c.d.DB().QueryContext(evictCtx,
			`SELECT exact_key_hash FROM semantic_index
			  WHERE service_id = ?
			  ORDER BY created_at ASC
			  LIMIT ?`, serviceID, trim)
		if err != nil {
			c.log.Warn("semantic evict: fetch oldest failed",
				slog.String("service", serviceID), slog.String("err", err.Error()))
			return
		}
		var toEvict []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err == nil {
				toEvict = append(toEvict, id)
			}
		}
		rows.Close()

		if len(toEvict) == 0 {
			return
		}

		// Build placeholders for the DELETE.
		placeholders := strings.Repeat("?,", len(toEvict))
		placeholders = placeholders[:len(placeholders)-1]
		args := make([]any, len(toEvict)+1)
		args[0] = serviceID
		for i, id := range toEvict {
			args[i+1] = id
		}
		if _, err := c.d.DB().ExecContext(evictCtx,
			`DELETE FROM semantic_index WHERE service_id = ? AND exact_key_hash IN (`+placeholders+`)`,
			args...); err != nil {
			c.log.Warn("semantic evict: delete failed",
				slog.String("service", serviceID), slog.String("err", err.Error()))
			return
		}

		// Remove from in-memory collection.
		for _, id := range toEvict {
			if err := sc.coll.Delete(evictCtx, nil, nil, id); err != nil {
				c.log.Warn("semantic evict: chromem delete failed",
					slog.String("service", serviceID), slog.String("id", id),
					slog.String("err", err.Error()))
			}
		}

		c.log.Info("semantic evict: trimmed entries",
			slog.String("service", serviceID),
			slog.Int("trimmed", len(toEvict)),
			slog.Int("max", maxIndexEntries))
	}()
}

// promptFingerprint returns the hex-encoded SHA-256 of the prompt bytes.
func promptFingerprint(prompt []byte) string {
	sum := sha256.Sum256(prompt)
	return fmt.Sprintf("%x", sum)
}

// float32ToBlob packs a []float32 into a little-endian BLOB.
// Format matches the embedding_blob column in migration 0011.
func float32ToBlob(v []float32) []byte {
	buf := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

// blobToFloat32 unpacks a little-endian BLOB into a []float32.
func blobToFloat32(blob []byte, dim int) ([]float32, error) {
	if len(blob) != dim*4 {
		return nil, fmt.Errorf("blob length %d does not match dim %d", len(blob), dim)
	}
	v := make([]float32, dim)
	for i := range v {
		bits := binary.LittleEndian.Uint32(blob[i*4:])
		v[i] = math.Float32frombits(bits)
	}
	return v, nil
}

// normalise converts v to a unit vector in-place. If the magnitude is 0
// (zero vector), v is left unchanged.
func normalise(v []float32) {
	var sum float64
	for _, f := range v {
		sum += float64(f) * float64(f)
	}
	if sum == 0 {
		return
	}
	mag := float32(math.Sqrt(sum))
	for i := range v {
		v[i] /= mag
	}
}
