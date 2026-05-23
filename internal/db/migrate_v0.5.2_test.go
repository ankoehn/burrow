//go:build postgres

package db

import (
	"database/sql"
	"os"
	"testing"
)

// TestConnLogRollupTopIPsDayColumnIsDate enforces v0.5.2 BACKLOG #5: the
// connection_log_rollup_top_ips.day column in the Postgres twin must be
// DATE (matching connection_log_rollups.day in 0015's postgres twin) so a
// future JOIN across the two tables does not require a cast.
//
// Requires a live Postgres URL in BURROW_TEST_POSTGRES_URL.
func TestConnLogRollupTopIPsDayColumnIsDate(t *testing.T) {
	pgURL := os.Getenv("BURROW_TEST_POSTGRES_URL")
	if pgURL == "" {
		t.Skip("BURROW_TEST_POSTGRES_URL not set; skipping postgres column-type check")
	}
	pgDB, err := sql.Open("pgx", pgURL)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer pgDB.Close()
	if err := pgDB.Ping(); err != nil {
		t.Fatalf("postgres ping: %v", err)
	}
	if err := MigrateForDriver(pgDB, "postgres"); err != nil {
		t.Fatalf("postgres migration ladder: %v", err)
	}
	var dataType string
	err = pgDB.QueryRow(
		`SELECT data_type FROM information_schema.columns
		 WHERE table_schema='public'
		   AND table_name='connection_log_rollup_top_ips'
		   AND column_name='day'`).Scan(&dataType)
	if err != nil {
		t.Fatalf("query data_type: %v", err)
	}
	if dataType != "date" {
		t.Errorf("connection_log_rollup_top_ips.day data_type=%q; want %q (alignment with connection_log_rollups.day)", dataType, "date")
	}
}
