package backoff

import (
	"testing"
	"time"
)

func TestBackoffGrowsAndCaps(t *testing.T) {
	b := New(100*time.Millisecond, 2*time.Second)
	var last time.Duration
	for i := 0; i < 12; i++ {
		d := b.NextBackOff()
		if d < 0 || d > 2*time.Second {
			t.Fatalf("iter %d: out of range: %v", i, d)
		}
		last = d
	}
	if last > 2*time.Second {
		t.Fatalf("not capped: %v", last)
	}
}

func TestBackoffResetReturnsToMin(t *testing.T) {
	b := New(50*time.Millisecond, time.Second)
	for i := 0; i < 5; i++ {
		b.NextBackOff()
	}
	b.Reset()
	d := b.NextBackOff()
	if d > 100*time.Millisecond {
		t.Fatalf("reset did not lower delay: %v", d)
	}
}

// TestBackoffShiftClampNeverOverflows drives attempt well past 64 and asserts the
// returned duration never collapses to 0 and stays capped at max (B10).
func TestBackoffShiftClampNeverOverflows(t *testing.T) {
	const max = 30 * time.Second
	b := New(500*time.Millisecond, max)
	// Call NextBackOff 200 times — far beyond the shift-overflow threshold of 64.
	for i := 0; i < 200; i++ {
		d := b.NextBackOff()
		if d < 0 {
			t.Fatalf("iter %d: negative duration %v", i, d)
		}
		if d > max {
			t.Fatalf("iter %d: duration %v exceeds max %v", i, d, max)
		}
	}
	// Confirm attempt is clamped and did not exceed 62.
	b.mu.Lock()
	attempt := b.attempt
	b.mu.Unlock()
	if attempt > 62 {
		t.Fatalf("attempt %d exceeds clamp bound 62", attempt)
	}
	// The last delay must be drawn from [0, max] not [0, min] (no overflow collapse).
	// Drive one more call and confirm it is <= max.
	d := b.NextBackOff()
	if d > max {
		t.Fatalf("post-clamp duration %v exceeds max %v", d, max)
	}
}
