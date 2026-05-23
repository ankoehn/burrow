package server

import (
	"sync"
	"testing"
)

func TestRegistryAddRemoveConcurrent(t *testing.T) {
	r := NewRegistry()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			cs := &ClientSession{SessionID: id(n)}
			r.AddSession(cs)
			r.AddTunnel(cs, &Tunnel{ID: id(n), RemotePort: 9000 + n})
			r.RemoveSession(cs)
		}(i)
	}
	wg.Wait()
	if got := len(r.Sessions()); got != 0 {
		t.Fatalf("expected 0 sessions after removal, got %d", got)
	}
}

func TestRegistryTunnelLifecycle(t *testing.T) {
	r := NewRegistry()
	cs := &ClientSession{SessionID: "s"}
	r.AddSession(cs)
	tn := &Tunnel{ID: "t1", RemotePort: 9000}
	r.AddTunnel(cs, tn)
	if len(cs.Tunnels) != 1 {
		t.Fatalf("want 1 tunnel, got %d", len(cs.Tunnels))
	}
	r.RemoveTunnel(cs, "t1")
	if len(cs.Tunnels) != 0 {
		t.Fatalf("tunnel not removed")
	}
}

func id(n int) string { return "s" + string(rune('A'+n%26)) + string(rune('0'+n/26)) }

// TestLookupSessionByTunnelID pins the v0.5.2 BACKLOG #1 fast path: a
// server-level O(1) probe that returns (sessionID, userID, ok) for a tunnel
// runtime ID. Replaces the O(N sessions x M tunnels) SnapshotSessions scan
// in cmd/server/proxy_wiring.go::lookupSessionFields.
func TestLookupSessionByTunnelID(t *testing.T) {
	// Build a minimal Server with a populated registry. We don't call New()
	// because that requires a TLS keypair; instead we construct the struct
	// directly so the test stays focused on registry semantics.
	srv := &Server{reg: NewRegistry()}

	cs := &ClientSession{SessionID: "session-A", UserID: "user-1"}
	srv.reg.AddSession(cs)
	tn := &Tunnel{ID: "tunnel-X", RemotePort: 9000}
	srv.reg.AddTunnel(cs, tn)

	gotSession, gotUser, ok := srv.LookupSessionByTunnelID("tunnel-X")
	if !ok {
		t.Fatal("LookupSessionByTunnelID(tunnel-X): want ok=true")
	}
	if gotSession != "session-A" {
		t.Errorf("sessionID=%q; want session-A", gotSession)
	}
	if gotUser != "user-1" {
		t.Errorf("userID=%q; want user-1", gotUser)
	}

	if _, _, ok := srv.LookupSessionByTunnelID("nonexistent"); ok {
		t.Error("LookupSessionByTunnelID(nonexistent): want ok=false")
	}

	// Removing the tunnel makes the lookup miss.
	srv.reg.RemoveTunnel(cs, "tunnel-X")
	if _, _, ok := srv.LookupSessionByTunnelID("tunnel-X"); ok {
		t.Error("after RemoveTunnel: want ok=false")
	}

	// Re-add tunnel and then remove the owning session — lookup must miss.
	srv.reg.AddTunnel(cs, tn)
	if _, _, ok := srv.LookupSessionByTunnelID("tunnel-X"); !ok {
		t.Fatal("after re-add: want ok=true")
	}
	srv.reg.RemoveSession(cs)
	if _, _, ok := srv.LookupSessionByTunnelID("tunnel-X"); ok {
		t.Error("after RemoveSession: want ok=false (index must follow session removal)")
	}
}
