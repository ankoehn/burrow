package store

import (
	"context"
	"path/filepath"
	"testing"

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
