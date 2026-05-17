package proto

import (
	"bytes"
	"testing"
)

func TestEveryMessageTypeRoundTrips(t *testing.T) {
	cases := []struct {
		typ MessageType
		val any
	}{
		{MsgAuthRequest, AuthRequest{ProtocolVersion: 1, Token: "tok", ClientVersion: "v", OS: "linux", Arch: "amd64"}},
		{MsgAuthResponse, AuthResponse{OK: true, SessionID: "s1"}},
		{MsgTunnelRegister, TunnelRegister{Name: "web", Type: "tcp", RemotePort: 9000, LocalAddr: "127.0.0.1:3000"}},
		{MsgTunnelRegisterResp, TunnelRegisterResponse{OK: true, TunnelID: "t1", RemotePort: 9000}},
		{MsgTunnelUnregister, TunnelUnregister{TunnelID: "t1"}},
		{MsgNewConnection, NewConnection{TunnelID: "t1", StreamID: "st1", SourceIP: "1.2.3.4:5"}},
		{MsgPing, Ping{Nonce: "n"}},
		{MsgPong, Pong{Nonce: "n"}},
		{MsgError, Error{Message: "boom"}},
		{MsgStreamOpen, StreamHeader{StreamID: "st-1", TunnelID: "tn-1"}},
	}
	for _, c := range cases {
		var buf bytes.Buffer
		if err := WriteMessage(&buf, c.typ, c.val); err != nil {
			t.Fatalf("%s WriteMessage: %v", c.typ, err)
		}
		var env Envelope
		if err := ReadFrame(&buf, &env); err != nil {
			t.Fatalf("%s ReadFrame: %v", c.typ, err)
		}
		if env.Type != c.typ {
			t.Fatalf("%s: got type %s", c.typ, env.Type)
		}
	}
}
