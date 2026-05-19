package db

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func testDB(t *testing.T) *DB {
	t.Helper()
	d, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(d); err != nil {
		t.Fatal(err)
	}
	x := Wrap(d)
	t.Cleanup(func() { _ = x.Close() })
	return x
}

func TestUserCRUDAndCascade(t *testing.T) {
	ctx := context.Background()
	x := testDB(t)
	if n, _ := x.CountUsers(ctx); n != 0 {
		t.Fatalf("want 0 users, got %d", n)
	}
	u := User{ID: "u1", Email: "a@b.c", PasswordHash: "h", Role: "admin"}
	if err := x.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	if err := x.CreateUser(ctx, User{ID: "u2", Email: "a@b.c", PasswordHash: "h", Role: "user"}); err == nil {
		t.Fatal("duplicate email must fail (UNIQUE)")
	}
	got, err := x.GetUserByEmail(ctx, "a@b.c")
	if err != nil || got.ID != "u1" {
		t.Fatalf("GetUserByEmail: %+v %v", got, err)
	}
	if _, err := x.GetUserByEmail(ctx, "nope@x"); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	// cascade: token + session + tunnel removed when user deleted
	_ = x.CreateClientToken(ctx, ClientToken{ID: "t1", UserID: "u1", Name: "n", TokenHash: "hh"})
	_ = x.CreateSession(ctx, Session{ID: "s1", UserID: "u1", ExpiresAt: time.Now().Add(time.Hour)})
	_ = x.UpsertTunnel(ctx, Tunnel{ID: "tn1", UserID: "u1", Name: "x", Type: "tcp", RemotePort: 9000, LocalAddr: "127.0.0.1:1"})
	if _, err := x.DB().Exec(`DELETE FROM users WHERE id=?`, "u1"); err != nil {
		t.Fatal(err)
	}
	if _, err := x.GetClientTokenByHash(ctx, "hh"); err != ErrNotFound {
		t.Fatalf("token should cascade-delete, got %v", err)
	}
}

func TestSessionExpiryAndTokens(t *testing.T) {
	ctx := context.Background()
	x := testDB(t)
	_ = x.CreateUser(ctx, User{ID: "u1", Email: "a@b.c", PasswordHash: "h", Role: "admin"})
	_ = x.CreateSession(ctx, Session{ID: "live", UserID: "u1", ExpiresAt: time.Now().UTC().Add(time.Hour)})
	_ = x.CreateSession(ctx, Session{ID: "dead", UserID: "u1", ExpiresAt: time.Now().UTC().Add(-time.Hour)})
	if s, err := x.GetSession(ctx, "live"); err != nil || s.UserID != "u1" {
		t.Fatalf("GetSession live: %+v %v", s, err)
	}
	n, err := x.DeleteExpiredSessions(ctx)
	if err != nil || n != 1 {
		t.Fatalf("DeleteExpiredSessions: n=%d err=%v", n, err)
	}
	if _, err := x.GetSession(ctx, "dead"); err != ErrNotFound {
		t.Fatalf("expired session gone? got %v", err)
	}
	_ = x.CreateClientToken(ctx, ClientToken{ID: "t1", UserID: "u1", Name: "cli", TokenHash: "abc"})
	ct, err := x.GetClientTokenByHash(ctx, "abc")
	if err != nil || ct.UserID != "u1" {
		t.Fatalf("GetClientTokenByHash: %+v %v", ct, err)
	}
	if err := x.TouchClientTokenLastUsed(ctx, "t1"); err != nil {
		t.Fatal(err)
	}
	lst, _ := x.ListClientTokensByUser(ctx, "u1")
	if len(lst) != 1 || lst[0].LastUsed == nil {
		t.Fatalf("ListClientTokensByUser/last_used: %+v", lst)
	}
	if err := x.DeleteClientToken(ctx, "t1", "u1"); err != nil {
		t.Fatal(err)
	}
	lst, _ = x.ListClientTokensByUser(ctx, "u1")
	if len(lst) != 0 {
		t.Fatal("token not deleted")
	}
}

func TestUpdateUserPassword(t *testing.T) {
	ctx := context.Background()
	x := testDB(t)
	_ = x.CreateUser(ctx, User{ID: "u1", Email: "a@b.c", PasswordHash: "oldhash", Role: "admin"})

	// Update to new hash.
	if err := x.UpdateUserPassword(ctx, "u1", "newhash"); err != nil {
		t.Fatalf("UpdateUserPassword: %v", err)
	}
	u, err := x.GetUserByID(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if u.PasswordHash != "newhash" {
		t.Fatalf("password_hash not updated, got %q", u.PasswordHash)
	}
	// updated_at must have been bumped (non-zero, different from created_at).
	// We only assert non-zero since SQLite's CURRENT_TIMESTAMP granularity is 1s.
	if u.UpdatedAt.IsZero() {
		t.Fatal("updated_at must be non-zero after UpdateUserPassword")
	}

	// Non-existent user → ErrNotFound.
	if err := x.UpdateUserPassword(ctx, "nouser", "h"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing user → ErrNotFound, got %v", err)
	}
}

func TestListUsersNeverExposesPasswordHash(t *testing.T) {
	ctx := context.Background()
	x := testDB(t)
	_ = x.CreateUser(ctx, User{ID: "u1", Email: "a@b.c", PasswordHash: "SECRETHASH", Role: "admin"})
	_ = x.CreateUser(ctx, User{ID: "u2", Email: "b@b.c", PasswordHash: "ANOTHERSECRET", Role: "user"})

	users, total, err := x.ListUsersPage(ctx, "", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 2 || total != 2 {
		t.Fatalf("want 2 users, got %d (total %d)", len(users), total)
	}
	for _, u := range users {
		if u.PasswordHash != "" {
			t.Errorf("ListUsersPage must not populate PasswordHash, got %q for %s", u.PasswordHash, u.Email)
		}
		if u.ID == "" || u.Email == "" || u.Role == "" {
			t.Errorf("ListUsersPage must populate id/email/role, got %+v", u)
		}
	}
}

func TestDeleteUserErrNotFound(t *testing.T) {
	ctx := context.Background()
	x := testDB(t)
	if err := x.DeleteUser(ctx, "nonexistent"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete non-existent → ErrNotFound, got %v", err)
	}
}

func TestDeleteUserCascadeDB(t *testing.T) {
	ctx := context.Background()
	x := testDB(t)
	_ = x.CreateUser(ctx, User{ID: "u1", Email: "a@b.c", PasswordHash: "h", Role: "admin"})
	_ = x.CreateClientToken(ctx, ClientToken{ID: "t1", UserID: "u1", Name: "n", TokenHash: "hh"})
	_ = x.CreateSession(ctx, Session{ID: "s1", UserID: "u1", ExpiresAt: time.Now().Add(time.Hour)})
	_ = x.UpsertTunnel(ctx, Tunnel{ID: "tn1", UserID: "u1", Name: "x", Type: "tcp", RemotePort: 9000, LocalAddr: "127.0.0.1:1"})

	if err := x.DeleteUser(ctx, "u1"); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	// All child rows must be gone via ON DELETE CASCADE.
	if _, err := x.GetClientTokenByHash(ctx, "hh"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("token must cascade-delete: %v", err)
	}
	if _, err := x.GetSession(ctx, "s1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("session must cascade-delete: %v", err)
	}
	tunnels, _ := x.ListTunnelsByUser(ctx, "u1")
	if len(tunnels) != 0 {
		t.Fatalf("tunnels must cascade-delete, got %d", len(tunnels))
	}
}

func TestTunnelUpsertTouchList(t *testing.T) {
	ctx := context.Background()
	x := testDB(t)
	_ = x.CreateUser(ctx, User{ID: "u1", Email: "a@b.c", PasswordHash: "h", Role: "admin"})
	tn := Tunnel{ID: "tn1", UserID: "u1", Name: "web", Type: "tcp", RemotePort: 9000, LocalAddr: "127.0.0.1:3000"}
	if err := x.UpsertTunnel(ctx, tn); err != nil {
		t.Fatal(err)
	}
	tn.Name = "web2"
	if err := x.UpsertTunnel(ctx, tn); err != nil {
		t.Fatal(err)
	}
	if err := x.TouchTunnelLastSeen(ctx, "tn1"); err != nil {
		t.Fatal(err)
	}
	ts, _ := x.ListTunnelsByUser(ctx, "u1")
	if len(ts) != 1 || ts[0].Name != "web2" || ts[0].LastSeen == nil {
		t.Fatalf("tunnels: %+v", ts)
	}
}
