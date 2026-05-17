package server

import (
	"crypto/subtle"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/google/uuid"

	"github.com/ankoehn/burrow/internal/proto"
)

const authReadTimeout = 10 * time.Second

// HandleHandshake reads the auth frame from a raw conn, validates the token in
// constant time, replies auth_response, and returns a new ClientSession on
// success. On failure it writes an error/auth_response and returns nil.
func HandleHandshake(conn net.Conn, expectedToken, sessionID string) (*ClientSession, error) {
	_ = conn.SetReadDeadline(time.Now().Add(authReadTimeout))
	var env proto.Envelope
	if err := proto.ReadFrame(conn, &env); err != nil {
		return nil, fmt.Errorf("read auth frame: %w", err)
	}
	if env.Type != proto.MsgAuthRequest {
		_ = proto.WriteMessage(conn, proto.MsgError, proto.Error{Message: "expected auth_request"})
		return nil, fmt.Errorf("first message was %s", env.Type)
	}
	var ar proto.AuthRequest
	if err := proto.DecodePayload(env, &ar); err != nil {
		_ = proto.WriteMessage(conn, proto.MsgError, proto.Error{Message: "bad auth payload"})
		return nil, err
	}
	if subtle.ConstantTimeCompare([]byte(ar.Token), []byte(expectedToken)) != 1 {
		_ = proto.WriteMessage(conn, proto.MsgAuthResponse, proto.AuthResponse{OK: false, Error: "invalid token"})
		return nil, fmt.Errorf("invalid token")
	}
	_ = conn.SetReadDeadline(time.Time{}) // clear deadline
	if err := proto.WriteMessage(conn, proto.MsgAuthResponse, proto.AuthResponse{OK: true, SessionID: sessionID}); err != nil {
		return nil, err
	}
	return &ClientSession{SessionID: sessionID, RemoteAddr: conn.RemoteAddr().String(), Tunnels: map[string]*Tunnel{}}, nil
}

// RunControlLoop processes control-stream messages until the stream closes.
func RunControlLoop(stream io.ReadWriteCloser, reg *Registry, cs *ClientSession) {
	defer stream.Close()
	for {
		var env proto.Envelope
		if err := proto.ReadFrame(stream, &env); err != nil {
			return
		}
		switch env.Type {
		case proto.MsgTunnelRegister:
			var tr proto.TunnelRegister
			if err := proto.DecodePayload(env, &tr); err != nil {
				_ = proto.WriteMessage(stream, proto.MsgError, proto.Error{Message: "bad tunnel_register"})
				continue
			}
			tn := &Tunnel{ID: uuid.NewString(), Name: tr.Name, Type: tr.Type, RemotePort: tr.RemotePort, LocalAddr: tr.LocalAddr}
			if tn.RemotePort == 0 {
				tn.RemotePort = 9000 // Phase 2: simple assignment; Phase 3 binds the listener
			}
			reg.AddTunnel(cs, tn)
			_ = proto.WriteMessage(stream, proto.MsgTunnelRegisterResp, proto.TunnelRegisterResponse{OK: true, TunnelID: tn.ID, RemotePort: tn.RemotePort})
		case proto.MsgTunnelUnregister:
			var tu proto.TunnelUnregister
			if err := proto.DecodePayload(env, &tu); err == nil {
				reg.RemoveTunnel(cs, tu.TunnelID)
			}
		case proto.MsgPing:
			var p proto.Ping
			_ = proto.DecodePayload(env, &p)
			_ = proto.WriteMessage(stream, proto.MsgPong, proto.Pong{Nonce: p.Nonce})
		case proto.MsgPong:
			// handled by heartbeat monitor (Task 9); ignore here
		default:
			_ = proto.WriteMessage(stream, proto.MsgError, proto.Error{Message: "unexpected: " + string(env.Type)})
		}
	}
}
