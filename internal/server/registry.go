package server

import (
	"io"
	"net"
	"sync"
	"sync/atomic"

	"github.com/hashicorp/yamux"

	"github.com/ankoehn/burrow/internal/proto"
)

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
}

// ClientSession is one authenticated client connection.
type ClientSession struct {
	SessionID  string
	RemoteAddr string
	Yamux      *yamux.Session
	mu         sync.Mutex
	Tunnels    map[string]*Tunnel

	pending *pendingStreams
	ctrlMu  sync.Mutex
	ctrl    io.Writer
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

// SetControl records the control stream used for serialized server→client writes.
func (cs *ClientSession) SetControl(w io.Writer) {
	cs.ctrlMu.Lock()
	cs.ctrl = w
	cs.ctrlMu.Unlock()
}

// SendControl writes one control message to the client, serialized against all
// other senders (control-loop replies and visitor notifies).
func (cs *ClientSession) SendControl(typ proto.MessageType, payload any) error {
	cs.ctrlMu.Lock()
	defer cs.ctrlMu.Unlock()
	if cs.ctrl == nil {
		return io.ErrClosedPipe
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
