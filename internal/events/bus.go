// Package events is a standalone, non-blocking, per-user pub/sub bus.
// It carries content-free "your tunnels changed, refetch" notifications
// (never data), so a publisher (the serial control loop / byte ticker)
// can never block on a slow SSE subscriber.
package events

import "sync"

// Bus fans out per-user "tunnels changed" notifications.
type Bus struct {
	mu   sync.Mutex
	subs map[string]map[*subscriber]struct{}
}

type subscriber struct{ ch chan struct{} }

// NewBus returns an empty Bus.
func NewBus() *Bus { return &Bus{subs: make(map[string]map[*subscriber]struct{})} }

// Subscribe registers a subscriber for userID and returns its receive channel
// plus an idempotent cancel that unsubscribes and closes the channel.
func (b *Bus) Subscribe(userID string) (<-chan struct{}, func()) {
	s := &subscriber{ch: make(chan struct{}, 1)}
	b.mu.Lock()
	if b.subs[userID] == nil {
		b.subs[userID] = make(map[*subscriber]struct{})
	}
	b.subs[userID][s] = struct{}{}
	b.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			b.mu.Lock()
			if m := b.subs[userID]; m != nil {
				delete(m, s)
				if len(m) == 0 {
					delete(b.subs, userID)
				}
			}
			b.mu.Unlock()
			close(s.ch)
		})
	}
	return s.ch, cancel
}

// PublishTunnelsChanged signals every subscriber of userID. It never blocks:
// if a subscriber's cap-1 buffer is full the notification is coalesced.
func (b *Bus) PublishTunnelsChanged(userID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for s := range b.subs[userID] {
		select {
		case s.ch <- struct{}{}:
		default:
		}
	}
}

// subscriberCount reports live subscribers for userID (test helper).
func (b *Bus) subscriberCount(userID string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs[userID])
}

// SubscriberCountForTest reports live subscribers for userID (cross-package tests).
func (b *Bus) SubscriberCountForTest(userID string) int { return b.subscriberCount(userID) }
