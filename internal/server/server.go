package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"sync"

	"github.com/google/uuid"
	"github.com/hashicorp/yamux"
)

// TokenAuthenticator validates a client's plaintext token, returning its user id.
type TokenAuthenticator interface {
	Authenticate(ctx context.Context, token string) (userID string, err error)
}

// TunnelStore persists tunnel rows (best-effort; never blocks the data path).
type TunnelStore interface {
	SaveTunnel(ctx context.Context, userID string, t *Tunnel) error
	MarkTunnelSeen(ctx context.Context, tunnelID string) error
}

// AuthFunc adapts a function to TokenAuthenticator.
type AuthFunc func(ctx context.Context, token string) (string, error)

// Authenticate implements TokenAuthenticator.
func (f AuthFunc) Authenticate(ctx context.Context, token string) (string, error) {
	return f(ctx, token)
}

// noopTunnelStore is the default TunnelStore: it persists nothing.
type noopTunnelStore struct{}

func (noopTunnelStore) SaveTunnel(context.Context, string, *Tunnel) error { return nil }
func (noopTunnelStore) MarkTunnelSeen(context.Context, string) error      { return nil }

// Options configures a Server.
type Options struct {
	Listen     string
	TLSCert    string
	TLSKey     string
	Auth       TokenAuthenticator
	Tunnels    TunnelStore
	Logger     *slog.Logger
	PublicBind string
	PortMin    int
	PortMax    int
}

// Server is the burrowd relay control server.
type Server struct {
	opts  Options
	log   *slog.Logger
	reg   *Registry
	tlsC  *tls.Config
	ports *portAllocator

	mu sync.Mutex
	ln net.Listener
	wg sync.WaitGroup
}

// New validates options and loads the TLS keypair.
func New(o Options) (*Server, error) {
	if o.Auth == nil {
		return nil, fmt.Errorf("server: Auth (TokenAuthenticator) is required")
	}
	if o.Tunnels == nil {
		o.Tunnels = noopTunnelStore{}
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.PublicBind == "" {
		o.PublicBind = "0.0.0.0"
	}
	if o.PortMin == 0 {
		o.PortMin = 9000
	}
	if o.PortMax == 0 {
		o.PortMax = 9100
	}
	cert, err := tls.LoadX509KeyPair(o.TLSCert, o.TLSKey)
	if err != nil {
		return nil, fmt.Errorf("load tls keypair: %w", err)
	}
	s := &Server{
		opts: o, log: o.Logger, reg: NewRegistry(),
		tlsC: &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12},
	}
	s.ports = newPortAllocator(o.PortMin, o.PortMax)
	return s, nil
}

// Addr returns the bound listen address ("" until listening).
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ln == nil {
		return ""
	}
	return s.ln.Addr().String()
}

// Wait blocks until all connection goroutines have exited.
func (s *Server) Wait() { s.wg.Wait() }

// Serve listens until ctx is cancelled.
func (s *Server) Serve(ctx context.Context) error {
	ln, err := tls.Listen("tcp", s.opts.Listen, s.tlsC)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.opts.Listen, err)
	}
	s.mu.Lock()
	s.ln = ln
	s.mu.Unlock()
	s.log.Info("control server listening", "addr", ln.Addr().String())

	go func() { <-ctx.Done(); _ = ln.Close() }()
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				s.wg.Wait()
				return nil
			default:
				s.log.Warn("accept", "err", err)
				continue
			}
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handleConn(ctx, conn)
		}()
	}
}

func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	sid := uuid.NewString()
	cs, err := HandleHandshake(conn, s.opts.Auth, sid)
	if err != nil {
		s.log.Warn("handshake failed", "remote_addr", conn.RemoteAddr().String(), "err", err)
		return
	}
	ysess, err := yamux.Server(conn, yamux.DefaultConfig())
	if err != nil {
		s.log.Warn("yamux server", "err", err)
		return
	}
	defer ysess.Close()
	cs.Yamux = ysess
	s.reg.AddSession(cs)
	defer s.reg.RemoveSession(cs)
	s.log.Info("client authenticated", "session_id", cs.SessionID, "remote_addr", cs.RemoteAddr)

	defer func() {
		for _, tn := range s.reg.snapshotTunnels(cs) {
			if tn.Listener != nil {
				_ = tn.Listener.Close()
			}
			s.ports.Release(tn.RemotePort)
			_ = s.opts.Tunnels.MarkTunnelSeen(context.Background(), tn.ID)
		}
	}()

	ctrl, err := ysess.AcceptStream()
	if err != nil {
		return
	}
	cs.SetControl(ctrl)
	go func() {
		for {
			st, e := ysess.AcceptStream()
			if e != nil {
				return
			}
			go s.handleDataStream(cs, st)
		}
	}()
	go s.heartbeat(ctx, ysess, cs)
	s.RunControlLoop(ctrl, s.reg, cs)
	s.log.Info("client disconnected", "session_id", cs.SessionID)
}

// heartbeat closes the session if ctx is cancelled or the yamux keepalive dies.
func (s *Server) heartbeat(ctx context.Context, y *yamux.Session, _ *ClientSession) {
	select {
	case <-ctx.Done():
		_ = y.Close()
	case <-y.CloseChan():
	}
}
