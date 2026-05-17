// Package backoff is a tiny exponential backoff with full jitter.
package backoff

import (
	"math/rand"
	"sync"
	"time"
)

// Backoff yields exponentially growing, fully-jittered delays capped at max.
type Backoff struct {
	mu       sync.Mutex
	min, max time.Duration
	attempt  uint
}

// New returns a Backoff producing delays in [0, min*2^attempt], capped at max.
func New(min, max time.Duration) *Backoff {
	return &Backoff{min: min, max: max}
}

// NextBackOff returns the next delay and advances the attempt counter.
func (b *Backoff) NextBackOff() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	d := float64(b.min) * float64(uint64(1)<<b.attempt)
	if d > float64(b.max) || d <= 0 {
		d = float64(b.max)
	} else {
		b.attempt++
	}
	return time.Duration(rand.Int63n(int64(d) + 1))
}

// Reset returns the backoff to its initial state.
func (b *Backoff) Reset() {
	b.mu.Lock()
	b.attempt = 0
	b.mu.Unlock()
}
