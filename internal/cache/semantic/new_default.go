//go:build !semantic_cache

package semantic

import (
	"log/slog"

	"github.com/ankoehn/burrow/internal/db"
)

// New returns a NoopCache in the default (non-semantic_cache-tagged) build.
// The chromem-backed implementation is provided by chromem.go under the
// semantic_cache build tag.
func New(_ *db.DB, _ *slog.Logger) Cache {
	return NoopCache{}
}
