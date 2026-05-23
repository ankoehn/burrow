package db

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// Backend is the v0.5.0 database-driver abstraction. It exposes the raw
// *sql.DB for callers that build queries directly, plus a small set of
// driver-specific helpers needed to write portable SQL.
//
// Both *DB (the v0.4.0 typed wrapper) and SQLiteBackend satisfy this interface.
// PostgresBackend satisfies it under the "postgres" build tag.
//
// NOTE: The v0.4.0 typed accessor methods (users.go, sessions.go, …) still
// accept *DB, not Backend. That refactor is deferred to a future release.
// New code that needs to be driver-agnostic should accept Backend instead.
type Backend interface {
	// DB returns the underlying *sql.DB for direct query use.
	DB() *sql.DB
	// Driver returns "sqlite" or "postgres".
	Driver() string
	// Now returns the SQL fragment for the current timestamp appropriate for
	// the driver: "CURRENT_TIMESTAMP" (SQLite) or "now()" (Postgres).
	Now() string
	// Placeholder returns the positional parameter placeholder for the given
	// 1-based argument index: "?" (SQLite) or "$N" (Postgres).
	Placeholder(n int) string
}

// SQLiteBackend is the v0.5.0 entry point for SQLite. It opens and migrates
// the database, then wraps it in a type that satisfies Backend.
type SQLiteBackend struct{ db *sql.DB }

// OpenSQLite opens (creating if needed) a SQLite database at path, applies the
// standard Burrow pragmas and runs all embedded migrations. The returned
// *SQLiteBackend satisfies Backend.
func OpenSQLite(path string) (*SQLiteBackend, error) {
	d, err := Open(path) // reuse existing pragma setup from db.go
	if err != nil {
		return nil, fmt.Errorf("OpenSQLite: %w", err)
	}
	if err := Migrate(d); err != nil {
		_ = d.Close()
		return nil, fmt.Errorf("OpenSQLite migrate: %w", err)
	}
	return &SQLiteBackend{db: d}, nil
}

// DB implements Backend.
func (b *SQLiteBackend) DB() *sql.DB { return b.db }

// Driver implements Backend.
func (b *SQLiteBackend) Driver() string { return "sqlite" }

// Now implements Backend.
func (b *SQLiteBackend) Now() string { return "CURRENT_TIMESTAMP" }

// Placeholder implements Backend. SQLite uses "?" for all positional args.
func (b *SQLiteBackend) Placeholder(_ int) string { return "?" }

// Close closes the underlying database connection.
func (b *SQLiteBackend) Close() error { return b.db.Close() }

// compile-time assertion: *SQLiteBackend satisfies Backend.
var _ Backend = (*SQLiteBackend)(nil)
