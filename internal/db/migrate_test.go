package db

import (
	"path/filepath"
	"testing"
)

func TestOpenAndMigrateIdempotent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.db")
	d, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := Migrate(d); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := Migrate(d); err != nil {
		t.Fatalf("migrate must be idempotent: %v", err)
	}
	for _, tbl := range []string{"users", "sessions", "client_tokens", "tunnels", "schema_migrations"} {
		var name string
		err := d.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, tbl).Scan(&name)
		if err != nil || name != tbl {
			t.Fatalf("table %s missing: %v", tbl, err)
		}
	}
	var fk int
	_ = d.QueryRow("PRAGMA foreign_keys").Scan(&fk)
	if fk != 1 {
		t.Fatalf("foreign_keys must be ON, got %d", fk)
	}
	if _, err := d.Exec(`INSERT INTO sessions(id, user_id, expires_at) VALUES('s1','nonexistent',CURRENT_TIMESTAMP)`); err == nil {
		t.Fatal("FK constraint must reject an orphaned session insert")
	}
}
