package api

// cache_settings_wire.go — shared wire shape for the cache.semantic block.
//
// SemanticSettings is the JSON shape of the cache.semantic sub-block. It
// round-trips between the v0.4 loader (cmd/server.decodeServiceAIConfig) and
// the runtime handler (internal/api/semantic_handlers.go::PutServiceAIConfig +
// cache_handlers.go::GetCacheSettings). Hoisting the type here keeps both
// callers locked to a single byte-identical wire format — adding a field in
// one place without the other previously caused silent drift on PUT/GET.

// SemanticSettings is the JSON shape of the cache.semantic settings block
// returned by GET /api/v1/cache/settings (spec A.4 / A.3 defaults) and
// accepted by PUT /api/v1/services/{id}/ai-config inside the cache block.
type SemanticSettings struct {
	Enabled         bool    `json:"enabled"`
	MinSimilarity   float64 `json:"min_similarity"`
	EmbeddingMode   string  `json:"embedding_mode"`
	EmbeddingURL    string  `json:"embedding_url"`
	EmbeddingModel  string  `json:"embedding_model"`
	FallbackPolicy  string  `json:"fallback_policy"`
	PromoteOnMiss   bool    `json:"promote_on_miss"`
	MaxIndexEntries int     `json:"max_index_entries"`
}
