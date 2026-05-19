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

func TestMigrate0003Schema(t *testing.T) {
	x := testDB(t) // opens + migrates a temp DB; FK ON via Open()
	db := x.DB()
	ctx := context.Background()

	// Seed a user so FK on services(user_id) is satisfied.
	if _, err := db.ExecContext(ctx,
		`INSERT INTO users(id,email,password_hash,role) VALUES('u1','svc@test.invalid','h','user')`); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	for _, q := range []string{
		`INSERT INTO services(id,user_id,name,type,subdomain,access_mode,api_key_header)
		   VALUES('s1','u1','web','http','k7p2qx','open','Authorization')`,
		`INSERT INTO service_api_keys(id,service_id,name,key_hash) VALUES('k1','s1','ci','deadbeef')`,
		`INSERT INTO service_access_policy(service_id,role) VALUES('s1','user')`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatalf("exec %q: %v", q, err)
		}
	}

	// cascade: deleting the service removes its keys + policy
	if _, err := db.ExecContext(ctx, `DELETE FROM services WHERE id='s1'`); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM service_api_keys`).Scan(&n); err != nil {
		t.Fatalf("count service_api_keys: %v", err)
	}
	if n != 0 {
		t.Fatalf("api keys not cascaded: %d", n)
	}
	var m int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM service_access_policy`).Scan(&m); err != nil {
		t.Fatalf("count service_access_policy: %v", err)
	}
	if m != 0 {
		t.Fatalf("access policy not cascaded: %d", m)
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
