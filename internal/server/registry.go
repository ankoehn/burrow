package server

import (
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hashicorp/yamux"

	"github.com/ankoehn/burrow/internal/proto"
)

// controlWriteTimeout is the per-write deadline applied to control-stream
// writes (B01). Keeps a stalled-but-not-disconnected client from pinning the
// visitor goroutine and its socket indefinitely.
// Declared as a var so tests can inject a shorter value without touching the
// production constant.
var controlWriteTimeout = 10 * time.Second

// writeDeadliner is the subset of net.Conn / *yamux.Stream we need to set
// per-write deadlines. Injecting it separately lets tests use a plain
// io.Writer (e.g. bytes.Buffer) without panic.
type writeDeadliner interface {
	SetWriteDeadline(t time.Time) error
}

// Tunnel is a registered tunnel.
type Tunnel struct {
	ID         string
	Name       string
	Type       string
	RemotePort int
	LocalAddr  string

	// Listener is the public port listener (Phase 3); nil until started.
	Listener net.Listener
	// BytesIn counts visitor→local bytes; BytesOut counts local→visitor.
	BytesIn  atomic.Uint64
	BytesOut atomic.Uint64
	// sess is the owning session (for control-channel notifies).
	sess *ClientSession

	// HTTP tunnel fields (only set when IsHTTP==true).
	Subdomain string // assigned subdomain; "" for tcp tunnels
	ServiceID string // stable service identity from ServiceResolver; "" for tcp tunnels
	IsHTTP    bool   // true for http-mode tunnels (no TCP port allocated)
}

// ClientSession is one authenticated client connection.
type ClientSession struct {
	SessionID  string
	UserID     string
	RemoteAddr string
	// Handshake metadata (best-effort, from AuthRequest). Zero values when a
	// client predates these fields. Hostname is reserved (v0.3) and unused.
	OS            string
	Arch          string
	ClientVersion string
	TokenName     string
	Yamux         *yamux.Session
	mu            sync.Mutex
	Tunnels       map[string]*Tunnel

	pending       *pendingStreams
	ctrlMu        sync.Mutex
	ctrl          io.Writer
	ctrlDeadliner writeDeadliner // non-nil when ctrl also supports SetWriteDeadline
}

// Registry tracks live sessions and their tunnels (in-memory, mutex-guarded).
type Registry struct {
	mu       sync.RWMutex
	sessions map[string]*ClientSession
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{sessions: make(map[string]*ClientSession)}
}

// AddSession registers a session (initialising its tunnel map).
func (r *Registry) AddSession(cs *ClientSession) {
	if cs.Tunnels == nil {
		cs.Tunnels = make(map[string]*Tunnel)
	}
	r.mu.Lock()
	r.sessions[cs.SessionID] = cs
	r.mu.Unlock()
}

// RemoveSession drops a session.
func (r *Registry) RemoveSession(cs *ClientSession) {
	r.mu.Lock()
	delete(r.sessions, cs.SessionID)
	r.mu.Unlock()
}

// AddTunnel records a tunnel under a session.
func (r *Registry) AddTunnel(cs *ClientSession, t *Tunnel) {
	cs.mu.Lock()
	if cs.Tunnels == nil {
		cs.Tunnels = make(map[string]*Tunnel)
	}
	cs.Tunnels[t.ID] = t
	cs.mu.Unlock()
}

// RemoveTunnel drops a tunnel from a session.
func (r *Registry) RemoveTunnel(cs *ClientSession, tunnelID string) {
	cs.mu.Lock()
	delete(cs.Tunnels, tunnelID)
	cs.mu.Unlock()
}

// Sessions returns a snapshot slice of live sessions.
func (r *Registry) Sessions() []*ClientSession {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*ClientSession, 0, len(r.sessions))
	for _, s := range r.sessions {
		out = append(out, s)
	}
	return out
}

// Tunnel returns the named tunnel for a session, or nil.
func (r *Registry) Tunnel(cs *ClientSession, id string) *Tunnel {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return cs.Tunnels[id]
}

// snapshotTunnels returns a copy of a session's tunnels (safe to range after).
func (r *Registry) snapshotTunnels(cs *ClientSession) []*Tunnel {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	out := make([]*Tunnel, 0, len(cs.Tunnels))
	for _, t := range cs.Tunnels {
		out = append(out, t)
	}
	return out
}

// snapshotTunnelsForTest exposes a session's tunnels for white-box tests.
func (cs *ClientSession) snapshotTunnelsForTest() []*Tunnel {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	out := make([]*Tunnel, 0, len(cs.Tunnels))
	for _, t := range cs.Tunnels {
		out = append(out, t)
	}
	return out
}

// SetControl records the control stream used for serialized server→client writes.
// If w also implements writeDeadliner (e.g. *yamux.Stream, net.Conn), per-write
// deadlines will be applied automatically by SendControl.
func (cs *ClientSession) SetControl(w io.Writer) {
	cs.ctrlMu.Lock()
	if d, ok := w.(writeDeadliner); ok {
		cs.ctrlDeadliner = d
	} else {
		cs.ctrlDeadliner = nil
	}
	cs.ctrl = w
	cs.ctrlMu.Unlock()
}

// SendControl writes one control message to the client, serialized against all
// other senders (control-loop replies and visitor notifies).
// B01: each write is bounded by controlWriteTimeout so a stalled client cannot
// pin the caller's goroutine/socket indefinitely. The deadline is cleared after
// each write so it is per-write, not cumulative.
func (cs *ClientSession) SendControl(typ proto.MessageType, payload any) error {
	cs.ctrlMu.Lock()
	defer cs.ctrlMu.Unlock()
	if cs.ctrl == nil {
		return io.ErrClosedPipe
	}
	if cs.ctrlDeadliner != nil {
		_ = cs.ctrlDeadliner.SetWriteDeadline(time.Now().Add(controlWriteTimeout))
		defer func() { _ = cs.ctrlDeadliner.SetWriteDeadline(time.Time{}) }()
	}
	return proto.WriteMessage(cs.ctrl, typ, payload)
}

// Pending returns the session's pending-stream registry (lazily created).
func (cs *ClientSession) Pending() *pendingStreams {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if cs.pending == nil {
		cs.pending = newPendingStreams()
	}
	return cs.pending
}
