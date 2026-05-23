//go:build postgres

package db

// postgres_rewriter_test.go — v0.5.1 P1.1 corpus gate for the ?-to-$N
// SQL placeholder rewriter that lets the unchanged SQLite-style query
// strings (44 production files, ~180 sites) execute under pgx/stdlib.
//
// The corpus JSON contains a mix of real production SQL strings sampled
// across internal/db, internal/connlog, internal/cache, internal/store,
// internal/aimeter, plus synthetic edge cases the state machine MUST
// handle correctly:
//
//   - '...' single-quoted string literals (incl. embedded '' escapes)
//   - "..." double-quoted identifiers
//   - $$...$$ and $tag$...$tag$ dollar-quoted strings (Postgres-only)
//   - -- line comments to newline/EOF
//   - /* ... */ block comments (incl. multi-line)
//   - multi-digit placeholder indices ($10, $15, …)
//
// Pre-implementation this test compiles but every case fails because
// rewriteQuestionMarks does not yet exist. That failure-first run is
// the TDD evidence the v0.5.1 plan calls for.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type rewriterCase struct {
	Name string `json:"name"`
	In   string `json:"in"`
	Out  string `json:"out"`
}

func loadCorpus(t *testing.T, path string) []rewriterCase {
	t.Helper()
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("filepath.Abs(%q): %v", path, err)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read corpus %s: %v", abs, err)
	}
	var cases []rewriterCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatalf("decode corpus %s: %v", abs, err)
	}
	if len(cases) == 0 {
		t.Fatalf("empty corpus at %s", abs)
	}
	return cases
}

func TestPostgresRewriteCorpus(t *testing.T) {
	cases := loadCorpus(t, "testdata/postgres_rewriter_corpus.json")
	for _, c := range cases {
		c := c
		t.Run(c.Name, func(t *testing.T) {
			got := rewriteQuestionMarks(c.In)
			if got != c.Out {
				t.Errorf("\n in: %q\nwant: %q\n got: %q", c.In, c.Out, got)
			}
		})
	}
}
