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

// Migrate applies every embedded SQLite migration not yet recorded, idempotently.
// It is a convenience wrapper around MigrateForDriver with driver="sqlite".
func Migrate(d *sql.DB) error {
	return MigrateForDriver(d, "sqlite")
}

// MigrateForDriver applies embedded migrations for the given driver ("sqlite" or "postgres").
//
// SQLite migrations use files ending in exactly ".sql" (but not ".postgres.sql").
// Postgres migrations use files ending in ".postgres.sql".
//
// The version key recorded in schema_migrations is the bare filename (e.g.
// "0001_init.sql" for sqlite, "0001_init.postgres.sql" for postgres), so the
// two ladders are independent and can coexist in the same embed.FS.
func MigrateForDriver(d *sql.DB, driver string) error {
	placeholder := "?"
	if driver == "postgres" {
		placeholder = "$1"
	}

	createDDL := `CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY, applied_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW())`
	if driver != "postgres" {
		createDDL = `CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY, applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP)`
	}

	if _, err := d.Exec(createDDL); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("list embedded migrations: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		n := e.Name()
		if driver == "postgres" {
			if strings.HasSuffix(n, ".postgres.sql") {
				names = append(names, n)
			}
		} else {
			// SQLite: include .sql files but exclude .postgres.sql
			if strings.HasSuffix(n, ".sql") && !strings.HasSuffix(n, ".postgres.sql") {
				names = append(names, n)
			}
		}
	}
	sort.Strings(names)

	for _, name := range names {
		var seen string
		err := d.QueryRow(`SELECT version FROM schema_migrations WHERE version=`+placeholder, name).Scan(&seen)
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
		if up == "" {
			return fmt.Errorf("migration %s: empty up block", name)
		}
		tx, err := d.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(up); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply %s: %w", name, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(version) VALUES(`+placeholder+`)`, name); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record %s: %w", name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit %s: %w", name, err)
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
