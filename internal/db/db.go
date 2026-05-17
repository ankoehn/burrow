// Package db is the hand-written typed SQLite data layer (pure-Go driver).
package db

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// Open opens (creating if needed) the SQLite database at path with the
// pragmas Burrow relies on, serialising writes (MVP-simple).
func Open(path string) (*sql.DB, error) {
	d, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", path, err)
	}
	d.SetMaxOpenConns(1)
	for _, p := range []string{
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
		"PRAGMA journal_mode=WAL",
	} {
		if _, err := d.Exec(p); err != nil {
			_ = d.Close()
			return nil, fmt.Errorf("pragma %q: %w", p, err)
		}
	}
	return d, nil
}
