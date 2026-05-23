//go:build !postgres

package main

import (
	"errors"
)

// openPostgres is the default-build stub: returns a clear error instructing
// the operator to rebuild with -tags=postgres. This path is only reached when
// cfg.DatabaseURL is non-empty AND cfg.ExperimentalPostgres is true, which
// validateDatabaseConfig in LoadServer enforces. In a production deployment
// without the postgres tag, the binary will fail at startup (before opening
// any listener) with a diagnostic error message.
func openPostgres(_ string) (BackendCloser, error) {
	return nil, errors.New(
		"postgres backend not compiled in; rebuild with -tags=postgres to enable Postgres support",
	)
}
