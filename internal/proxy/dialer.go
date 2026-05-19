// Package proxy implements the vhost reverse-proxy data plane for Burrow.
//
// It routes HTTPS requests arriving on the shared ingress port to live HTTP
// tunnels by matching the subdomain label against the in-memory tunnel
// registry.
//
// Trust boundary: Burrow is the TLS edge. All inbound X-Forwarded-* and
// Forwarded headers from visitors are stripped and replaced with authoritative
// values set by this proxy.
//
// h2c is explicitly not supported. Any request carrying "Upgrade: h2c" is
// answered with HTTP 505 (HTTP Version Not Supported). This is documented and
// permanent: Burrow's ingress is HTTP/1.1 over TLS; h2c (cleartext HTTP/2
// upgrade) has no place in this stack.
//
// WebSocket upgrades are handled transparently by httputil.ReverseProxy (Go
// ≥ 1.20). No extra code is required; the Upgrade and Connection headers pass
// through to the upstream as-is.
//
// Gate handler: requests to the auth domain under /__burrow/* are dispatched
// to an optional gate http.Handler injected via New. When the gate is nil (the
// default, and for all tests in this package), those requests return 404.
// Task 9 wires the real gate handler.
package proxy

import (
	"context"
	"errors"
	"net"
)

// ErrNotFound is returned by StreamDialer methods when no live tunnel exists
// for the requested subdomain (or the tunnel has gone away).
var ErrNotFound = errors.New("proxy: tunnel not found")

// Resolved carries the service metadata that the proxy needs after a
// successful Lookup. It is populated by StreamDialer.Lookup and threaded
// through the access checker before any stream is opened.
//
// Fields:
//   - ServiceID: stable service identity (matches store.Service.ID).
//   - AccessMode: one of "open", "api_key", or "burrow_login".
//   - APIKeyHeader: name of the HTTP header the upstream uses for its API key
//     (only meaningful when AccessMode == "api_key").
//   - LocalHost: the Host header and URL host to use when forwarding to the
//     upstream. Typically the host part of the tunnel's LocalAddr
//     (e.g. "127.0.0.1" for "127.0.0.1:3000"). When empty the proxy falls
//     back to the tunnel's full LocalAddr string.
type Resolved struct {
	ServiceID    string
	AccessMode   string
	APIKeyHeader string
	LocalHost    string
}

// StreamDialer is the interface that the proxy uses to:
//  1. Look up service metadata for a subdomain (Lookup) — called before
//     access checking so no stream is opened for denied requests.
//  2. Open a new yamux stream to the tunnel's client (DialTunnelStream) —
//     called only after access has been granted, once per proxied request.
//
// The concrete implementation (a proxyDialerAdapter wrapping *server.Server)
// is wired in Task 12. Tests use a fake backed by net.Pipe().
type StreamDialer interface {
	// Lookup returns the service metadata for subdomain.
	// Returns ErrNotFound when no live HTTP tunnel with that subdomain exists.
	Lookup(ctx context.Context, subdomain string) (*Resolved, error)

	// DialTunnelStream opens a new yamux stream for an individual HTTP
	// request to the tunnel identified by subdomain.
	// Returns ErrNotFound if the tunnel has gone away since Lookup.
	// The returned net.Conn is the stream; the caller is responsible for
	// closing it after the request completes.
	DialTunnelStream(ctx context.Context, subdomain string) (net.Conn, error)
}
