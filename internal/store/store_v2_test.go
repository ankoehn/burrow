package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ankoehn/burrow/internal/db"
)

func testStore(t *testing.T) *Store {
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

func TestStoreRolesAndSettings(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)

	roles, err := s.ListRoles(ctx)
	if err != nil || len(roles) != 2 {
		t.Fatalf("ListRoles: n=%d err=%v", len(roles), err)
	}
	rd, err := s.GetRole(ctx, "admin")
	if err != nil || rd.Name != "admin" || len(rd.Permissions) == 0 {
		t.Fatalf("GetRole admin: %+v %v", rd, err)
	}
	if _, err := s.GetRole(ctx, "ghost"); err == nil {
		t.Fatal("GetRole ghost must error")
	}

	if err := s.SaveSettings(ctx, map[string]string{"smtp.host": "mx", "smtp.port": "587"}); err != nil {
		t.Fatal(err)
	}
	m, err := s.GetSettings(ctx)
	if err != nil || m["smtp.host"] != "mx" {
		t.Fatalf("GetSettings: %+v %v", m, err)
	}
}

func TestStoreUserStatusSuspendRevokesSessions(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)
	u, err := s.CreateUser(ctx, "a@b.c", "password1", "user")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateSession(ctx, u.ID, "UA", "1.1.1.1"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetUserStatus(ctx, u.ID, "bogus"); err != ErrInvalidStatus {
		t.Fatalf("bad status: want ErrInvalidStatus, got %v", err)
	}
	if err := s.SetUserStatus(ctx, u.ID, "suspended"); err != nil {
		t.Fatal(err)
	}
	ss, _ := s.ListSessions(ctx, u.ID)
	if len(ss) != 0 {
		t.Fatalf("suspend must revoke sessions, %d remain", len(ss))
	}
	if err := s.UpdateUserRole(ctx, u.ID, "nope"); err != ErrInvalidRole {
		t.Fatalf("bad role: want ErrInvalidRole, got %v", err)
	}
}

func TestStoreTunnelAccessModeAndTotals(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)
	u, _ := s.CreateUser(ctx, "a@b.c", "password1", "user")
	if err := s.SaveTunnel(ctx, u.ID, &SaveTunnelArg{ID: "tn1", Name: "n", Type: "tcp", RemotePort: 9000, LocalAddr: "127.0.0.1:1"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTunnelAccessMode(ctx, "tn1", u.ID, "weird"); err != ErrInvalidAccessMode {
		t.Fatalf("bad mode: want ErrInvalidAccessMode, got %v", err)
	}
	if err := s.SetTunnelAccessMode(ctx, "tn1", u.ID, "api_key"); err != nil {
		t.Fatal(err) // persisted-but-inert is allowed
	}
	if err := s.FlushTunnelTotals(ctx, "tn1", 10, 3); err != nil {
		t.Fatal(err)
	}
	tn, err := s.GetTunnel(ctx, "tn1")
	if err != nil || tn.TotalBytesIn != 10 || tn.AccessMode != "api_key" {
		t.Fatalf("GetTunnel: %+v %v", tn, err)
	}
}
