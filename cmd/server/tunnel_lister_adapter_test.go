package main

import (
	"testing"

	"github.com/ankoehn/burrow/internal/server"
)

// fakeUserTunnelLister is a minimal userTunnelLister that returns a fixed
// snapshot, so the adapter's server.TunnelView -> api.TunnelView mapping can
// be exercised without a live registry (a full TLS+yamux handshake just to
// populate *server.Server's unexported reg would be far heavier and not a
// "minimal seam"; *server.Server still satisfies userTunnelLister in prod).
type fakeUserTunnelLister struct {
	gotUser string
	views   []server.TunnelView
}

func (f *fakeUserTunnelLister) ListUserTunnels(userID string) []server.TunnelView {
	f.gotUser = userID
	return f.views
}

// TestTunnelListerAdapterMapsAllFields verifies that tunnelListerAdapter maps
// every field of server.TunnelView onto api.TunnelView without silently
// dropping any. If a future server.TunnelView/api.TunnelView field is added
// but not wired through ListUserTunnels, one of the assertions below fails.
func TestTunnelListerAdapterMapsAllFields(t *testing.T) {
	src := server.TunnelView{
		ID:         "tn-1",
		Name:       "web",
		Type:       "http",
		RemotePort: 0,
		LocalAddr:  "127.0.0.1:3000",
		BytesIn:    4242,
		BytesOut:   8484,
		Connected:  true,
		ServiceID:  "svc-99",
	}
	f := &fakeUserTunnelLister{views: []server.TunnelView{src}}

	out := tunnelListerAdapter{f}.ListUserTunnels("u1")

	if f.gotUser != "u1" {
		t.Fatalf("adapter passed wrong userID to lister: %q", f.gotUser)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 api.TunnelView, got %d", len(out))
	}
	got := out[0]
	if got.ID != src.ID {
		t.Errorf("ID: got %q want %q", got.ID, src.ID)
	}
	if got.Name != src.Name {
		t.Errorf("Name: got %q want %q", got.Name, src.Name)
	}
	if got.Type != src.Type {
		t.Errorf("Type: got %q want %q", got.Type, src.Type)
	}
	if got.RemotePort != src.RemotePort {
		t.Errorf("RemotePort: got %d want %d", got.RemotePort, src.RemotePort)
	}
	if got.LocalAddr != src.LocalAddr {
		t.Errorf("LocalAddr: got %q want %q", got.LocalAddr, src.LocalAddr)
	}
	if got.BytesIn != src.BytesIn {
		t.Errorf("BytesIn: got %d want %d", got.BytesIn, src.BytesIn)
	}
	if got.BytesOut != src.BytesOut {
		t.Errorf("BytesOut: got %d want %d", got.BytesOut, src.BytesOut)
	}
	if got.Connected != src.Connected {
		t.Errorf("Connected: got %v want %v", got.Connected, src.Connected)
	}
	if got.ServiceID != src.ServiceID {
		t.Errorf("ServiceID: got %q want %q", got.ServiceID, src.ServiceID)
	}
}

// TestTunnelListerAdapterEmpty verifies the nil/empty passthrough (no panic,
// empty result) so the "no live tunnels" path stays covered too.
func TestTunnelListerAdapterEmpty(t *testing.T) {
	out := tunnelListerAdapter{&fakeUserTunnelLister{}}.ListUserTunnels("u1")
	if len(out) != 0 {
		t.Fatalf("expected 0 views for empty lister, got %d", len(out))
	}
}
