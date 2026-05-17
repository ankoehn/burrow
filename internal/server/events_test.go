package server

import (
	"path/filepath"
	"testing"

	"github.com/ankoehn/burrow/internal/devcert"
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
