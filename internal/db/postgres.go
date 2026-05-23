//go:build postgres

package db

import (
	"database/sql"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// PostgresBackend is the v0.5.0 entry point for PostgreSQL. It opens and
// migrates the database using the postgres migration ladder, then wraps it
// in a type that satisfies Backend.
//
// Only compiled when the "postgres" build tag is set.
type PostgresBackend struct{ db *sql.DB }

// OpenPostgres opens a connection to the Postgres database at url (e.g.
// "postgres://user:pass@host/dbname"), pings it, sets a sensible connection
// limit, and runs the embedded postgres migration ladder.
func OpenPostgres(url string) (*PostgresBackend, error) {
	d, err := sql.Open("pgx", url)
	if err != nil {
		return nil, fmt.Errorf("OpenPostgres open: %w", err)
	}
	if err := d.Ping(); err != nil {
		_ = d.Close()
		return nil, fmt.Errorf("OpenPostgres ping: %w", err)
	}
	d.SetMaxOpenConns(10)
	if err := MigrateForDriver(d, "postgres"); err != nil {
		_ = d.Close()
		return nil, fmt.Errorf("OpenPostgres migrate: %w", err)
	}
	return &PostgresBackend{db: d}, nil
}

// DB implements Backend.
func (b *PostgresBackend) DB() *sql.DB { return b.db }

// Driver implements Backend.
func (b *PostgresBackend) Driver() string { return "postgres" }

// Now implements Backend. Postgres uses now() for the current timestamp.
func (b *PostgresBackend) Now() string { return "now()" }

// Placeholder implements Backend. Postgres uses $N positional parameters.
func (b *PostgresBackend) Placeholder(n int) string { return fmt.Sprintf("$%d", n) }

// Close closes the underlying database connection.
func (b *PostgresBackend) Close() error { return b.db.Close() }

// compile-time assertion: *PostgresBackend satisfies Backend.
var _ Backend = (*PostgresBackend)(nil)
