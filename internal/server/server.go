package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/yamux"
)

// TokenAuthenticator validates a client's plaintext token, returning its user id.
type TokenAuthenticator interface {
	Authenticate(ctx context.Context, token string) (userID string, err error)
}

// TunnelStore persists tunnel rows (best-effort; never blocks the data path).
// Implementations MUST be fast and non-blocking: SaveTunnel is called inline on
// the serial control loop, and MarkTunnelSeen is called at session/tunnel
// teardown to record the last-observed state (it is NOT a liveness heartbeat).
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

// EventPublisher receives "this user's tunnels changed" notifications
// (best-effort, must never block the control loop). *events.Bus satisfies it.
type EventPublisher interface {
	PublishTunnelsChanged(userID string)
}

type noopEventPublisher struct{}

func (noopEventPublisher) PublishTunnelsChanged(string) {}

// TunnelView is a read-only snapshot of a live tunnel for the HTTP API.
type TunnelView struct {
	ID         string
	Name       string
	Type       string
	RemotePort int
	LocalAddr  string
	BytesIn    uint64
	BytesOut   uint64
	Connected  bool
}

// Options configures a Server.
type Options struct {
	Listen     string
	TLSCert    string
	TLSKey     string
	Auth       TokenAuthenticator
	Tunnels    TunnelStore
	Events     EventPublisher
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
	if o.Events == nil {
		o.Events = noopEventPublisher{}
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

	s.wg.Add(1)
	go s.byteTicker(ctx)

	// B11: capped exponential backoff on transient accept errors (EMFILE etc.)
	// mirrors the net/http tempDelay pattern. Exits promptly on ctx cancel.
	var delay time.Duration
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				s.wg.Wait()
				return nil
			default:
			}
			s.log.Warn("accept", "err", err)
			if delay == 0 {
				delay = 5 * time.Millisecond
			} else {
				delay *= 2
			}
			if delay > time.Second {
				delay = time.Second
			}
			time.Sleep(delay)
			continue
		}
		delay = 0 // reset on success
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handleConn(ctx, conn)
		}()
	}
}

// yamuxConfig returns the yamux session configuration used by the server.
// Dead-peer detection relies on yamux's built-in keepalive; callers MUST NOT
// disable EnableKeepAlive (a CI test in keepalive_test.go enforces this).
func yamuxConfig() *yamux.Config {
	return yamux.DefaultConfig()
}

func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	sid := uuid.NewString()
	cs, err := HandleHandshake(conn, s.opts.Auth, sid)
	if err != nil {
		s.log.Warn("handshake failed", "remote_addr", conn.RemoteAddr().String(), "err", err)
		return
	}
	ysess, err := yamux.Server(conn, yamuxConfig())
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
		if cs.UserID != "" {
			s.opts.Events.PublishTunnelsChanged(cs.UserID)
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

// heartbeat closes the yamux session when ctx is cancelled; the session's own
// keepalive (EnableKeepAlive=true, KeepAliveInterval=30s — see yamuxConfig) will
// close it autonomously if the peer goes silent, so no additional ping/pong
// deadline logic is needed here.
func (s *Server) heartbeat(ctx context.Context, y *yamux.Session, _ *ClientSession) {
	select {
	case <-ctx.Done():
		_ = y.Close()
	case <-y.CloseChan():
	}
}

// byteTicker publishes a per-user "tunnels changed" ping ~1/s while any of
// that user's tunnels are live, so dashboards refresh byte counters. It is
// WaitGroup-tracked and exits on ctx cancellation.
func (s *Server) byteTicker(ctx context.Context) {
	defer s.wg.Done()
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			seen := map[string]struct{}{}
			for _, cs := range s.reg.Sessions() {
				if cs.UserID == "" {
					continue
				}
				if _, dup := seen[cs.UserID]; dup {
					continue
				}
				if len(s.reg.snapshotTunnels(cs)) > 0 {
					seen[cs.UserID] = struct{}{}
					// Best-effort invalidate ping; a publish for a mid-teardown
					// session is harmless (client refetches and sees the update).
					s.opts.Events.PublishTunnelsChanged(cs.UserID)
				}
			}
		}
	}
}

// ListUserTunnels returns a snapshot of the live tunnels owned by userID.
func (s *Server) ListUserTunnels(userID string) []TunnelView {
	var out []TunnelView
	for _, cs := range s.reg.Sessions() {
		if cs.UserID != userID {
			continue
		}
		for _, tn := range s.reg.snapshotTunnels(cs) {
			out = append(out, TunnelView{
				ID: tn.ID, Name: tn.Name, Type: tn.Type, RemotePort: tn.RemotePort,
				LocalAddr: tn.LocalAddr, BytesIn: tn.BytesIn.Load(), BytesOut: tn.BytesOut.Load(),
				Connected: true,
			})
		}
	}
	return out
}
