package server

import (
	"testing"
	"time"
)

// TestYamuxConfigKeepAliveEnabled guards the dead-peer detection invariant:
// the yamux configuration used by the server MUST have EnableKeepAlive=true and
// a sane KeepAliveInterval. If someone later disables keepalive, this test fails
// CI and prevents silent removal of the only liveness mechanism.
func TestYamuxConfigKeepAliveEnabled(t *testing.T) {
	cfg := yamuxConfig()
	if !cfg.EnableKeepAlive {
		t.Fatal("yamuxConfig: EnableKeepAlive must be true — yamux keepalive is the authoritative dead-peer detection mechanism for the MVP")
	}
	if cfg.KeepAliveInterval <= 0 || cfg.KeepAliveInterval > 60*time.Second {
		t.Fatalf("yamuxConfig: KeepAliveInterval=%v must be in (0, 60s] — sane range for dead-peer detection", cfg.KeepAliveInterval)
	}
}
