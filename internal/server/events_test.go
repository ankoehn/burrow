package server

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ankoehn/burrow/internal/devcert"
	"github.com/ankoehn/burrow/internal/proto"
)

func newEvtServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	if err := devcert.Generate(dir, true); err != nil {
		t.Fatal(err)
	}
	s, err := New(Options{
		Listen:  "127.0.0.1:0",
		TLSCert: filepath.Join(dir, "dev-server.pem"),
		TLSKey:  filepath.Join(dir, "dev-server-key.pem"),
		Auth:    fakeAuth{uid: "u1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestNewDefaultsEventsToNoop(t *testing.T) {
	s := newEvtServer(t)
	if s.opts.Events == nil {
		t.Fatal("New must default Events to a non-nil noop publisher")
	}
	s.opts.Events.PublishTunnelsChanged("u1") // must not panic
}

func TestListUserTunnelsEmpty(t *testing.T) {
	s := newEvtServer(t)
	if got := s.ListUserTunnels("nobody"); len(got) != 0 {
		t.Fatalf("want 0 tunnels, got %d", len(got))
	}
}

// recordPub records every PublishTunnelsChanged call for assertion.
type recordPub struct {
	mu  sync.Mutex
	ids []string
}

func (p *recordPub) PublishTunnelsChanged(userID string) {
	p.mu.Lock()
	p.ids = append(p.ids, userID)
	p.mu.Unlock()
}

func (p *recordPub) snapshot() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.ids...)
}

// TestRegisterPublishesTunnelsChanged mirrors the exact harness from
// TestControlLoopRegisterAndPing but injects a recordPub so we can assert
// that RunControlLoop calls PublishTunnelsChanged exactly once on a successful
// tunnel_register. This is a genuine regression guard: removing control.go's
// s.opts.Events.PublishTunnelsChanged call would leave rp.ids empty and fail
// the len==1 assertion.
func TestRegisterPublishesTunnelsChanged(t *testing.T) {
	cli, srv := dialPair()
	defer cli.Close()

	reg := NewRegistry()
	cs := &ClientSession{SessionID: "s2", UserID: "u1", Tunnels: map[string]*Tunnel{}}
	reg.AddSession(cs)
	cs.SetControl(srv)

	rp := &recordPub{}
	s := &Server{
		opts:  Options{PublicBind: "127.0.0.1", PortMin: 18100, PortMax: 18199, Tunnels: noopTunnelStore{}, Events: rp},
		log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		ports: newPortAllocator(18100, 18199),
	}
	t.Cleanup(func() {
		for _, tn := range reg.snapshotTunnels(cs) {
			if tn.Listener != nil {
				_ = tn.Listener.Close()
			}
			s.ports.Release(tn.RemotePort)
		}
	})

	go s.RunControlLoop(srv, reg, cs)

	_ = proto.WriteMessage(cli, proto.MsgTunnelRegister, proto.TunnelRegister{
		Name: "web", Type: "tcp", RemotePort: 18100, LocalAddr: "127.0.0.1:3000",
	})
	var env proto.Envelope
	cli.SetReadDeadline(time.Now().Add(time.Second))
	_ = proto.ReadFrame(cli, &env)
	if env.Type != proto.MsgTunnelRegisterResp {
		t.Fatalf("want register response, got %v", env.Type)
	}
	var rr proto.TunnelRegisterResponse
	_ = proto.DecodePayload(env, &rr)
	if !rr.OK {
		t.Fatalf("register failed: %+v", rr)
	}

	// The publish is synchronous in RunControlLoop (no goroutine), so it has
	// already fired before the response was sent. No sleep needed.
	got := rp.snapshot()
	if len(got) != 1 {
		t.Fatalf("want exactly 1 publish after register, got %d: %v", len(got), got)
	}
	if got[0] != "u1" {
		t.Fatalf("want publish for userID %q, got %q", "u1", got[0])
	}
}

// TestByteTickerPublishesForLiveTunnel starts a real Server with a recordPub,
// injects a live session+tunnel directly into s.reg (same package), and asserts
// that byteTicker emits at least one publish for the tunnel owner within 2s.
func TestByteTickerPublishesForLiveTunnel(t *testing.T) {
	dir := t.TempDir()
	if err := devcert.Generate(dir, true); err != nil {
		t.Fatal(err)
	}
	rp := &recordPub{}
	s, err := New(Options{
		Listen:  "127.0.0.1:0",
		TLSCert: filepath.Join(dir, "dev-server.pem"),
		TLSKey:  filepath.Join(dir, "dev-server-key.pem"),
		Auth:    fakeAuth{uid: "u1"},
		Events:  rp,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = s.Serve(ctx) }()
	defer func() { cancel(); s.Wait() }()
	waitListening(t, s)

	// Wire a live session with one tunnel directly into the server's registry.
	cs := &ClientSession{SessionID: "bt-s1", UserID: "u1", Tunnels: map[string]*Tunnel{}}
	tn := &Tunnel{ID: "bt-t1", Name: "bt", Type: "tcp", RemotePort: 0, LocalAddr: "127.0.0.1:1"}
	s.reg.AddSession(cs)
	s.reg.AddTunnel(cs, tn)
	t.Cleanup(func() { s.reg.RemoveSession(cs) })

	// Poll up to 2s for byteTicker to fire at least once for "u1".
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snap := rp.snapshot()
		found := false
		for _, id := range snap {
			if id == "u1" {
				found = true
				break
			}
		}
		if found {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("byteTicker did not publish for 'u1' within 2s")
}
