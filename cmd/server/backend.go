// Package main — database backend selection helper for cmd/server.
//
// openBackend branches on the ServerConfig's database_url /
// experimental_postgres_backend fields to construct either a SQLiteBackend
// (default) or a PostgresBackend (tag-gated). It logs the chosen driver at
// Info level so operators see the selection at startup.
//
// The BackendCloser interface combines db.Backend with io.Closer so the
// caller can defer backend.Close() without a type assertion.
package main

import (
	"log/slog"

	"github.com/ankoehn/burrow/internal/api"
	"github.com/ankoehn/burrow/internal/config"
	"github.com/ankoehn/burrow/internal/db"
)

// BackendCloser is db.Backend + Close(). Both *db.SQLiteBackend and
// *db.PostgresBackend satisfy this; the serve command uses it so it can
// defer backend.Close() without importing the concrete types.
type BackendCloser interface {
	db.Backend
	Close() error
}

// openBackend selects and opens the database backend:
//   - Postgres when cfg.DatabaseURL != "" && cfg.ExperimentalPostgres.
//   - SQLite (default) otherwise, using cfg.DatabasePath.
//
// The chosen backend is logged at Info level via log. openBackend runs all
// migrations (via the backend's own Open/migrate path).
func openBackend(cfg *config.ServerConfig, log *slog.Logger) (BackendCloser, error) {
	if cfg.DatabaseURL != "" && cfg.ExperimentalPostgres {
		// openPostgres is tag-gated: db_postgres.go (postgres tag) returns the
		// real backend; db_default.go (default) returns an explanatory error.
		b, err := openPostgres(cfg.DatabaseURL)
		if err != nil {
			return nil, err
		}
		log.Info("database", "backend", b.Driver(), "url_redacted", api.RedactDatabaseURL(cfg.DatabaseURL))
		return b, nil
	}
	b, err := db.OpenSQLite(cfg.DatabasePath)
	if err != nil {
		return nil, err
	}
	log.Info("database", "backend", b.Driver())
	return b, nil
}

// dbURLForStatus returns the connection string appropriate for the
// GET /api/v1/database url_redacted field:
//   - Postgres: redacted DSN (user:pass replaced with ****:****).
//   - SQLite: the on-disk file path (no credentials to redact).
func dbURLForStatus(cfg *config.ServerConfig, b BackendCloser) string {
	if b.Driver() == "postgres" {
		return api.RedactDatabaseURL(cfg.DatabaseURL)
	}
	return cfg.DatabasePath
}
