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

// Options configures a Server.
type Options struct {
	Listen  string
	TLSCert string
	TLSKey  string
	Token   string
	Logger  *slog.Logger
}

// Server is the burrowd relay control server (Phase 2: control plane only).
type Server struct {
	opts Options
	log  *slog.Logger
	reg  *Registry
	tlsC *tls.Config

	mu sync.Mutex
	ln net.Listener
	wg sync.WaitGroup
}

// New validates options and loads the TLS keypair.
func New(o Options) (*Server, error) {
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	cert, err := tls.LoadX509KeyPair(o.TLSCert, o.TLSKey)
	if err != nil {
		return nil, fmt.Errorf("load tls keypair: %w", err)
	}
	return &Server{
		opts: o, log: o.Logger, reg: NewRegistry(),
		tlsC: &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12},
	}, nil
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
	cs, err := HandleHandshake(conn, s.opts.Token, sid)
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

	ctrl, err := ysess.AcceptStream()
	if err != nil {
		return
	}
	cs.SetControl(ctrl)
	go s.heartbeat(ctx, ysess, cs)
	RunControlLoop(ctrl, s.reg, cs)
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
