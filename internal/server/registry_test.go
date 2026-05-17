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
