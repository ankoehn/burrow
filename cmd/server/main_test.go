package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ankoehn/burrow/internal/api"
	"github.com/ankoehn/burrow/internal/db"
	"github.com/ankoehn/burrow/internal/server"
	"github.com/ankoehn/burrow/internal/store"
)

// TestAPIShutdownGraceExceedsHandlerTimeout asserts the compile-visible
// invariant that apiShutdownGrace (the timeout passed to http.Server.Shutdown)
// is strictly greater than api.JSONHandlerTimeout (the chi middleware.Timeout
// on JSON routes). If a future edit shrinks apiShutdownGrace below or equal to
// the chi handler timeout, in-flight handlers can outlive Shutdown and then
// touch a closed *sql.DB, producing spurious 500s on graceful stop.
func TestAPIShutdownGraceExceedsHandlerTimeout(t *testing.T) {
	if apiShutdownGrace <= api.JSONHandlerTimeout {
		t.Fatalf("apiShutdownGrace (%s) must be strictly greater than api.JSONHandlerTimeout (%s)",
			apiShutdownGrace, api.JSONHandlerTimeout)
	}
}

// TestTunnelStoreAdapterPersistsAllFields verifies that tunnelStoreAdapter
// maps every field of *server.Tunnel to the db layer without silently dropping
// any. Uses real SQLite so the whole stack (adapter → store → db) is exercised.
func TestTunnelStoreAdapterPersistsAllFields(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(d); err != nil {
		t.Fatal(err)
	}

	st := store.New(d)
	if err := st.SeedAdmin(context.Background(), "a@x", "pw"); err != nil {
		t.Fatal(err)
	}
	u, err := st.GetUserByEmail(context.Background(), "a@x")
	if err != nil {
		t.Fatal(err)
	}

	a := tunnelStoreAdapter{st}
	tn := &server.Tunnel{
		ID:         "tn-1",
		Name:       "web",
		Type:       "tcp",
		RemotePort: 9012,
		LocalAddr:  "127.0.0.1:3000",
	}
	if err := a.SaveTunnel(context.Background(), u.ID, tn); err != nil {
		t.Fatal(err)
	}

	tns, err := db.Wrap(d).ListTunnelsByUser(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tns) != 1 {
		t.Fatalf("expected 1 tunnel, got %d", len(tns))
	}
	got := tns[0]
	if got.ID != "tn-1" || got.Name != "web" || got.Type != "tcp" ||
		got.RemotePort != 9012 || got.LocalAddr != "127.0.0.1:3000" || got.UserID != u.ID {
		t.Fatalf("adapter dropped/garbled a field: %+v", got)
	}

	if err := a.MarkTunnelSeen(context.Background(), "tn-1"); err != nil {
		t.Fatal(err)
	}
}
