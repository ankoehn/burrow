//go:build postgres

package main

import (
	"github.com/ankoehn/burrow/internal/db"
)

// openPostgres opens a PostgreSQL database using the provided DSN and returns a
// *db.PostgresBackend that satisfies BackendCloser. Only compiled when the
// "postgres" build tag is set; the default build provides a stub that errors.
func openPostgres(url string) (BackendCloser, error) {
	return db.OpenPostgres(url)
}
