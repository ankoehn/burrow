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
func (s *Server) RunControlLoop(stream io.ReadWriteCloser, reg *Registry, cs *ClientSession) {
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
				_ = cs.SendControl(proto.MsgError, proto.Error{Message: "bad tunnel_register"})
				continue
			}
			port, perr := s.ports.Allocate(tr.RemotePort)
			if perr != nil {
				_ = cs.SendControl(proto.MsgTunnelRegisterResp, proto.TunnelRegisterResponse{OK: false, Error: perr.Error()})
				continue
			}
			tn := &Tunnel{ID: uuid.NewString(), Name: tr.Name, Type: tr.Type, RemotePort: port, LocalAddr: tr.LocalAddr, sess: cs}
			if lerr := s.startPublicListener(tn); lerr != nil {
				s.ports.Release(port)
				_ = cs.SendControl(proto.MsgTunnelRegisterResp, proto.TunnelRegisterResponse{OK: false, Error: lerr.Error()})
				continue
			}
			reg.AddTunnel(cs, tn)
			s.log.Info("tunnel registered", "tunnel_id", tn.ID, "remote_port", port, "session_id", cs.SessionID)
			_ = cs.SendControl(proto.MsgTunnelRegisterResp, proto.TunnelRegisterResponse{OK: true, TunnelID: tn.ID, RemotePort: port})
		case proto.MsgTunnelUnregister:
			var tu proto.TunnelUnregister
			if err := proto.DecodePayload(env, &tu); err == nil {
				if tn := reg.Tunnel(cs, tu.TunnelID); tn != nil && tn.Listener != nil {
					_ = tn.Listener.Close()
					s.ports.Release(tn.RemotePort)
				}
				reg.RemoveTunnel(cs, tu.TunnelID)
			}
		case proto.MsgPing:
			var p proto.Ping
			_ = proto.DecodePayload(env, &p)
			_ = cs.SendControl(proto.MsgPong, proto.Pong{Nonce: p.Nonce})
		case proto.MsgPong:
			// handled by heartbeat monitor (Task 9); ignore here
		default:
			_ = cs.SendControl(proto.MsgError, proto.Error{Message: "unexpected: " + string(env.Type)})
		}
	}
}
