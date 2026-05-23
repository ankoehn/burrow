//go:build postgres

// postgres_test.go — integration test for Postgres backend selection wiring.
//
// Requires a real Postgres instance. Set BURROW_TEST_POSTGRES_URL to run;
// otherwise the test is skipped. The CI e2e compose job provides this var
// via a sidecar pg container.
package main

import (
	"os"
	"testing"

	"github.com/ankoehn/burrow/internal/config"
)

// TestOpenBackendPostgres verifies that openBackend selects the Postgres
// backend when DatabaseURL + ExperimentalPostgres are both set and a real
// Postgres is reachable. Requires BURROW_TEST_POSTGRES_URL.
func TestOpenBackendPostgres(t *testing.T) {
	pgURL := os.Getenv("BURROW_TEST_POSTGRES_URL")
	if pgURL == "" {
		t.Skip("BURROW_TEST_POSTGRES_URL not set; skipping Postgres wiring test")
	}

	// Build a minimal ServerConfig with Postgres selected.
	cfg := &config.ServerConfig{
		Listen:               ":7000",
		TLSCert:              "certs/dev-server.pem",
		TLSKey:               "certs/dev-server-key.pem",
		DatabaseURL:          pgURL,
		ExperimentalPostgres: true,
		DatabasePath:         "", // clear SQLite path
	}

	b, err := openBackend(cfg, discardSlog())
	if err != nil {
		t.Fatalf("openBackend (postgres): %v", err)
	}
	defer b.Close()

	if b.Driver() != "postgres" {
		t.Errorf("driver = %q, want postgres", b.Driver())
	}
	if b.DB() == nil {
		t.Error("DB() returned nil")
	}
	if err := b.DB().Ping(); err != nil {
		t.Errorf("Ping after openBackend: %v", err)
	}
}

// TestOpenBackendSQLiteDefault verifies that when DatabaseURL is empty,
// openBackend falls back to SQLite using DatabasePath.
func TestOpenBackendSQLiteDefault(t *testing.T) {
	cfg := &config.ServerConfig{
		Listen:               ":7000",
		TLSCert:              "certs/dev-server.pem",
		TLSKey:               "certs/dev-server-key.pem",
		DatabasePath:         t.TempDir() + "/burrow_pg_test.db",
		DatabaseURL:          "",
		ExperimentalPostgres: false,
	}

	b, err := openBackend(cfg, discardSlog())
	if err != nil {
		t.Fatalf("openBackend (sqlite fallback): %v", err)
	}
	defer b.Close()

	if b.Driver() != "sqlite" {
		t.Errorf("driver = %q, want sqlite", b.Driver())
	}
}
