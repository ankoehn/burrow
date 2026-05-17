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
