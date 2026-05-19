package db

import (
	"context"
	"database/sql"
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

func TestMigration0002Foundation(t *testing.T) {
	x := testDB(t) // testDB in crud_test.go opens + migrates a temp DB
	ctx := context.Background()
	db := x.DB()

	// roles seeded
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM roles`).Scan(&n); err != nil {
		t.Fatalf("roles table: %v", err)
	}
	if n != 2 {
		t.Fatalf("want 2 seeded roles, got %d", n)
	}
	for _, r := range []string{"admin", "user"} {
		var desc string
		err := db.QueryRowContext(ctx, `SELECT description FROM roles WHERE name=?`, r).Scan(&desc)
		if err != nil || desc == "" {
			t.Fatalf("role %q: desc=%q err=%v", r, desc, err)
		}
	}

	// new user columns with defaults
	_, _ = db.ExecContext(ctx, `INSERT INTO users(id,email,password_hash,role) VALUES('u1','a@b.c','h','admin')`)
	var status string
	var lastLogin sql.NullTime
	if err := db.QueryRowContext(ctx, `SELECT status,last_login FROM users WHERE id='u1'`).Scan(&status, &lastLogin); err != nil {
		t.Fatalf("user new cols: %v", err)
	}
	if status != "active" || lastLogin.Valid {
		t.Fatalf("want status=active last_login=NULL, got %q valid=%v", status, lastLogin.Valid)
	}

	// new tunnel columns with defaults
	_, _ = db.ExecContext(ctx, `INSERT INTO tunnels(id,user_id,name,type,remote_port,local_addr) VALUES('tn1','u1','n','tcp',9000,'127.0.0.1:1')`)
	var in, out int64
	var mode string
	var flushed sql.NullTime
	if err := db.QueryRowContext(ctx, `SELECT total_bytes_in,total_bytes_out,access_mode,last_flushed_at FROM tunnels WHERE id='tn1'`).Scan(&in, &out, &mode, &flushed); err != nil {
		t.Fatalf("tunnel new cols: %v", err)
	}
	if in != 0 || out != 0 || mode != "open" || flushed.Valid {
		t.Fatalf("tunnel defaults wrong: in=%d out=%d mode=%q flushed=%v", in, out, mode, flushed.Valid)
	}

	// settings table usable
	if _, err := db.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES('k','v')`); err != nil {
		t.Fatalf("settings table: %v", err)
	}
}
