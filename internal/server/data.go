package server

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/hashicorp/yamux"
)

// pendingStreams pairs "a visitor is waiting for stream X" with "the client
// just opened stream X".
type pendingStreams struct {
	mu      sync.Mutex
	waiters map[string]chan *yamux.Stream
}

func newPendingStreams() *pendingStreams {
	return &pendingStreams{waiters: make(map[string]chan *yamux.Stream)}
}

// Await registers a waiter for id and blocks until Resolve(id,...) or timeout.
func (p *pendingStreams) Await(id string, timeout time.Duration) (*yamux.Stream, error) {
	ch := make(chan *yamux.Stream, 1)
	p.mu.Lock()
	p.waiters[id] = ch
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		delete(p.waiters, id)
		p.mu.Unlock()
	}()
	select {
	case s := <-ch:
		return s, nil
	case <-time.After(timeout):
		return nil, errors.New("timeout waiting for client stream")
	}
}

// Resolve hands stream to a waiter for id; returns false if none was waiting.
func (p *pendingStreams) Resolve(id string, stream *yamux.Stream) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	ch, ok := p.waiters[id]
	if !ok {
		return false
	}
	ch <- stream
	return true
}

// portAllocator hands out free TCP ports in [min,max].
type portAllocator struct {
	mu       sync.Mutex
	min, max int
	used     map[int]bool
}

func newPortAllocator(min, max int) *portAllocator {
	return &portAllocator{min: min, max: max, used: make(map[int]bool)}
}

// Allocate reserves requested (or, if 0, the first free port in range).
func (a *portAllocator) Allocate(requested int) (int, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if requested != 0 {
		if requested < a.min || requested > a.max {
			return 0, fmt.Errorf("port %d out of range %d-%d", requested, a.min, a.max)
		}
		if a.used[requested] {
			return 0, fmt.Errorf("port %d already in use", requested)
		}
		a.used[requested] = true
		return requested, nil
	}
	for p := a.min; p <= a.max; p++ {
		if !a.used[p] {
			a.used[p] = true
			return p, nil
		}
	}
	return 0, errors.New("no free ports in range")
}

// Release returns a port to the pool.
func (a *portAllocator) Release(port int) {
	a.mu.Lock()
	delete(a.used, port)
	a.mu.Unlock()
}
