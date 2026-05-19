package db

import (
	"context"
	"testing"
)

func TestTunnelTotalsAndAccessMode(t *testing.T) {
	ctx := context.Background()
	x := testDB(t)
	_ = x.CreateUser(ctx, User{ID: "u1", Email: "a@b.c", PasswordHash: "h", Role: "user"})
	_ = x.CreateUser(ctx, User{ID: "u2", Email: "x@y.z", PasswordHash: "h", Role: "user"})
	if err := x.UpsertTunnel(ctx, Tunnel{ID: "tn1", UserID: "u1", Name: "n", Type: "tcp", RemotePort: 9000, LocalAddr: "127.0.0.1:1"}); err != nil {
		t.Fatal(err)
	}

	g, err := x.GetTunnel(ctx, "tn1")
	if err != nil || g.AccessMode != "open" || g.TotalBytesIn != 0 || g.LastFlushedAt != nil {
		t.Fatalf("fresh tunnel: %+v %v", g, err)
	}
	if _, err := x.GetTunnel(ctx, "nope"); err != ErrNotFound {
		t.Fatalf("GetTunnel missing: want ErrNotFound, got %v", err)
	}

	if err := x.FlushTunnelTotals(ctx, "tn1", 100, 40); err != nil {
		t.Fatal(err)
	}
	if err := x.FlushTunnelTotals(ctx, "tn1", 5, 6); err != nil {
		t.Fatal(err)
	}
	g, _ = x.GetTunnel(ctx, "tn1")
	if g.TotalBytesIn != 105 || g.TotalBytesOut != 46 || g.LastFlushedAt == nil {
		t.Fatalf("after flush: %+v", g)
	}

	// access mode scoped to owner
	if err := x.SetTunnelAccessMode(ctx, "tn1", "u2", "api_key"); err != ErrNotFound {
		t.Fatalf("cross-user set: want ErrNotFound, got %v", err)
	}
	if err := x.SetTunnelAccessMode(ctx, "tn1", "u1", "api_key"); err != nil {
		t.Fatal(err)
	}
	g, _ = x.GetTunnel(ctx, "tn1")
	if g.AccessMode != "api_key" {
		t.Fatalf("access_mode not persisted: %q", g.AccessMode)
	}
}
