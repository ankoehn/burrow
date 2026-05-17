package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hashicorp/yamux"

	"github.com/ankoehn/burrow/internal/devcert"
	"github.com/ankoehn/burrow/internal/proto"
	"github.com/ankoehn/burrow/internal/testutil"
)

func TestServerEndToEndAuthRegister(t *testing.T) {
	defer testutil.AssertNoGoroutineLeak(t)()
	dir := t.TempDir()
	if err := devcert.Generate(dir, true); err != nil {
		t.Fatal(err)
	}
	srv, err := New(Options{
		Listen: "127.0.0.1:0", TLSCert: filepath.Join(dir, "dev-server.pem"),
		TLSKey: filepath.Join(dir, "dev-server-key.pem"), Token: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = srv.Serve(ctx) }()
	defer func() { cancel(); srv.Wait() }()
	waitListening(t, srv)

	caPEM, _ := os.ReadFile(filepath.Join(dir, "dev-ca.pem"))
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(caPEM)
	conn, err := tls.Dial("tcp", srv.Addr(), &tls.Config{RootCAs: pool, ServerName: "localhost"})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	_ = proto.WriteMessage(conn, proto.MsgAuthRequest, proto.AuthRequest{ProtocolVersion: 1, Token: "secret"})
	var env proto.Envelope
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if err := proto.ReadFrame(conn, &env); err != nil || env.Type != proto.MsgAuthResponse {
		t.Fatalf("auth: %v %v", env.Type, err)
	}
	sess, err := yamux.Client(conn, yamux.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	cs, err := sess.OpenStream()
	if err != nil {
		t.Fatal(err)
	}
	_ = proto.WriteMessage(cs, proto.MsgTunnelRegister, proto.TunnelRegister{Type: "tcp", RemotePort: 9001, LocalAddr: "127.0.0.1:3000"})
	_ = proto.ReadFrame(cs, &env)
	if env.Type != proto.MsgTunnelRegisterResp {
		t.Fatalf("want register resp, got %v", env.Type)
	}
}

func waitListening(t *testing.T, s *Server) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if s.Addr() != "" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("server never started listening")
}
