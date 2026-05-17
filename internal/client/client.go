// Package client implements the burrow control client.
package client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/hashicorp/yamux"

	"github.com/ankoehn/burrow/internal/backoff"
	"github.com/ankoehn/burrow/internal/proto"
	"github.com/ankoehn/burrow/internal/version"
)

// TunnelSpec is one tunnel to register.
type TunnelSpec struct {
	Name       string
	Type       string
	RemotePort int
	LocalAddr  string
}

// Options configures a Client.
type Options struct {
	Server     string
	Token      string
	Insecure   bool
	RootCAs    *x509.CertPool
	ServerName string
	Tunnels    []TunnelSpec
	Logger     *slog.Logger
}

// Client maintains an authenticated control session with auto-reconnect.
type Client struct {
	opts       Options
	log        *slog.Logger
	bo         *backoff.Backoff
	registered atomic.Bool
}

// New builds a Client.
func New(o Options) *Client {
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	return &Client{opts: o, log: o.Logger, bo: backoff.New(500*time.Millisecond, 30*time.Second)}
}

// Registered reports whether at least one tunnel is currently registered.
func (c *Client) Registered() bool { return c.registered.Load() }

func (c *Client) resetRegisteredForTest() { c.registered.Store(false) }

// Run connects and keeps reconnecting until ctx is cancelled.
func (c *Client) Run(ctx context.Context) error {
	for {
		if err := c.connectOnce(ctx); err != nil && ctx.Err() == nil {
			c.log.Warn("connection ended", "err", err)
		}
		c.registered.Store(false)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(c.bo.NextBackOff()):
		}
	}
}

func (c *Client) connectOnce(ctx context.Context) error {
	tlsCfg := &tls.Config{
		InsecureSkipVerify: c.opts.Insecure, //nolint:gosec // dev-only opt-in (spec D4)
		RootCAs:            c.opts.RootCAs,
		ServerName:         c.opts.ServerName,
		MinVersion:         tls.VersionTLS12,
	}
	d := &tls.Dialer{Config: tlsCfg}
	rawConn, err := d.DialContext(ctx, "tcp", c.opts.Server)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	conn := rawConn
	defer conn.Close()

	if err := proto.WriteMessage(conn, proto.MsgAuthRequest, proto.AuthRequest{
		ProtocolVersion: proto.ProtocolVersion, Token: c.opts.Token,
		ClientVersion: version.Version, OS: runtime.GOOS, Arch: runtime.GOARCH,
	}); err != nil {
		return err
	}
	var env proto.Envelope
	if err := proto.ReadFrame(conn, &env); err != nil {
		return err
	}
	var ar proto.AuthResponse
	if env.Type != proto.MsgAuthResponse || proto.DecodePayload(env, &ar) != nil || !ar.OK {
		return fmt.Errorf("auth failed: %s", ar.Error)
	}
	c.bo.Reset()
	c.log.Info("connected", "session_id", ar.SessionID)

	ysess, err := yamux.Client(conn, yamux.DefaultConfig())
	if err != nil {
		return err
	}
	defer ysess.Close()
	ctrl, err := ysess.OpenStream()
	if err != nil {
		return err
	}
	defer ctrl.Close()
	for _, tn := range c.opts.Tunnels {
		if err := proto.WriteMessage(ctrl, proto.MsgTunnelRegister, proto.TunnelRegister{
			Name: tn.Name, Type: tn.Type, RemotePort: tn.RemotePort, LocalAddr: tn.LocalAddr,
		}); err != nil {
			return err
		}
		if err := proto.ReadFrame(ctrl, &env); err != nil {
			return err
		}
		var rr proto.TunnelRegisterResponse
		if env.Type != proto.MsgTunnelRegisterResp || proto.DecodePayload(env, &rr) != nil || !rr.OK {
			return fmt.Errorf("register failed: %s", rr.Error)
		}
		c.log.Info("tunnel registered", "tunnel_id", rr.TunnelID, "remote_port", rr.RemotePort)
	}
	c.registered.Store(true)

	go c.pingLoop(ctx, ctrl)
	// Block until the session dies (server gone) or ctx cancelled.
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ysess.CloseChan():
		return fmt.Errorf("session closed")
	}
}

func (c *Client) pingLoop(ctx context.Context, ctrl *yamux.Stream) {
	t := time.NewTicker(60 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := proto.WriteMessage(ctrl, proto.MsgPing, proto.Ping{Nonce: "hb"}); err != nil {
				return
			}
		}
	}
}
