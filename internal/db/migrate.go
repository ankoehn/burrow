package db

import (
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"strings"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Migrate applies every embedded migration not yet recorded, idempotently.
func Migrate(d *sql.DB) error {
	if _, err := d.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY, applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		var seen string
		err := d.QueryRow(`SELECT version FROM schema_migrations WHERE version=?`, name).Scan(&seen)
		if err == nil {
			continue // already applied
		}
		if err != sql.ErrNoRows {
			return fmt.Errorf("check migration %s: %w", name, err)
		}
		raw, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		up := upBlock(string(raw))
		tx, err := d.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(up); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply %s: %w", name, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(version) VALUES(?)`, name); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record %s: %w", name, err)
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// upBlock returns the SQL between "-- +goose Up" and "-- +goose Down"
// (or the whole file if the markers are absent).
func upBlock(s string) string {
	up := s
	if i := strings.Index(s, "-- +goose Up"); i >= 0 {
		up = s[i+len("-- +goose Up"):]
	}
	if j := strings.Index(up, "-- +goose Down"); j >= 0 {
		up = up[:j]
	}
	return strings.TrimSpace(up)
}
