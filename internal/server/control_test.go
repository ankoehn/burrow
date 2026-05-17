package server

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/ankoehn/burrow/internal/proto"
)

// fakeAuth is a test TokenAuthenticator. Shared by control_test.go and
// server_test.go (both package server).
type fakeAuth struct {
	uid string
	err error
}

func (f fakeAuth) Authenticate(_ context.Context, tok string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	if tok == "" {
		return "", fmt.Errorf("empty token")
	}
	return f.uid, nil
}

func dialPair() (net.Conn, net.Conn) { return net.Pipe() }

func TestHandshakeSuccess(t *testing.T) {
	cli, srv := dialPair()
	defer cli.Close()
	done := make(chan *ClientSession, 1)
	go func() { cs, _ := HandleHandshake(srv, fakeAuth{uid: "u1"}, "sid-1"); done <- cs }()

	_ = proto.WriteMessage(cli, proto.MsgAuthRequest, proto.AuthRequest{ProtocolVersion: 1, Token: "good"})
	var env proto.Envelope
	cli.SetReadDeadline(time.Now().Add(time.Second))
	if err := proto.ReadFrame(cli, &env); err != nil || env.Type != proto.MsgAuthResponse {
		t.Fatalf("want auth_response, got %v err=%v", env.Type, err)
	}
	var ar proto.AuthResponse
	_ = proto.DecodePayload(env, &ar)
	if !ar.OK || ar.SessionID != "sid-1" {
		t.Fatalf("bad auth response: %+v", ar)
	}
	if cs := <-done; cs == nil || cs.SessionID != "sid-1" || cs.UserID != "u1" {
		t.Fatalf("handshake returned %+v", cs)
	}
}

func TestHandshakeBadToken(t *testing.T) {
	cli, srv := dialPair()
	defer cli.Close()
	go func() { _, _ = HandleHandshake(srv, fakeAuth{err: fmt.Errorf("nope")}, "sid") }()
	_ = proto.WriteMessage(cli, proto.MsgAuthRequest, proto.AuthRequest{ProtocolVersion: 1, Token: "bad"})
	var env proto.Envelope
	cli.SetReadDeadline(time.Now().Add(time.Second))
	_ = proto.ReadFrame(cli, &env)
	var ar proto.AuthResponse
	_ = proto.DecodePayload(env, &ar)
	if ar.OK {
		t.Fatal("bad token must be rejected")
	}
}

func TestHandshakeWrongFirstMessage(t *testing.T) {
	cli, srv := dialPair()
	defer cli.Close()
	go func() { _, _ = HandleHandshake(srv, fakeAuth{uid: "u1"}, "sid") }()
	_ = proto.WriteMessage(cli, proto.MsgPing, proto.Ping{})
	var env proto.Envelope
	cli.SetReadDeadline(time.Now().Add(time.Second))
	if err := proto.ReadFrame(cli, &env); err != nil || env.Type != proto.MsgError {
		t.Fatalf("want error message, got %v err=%v", env.Type, err)
	}
}

func TestControlLoopRegisterAndPing(t *testing.T) {
	cli, srv := dialPair()
	defer cli.Close()
	reg := NewRegistry()
	cs := &ClientSession{SessionID: "s", Tunnels: map[string]*Tunnel{}}
	reg.AddSession(cs)
	cs.SetControl(srv)
	s := &Server{
		opts:  Options{PublicBind: "127.0.0.1", PortMin: 18000, PortMax: 18099, Tunnels: noopTunnelStore{}},
		log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		ports: newPortAllocator(18000, 18099),
	}
	// Close all tunnel listeners on test cleanup to avoid port conflicts across runs.
	t.Cleanup(func() {
		for _, tn := range reg.snapshotTunnels(cs) {
			if tn.Listener != nil {
				_ = tn.Listener.Close()
			}
			s.ports.Release(tn.RemotePort)
		}
	})
	go s.RunControlLoop(srv, reg, cs)

	_ = proto.WriteMessage(cli, proto.MsgTunnelRegister, proto.TunnelRegister{Name: "web", Type: "tcp", RemotePort: 18000, LocalAddr: "127.0.0.1:3000"})
	var env proto.Envelope
	cli.SetReadDeadline(time.Now().Add(time.Second))
	_ = proto.ReadFrame(cli, &env)
	if env.Type != proto.MsgTunnelRegisterResp {
		t.Fatalf("want register response, got %v", env.Type)
	}
	var rr proto.TunnelRegisterResponse
	_ = proto.DecodePayload(env, &rr)
	if !rr.OK || rr.TunnelID == "" || rr.RemotePort < 18000 || rr.RemotePort > 18099 {
		t.Fatalf("bad register response: %+v", rr)
	}
	if len(cs.Tunnels) != 1 {
		t.Fatalf("tunnel not recorded")
	}
	_ = proto.WriteMessage(cli, proto.MsgPing, proto.Ping{Nonce: "n1"})
	_ = proto.ReadFrame(cli, &env)
	if env.Type != proto.MsgPong {
		t.Fatalf("want pong, got %v", env.Type)
	}
}
