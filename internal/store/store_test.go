package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ankoehn/burrow/internal/db"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(d); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return New(d)
}

func TestSeedAdminIdempotentAndAuth(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	if err := s.SeedAdmin(ctx, "", ""); err != nil {
		t.Fatalf("empty creds → no-op: %v", err)
	}
	if err := s.SeedAdmin(ctx, "admin@x", "pw"); err != nil {
		t.Fatal(err)
	}
	if err := s.SeedAdmin(ctx, "admin@x", "pw"); err != nil {
		t.Fatalf("second seed (users exist) → no-op: %v", err)
	}
	u, err := s.GetUserByEmail(ctx, "admin@x")
	if err != nil || u.Role != "admin" {
		t.Fatalf("admin not seeded: %+v %v", u, err)
	}
	ok, _ := s.VerifyUserPassword(ctx, "admin@x", "pw")
	if !ok {
		t.Fatal("seeded password must verify")
	}
}

func TestTokenAuthenticateAndTunnelPersist(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	_ = s.SeedAdmin(ctx, "a@x", "pw")
	u, _ := s.GetUserByEmail(ctx, "a@x")
	pt, err := s.IssueClientToken(ctx, u.ID, "laptop")
	if err != nil {
		t.Fatal(err)
	}
	uid, err := s.Authenticate(ctx, pt)
	if err != nil || uid != u.ID {
		t.Fatalf("Authenticate valid: uid=%s err=%v", uid, err)
	}
	if _, err := s.Authenticate(ctx, "bur_bogus"); err == nil {
		t.Fatal("unknown token must fail")
	}
	if err := s.SaveTunnel(ctx, u.ID, &tunnelFixture); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkTunnelSeen(ctx, "tn1"); err != nil {
		t.Fatal(err)
	}
}

// minimal stand-in matching the server.Tunnel shape SaveTunnel needs.
var tunnelFixture = serverTunnel{ID: "tn1", Name: "web", Type: "tcp", RemotePort: 9000, LocalAddr: "127.0.0.1:3000"}

type serverTunnel = SaveTunnelArg

// newStoreWithDB opens a fresh test database and returns both the *Store and
// the underlying *sql.DB so tests can insert rows bypassing the store layer.
func newStoreWithDB(t *testing.T) (*Store, *sql.DB) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(d); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return New(d), d
}

func TestRevokeThenAuthenticateFails(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	if err := s.SeedAdmin(ctx, "a@x", "pw"); err != nil {
		t.Fatal(err)
	}
	u, err := s.GetUserByEmail(ctx, "a@x")
	if err != nil {
		t.Fatal(err)
	}
	pt, err := s.IssueClientToken(ctx, u.ID, "x")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, pt); err != nil {
		t.Fatalf("Authenticate before revoke: %v", err)
	}
	tokens, err := s.ListClientTokens(ctx, u.ID)
	if err != nil || len(tokens) == 0 {
		t.Fatalf("ListClientTokens: %v %v", tokens, err)
	}
	if err := s.RevokeClientToken(ctx, tokens[0].ID, u.ID); err != nil {
		t.Fatalf("RevokeClientToken: %v", err)
	}
	if _, err := s.Authenticate(ctx, pt); err == nil {
		t.Fatal("Authenticate after revoke must error")
	}
	err = s.RevokeClientToken(ctx, "does-not-exist", u.ID)
	if err == nil {
		t.Fatal("revoking non-existent token must return error")
	}
	if !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("expected db.ErrNotFound, got %v", err)
	}
}

func TestVerifyUserPasswordWrong(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	if err := s.SeedAdmin(ctx, "a@x", "pw"); err != nil {
		t.Fatal(err)
	}
	ok, err := s.VerifyUserPassword(ctx, "a@x", "WRONG")
	if ok || err != nil {
		t.Fatalf("wrong password: ok=%v err=%v", ok, err)
	}
	ok, err = s.VerifyUserPassword(ctx, "missing@x", "pw")
	if ok || err != nil {
		t.Fatalf("unknown email: ok=%v err=%v", ok, err)
	}
}

func TestValidateSessionExpiry(t *testing.T) {
	ctx := context.Background()
	st, sqlDB := newStoreWithDB(t)
	q := db.Wrap(sqlDB)

	// Seed a user directly through the db layer.
	if err := q.CreateUser(ctx, db.User{ID: "u1", Email: "a@x", PasswordHash: "h", Role: "admin"}); err != nil {
		t.Fatal(err)
	}

	// Create a valid session via the store.
	id, err := st.CreateSession(ctx, "u1", "ua", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	uid, err := st.ValidateSession(ctx, id)
	if err != nil || uid != "u1" {
		t.Fatalf("ValidateSession live: uid=%s err=%v", uid, err)
	}

	// Insert an already-expired session directly via the db layer.
	if err := q.CreateSession(ctx, db.Session{
		ID:        "exp",
		UserID:    "u1",
		ExpiresAt: time.Now().UTC().Add(-time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	// ValidateSession must return ErrUnauthorized for the expired session.
	_, err = st.ValidateSession(ctx, "exp")
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expired session: expected ErrUnauthorized, got %v", err)
	}

	// The expired session must have been deleted best-effort by ValidateSession.
	_, err = q.GetSession(ctx, "exp")
	if !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("expired session should be deleted, got %v", err)
	}
}
