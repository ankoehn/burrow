package server

import (
	"sync"

	"github.com/hashicorp/yamux"
)

// Tunnel is a registered (Phase 2: bookkeeping only) tunnel.
type Tunnel struct {
	ID         string
	Name       string
	Type       string
	RemotePort int
	LocalAddr  string
}

// ClientSession is one authenticated client connection.
type ClientSession struct {
	SessionID  string
	RemoteAddr string
	Yamux      *yamux.Session
	mu         sync.Mutex
	Tunnels    map[string]*Tunnel
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
