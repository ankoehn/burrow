package server

import (
	"context"
	"net"
	"testing"

	"github.com/ankoehn/burrow/internal/proto"
)

// namedFakeAuth also reports a token name (AuthenticateNamed).
type namedFakeAuth struct{ uid, name string }

func (n namedFakeAuth) Authenticate(context.Context, string) (string, error) {
	return n.uid, nil
}
func (n namedFakeAuth) AuthenticateNamed(_ context.Context, _ string) (string, string, error) {
	return n.uid, n.name, nil
}

func TestHandshakeCapturesMetadata(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()
	go func() {
		_ = proto.WriteMessage(c2, proto.MsgAuthRequest, proto.AuthRequest{
			ProtocolVersion: 1, Token: "t", ClientVersion: "9.9",
			OS: "linux", Arch: "amd64", Hostname: "box-1",
		})
		var resp proto.Envelope
		_ = proto.ReadFrame(c2, &resp)
	}()
	cs, err := HandleHandshake(c1, namedFakeAuth{uid: "u1", name: "laptop"}, "sid-1")
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	if cs.OS != "linux" || cs.Arch != "amd64" || cs.ClientVersion != "9.9" {
		t.Fatalf("metadata not captured: %+v", cs)
	}
	if cs.TokenName != "laptop" {
		t.Fatalf("token name not captured: %q", cs.TokenName)
	}
}
