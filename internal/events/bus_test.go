package events

import (
	"sync"
	"testing"
	"time"
)

func TestSubscribePublishDeliver(t *testing.T) {
	b := NewBus()
	ch, cancel := b.Subscribe("u1")
	defer cancel()
	b.PublishTunnelsChanged("u1")
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("expected a notification")
	}
}

func TestPerUserIsolation(t *testing.T) {
	b := NewBus()
	chA, cancelA := b.Subscribe("A")
	defer cancelA()
	chB, cancelB := b.Subscribe("B")
	defer cancelB()
	b.PublishTunnelsChanged("A")
	select {
	case <-chA:
	case <-time.After(time.Second):
		t.Fatal("A should receive")
	}
	select {
	case <-chB:
		t.Fatal("B must not receive A's event")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestPublishNonBlockingCoalesces(t *testing.T) {
	b := NewBus()
	_, cancel := b.Subscribe("u1") // never drained
	defer cancel()
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			b.PublishTunnelsChanged("u1") // must never block on the full cap-1 buffer
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("PublishTunnelsChanged blocked on a full subscriber buffer")
	}
}

func TestCancelUnsubscribes(t *testing.T) {
	b := NewBus()
	ch, cancel := b.Subscribe("u1")
	cancel()
	cancel() // idempotent, must not panic
	if _, ok := <-ch; ok {
		t.Fatal("channel must be closed after cancel")
	}
	b.PublishTunnelsChanged("u1") // no subscribers; must not panic
}

func TestNoGoroutineOrMapLeak(t *testing.T) {
	b := NewBus()
	var cancels []func()
	for i := 0; i < 100; i++ {
		_, c := b.Subscribe("u1")
		cancels = append(cancels, c)
	}
	var wg sync.WaitGroup
	for _, c := range cancels {
		wg.Add(1)
		go func(f func()) { defer wg.Done(); f() }(c)
	}
	wg.Wait()
	if n := b.subscriberCount("u1"); n != 0 {
		t.Fatalf("want 0 subscribers after all cancel, got %d", n)
	}
}

func TestConcurrentPublishSubscribeCancel(t *testing.T) {
	b := NewBus()
	const goroutines = 50
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(3)
		go func() { defer wg.Done(); b.PublishTunnelsChanged("u1") }()
		go func() {
			defer wg.Done()
			_, cancel := b.Subscribe("u1")
			cancel()
		}()
		go func() { defer wg.Done(); b.PublishTunnelsChanged("u1") }()
	}
	wg.Wait()
	if n := b.subscriberCount("u1"); n != 0 {
		t.Fatalf("want 0 subscribers after all cancel, got %d", n)
	}
}
