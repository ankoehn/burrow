//go:build !semantic_cache

package main

import (
	"github.com/ankoehn/burrow/internal/api"
	"github.com/ankoehn/burrow/internal/cache/semantic"
	"github.com/ankoehn/burrow/internal/db"
)

// newSemanticEngine returns a noopSemanticEngine in the default
// (non-semantic_cache-tagged) build. The real adapter is provided by
// v05_semantic_adapter.go under the semantic_cache build tag.
func newSemanticEngine(_ semantic.Cache, _ *db.DB) api.SemanticEngine {
	return noopSemanticEngine{}
}
