package server

import (
	"testing"
	"time"
)

func TestPendingStreamsResolveAndTimeout(t *testing.T) {
	p := newPendingStreams()
	go func() { time.Sleep(20 * time.Millisecond); p.Resolve("a", nil) }()
	if _, err := p.Await("a", time.Second, nil); err != nil {
		t.Fatalf("await a: %v", err)
	}
	if _, err := p.Await("missing", 50*time.Millisecond, nil); err == nil {
		t.Fatal("expected timeout for missing id")
	}
	if p.Resolve("nobody", nil) {
		t.Fatal("Resolve should report false when no waiter")
	}
}

func TestPortAllocator(t *testing.T) {
	pa := newPortAllocator(9000, 9001)
	p1, err := pa.Allocate(0)
	if err != nil || p1 < 9000 || p1 > 9001 {
		t.Fatalf("auto allocate: %d %v", p1, err)
	}
	p2, err := pa.Allocate(0)
	if err != nil || p2 == p1 {
		t.Fatalf("second auto: %d %v", p2, err)
	}
	if _, err := pa.Allocate(0); err == nil {
		t.Fatal("expected exhaustion error")
	}
	if _, err := pa.Allocate(p1); err == nil {
		t.Fatal("expected in-use error for explicit port")
	}
	pa.Release(p1)
	if _, err := pa.Allocate(p1); err != nil {
		t.Fatalf("re-allocate after release: %v", err)
	}
	if _, err := pa.Allocate(70000); err == nil {
		t.Fatal("expected out-of-range error")
	}
}
