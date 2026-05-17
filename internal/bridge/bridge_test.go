package bridge

import (
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ankoehn/burrow/internal/testutil"
)

// tcpPipe creates a pair of connected TCP connections that support CloseWrite.
// net.Pipe() returns *net.pipe which does not implement CloseWrite on Go 1.26.
func tcpPipe(t *testing.T) (net.Conn, net.Conn) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	ch := make(chan net.Conn, 1)
	go func() {
		c, _ := ln.Accept()
		ch <- c
	}()
	client, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	server := <-ch
	return client, server
}

func TestPipeCopiesBothWaysAndCounts(t *testing.T) {
	defer testutil.AssertNoGoroutineLeak(t)()
	a1, a2 := tcpPipe(t) // "visitor" side: write to a1, Pipe reads a2
	b1, b2 := tcpPipe(t) // "target" side: Pipe writes b1, read b2
	var in, out atomic.Uint64
	done := make(chan struct{})
	go func() { Pipe(a2, b1, &in, &out); close(done) }()

	_, _ = a1.Write([]byte("ping"))
	got := make([]byte, 4)
	_ = b2.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadFull(b2, got); err != nil || string(got) != "ping" {
		t.Fatalf("target got %q err=%v", got, err)
	}
	_, _ = b2.Write([]byte("pong!"))
	r := make([]byte, 5)
	_ = a1.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadFull(a1, r); err != nil || string(r) != "pong!" {
		t.Fatalf("visitor got %q err=%v", r, err)
	}
	_ = a1.Close()
	_ = b2.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Pipe did not return after both sides closed")
	}
	if in.Load() != 4 || out.Load() != 5 {
		t.Fatalf("counters in=%d out=%d (want 4/5)", in.Load(), out.Load())
	}
}

func TestPipeClosesBothWhenOneEnds(t *testing.T) {
	defer testutil.AssertNoGoroutineLeak(t)()
	a2, aPeer := tcpPipe(t)
	b1, bPeer := tcpPipe(t)
	var in, out atomic.Uint64
	done := make(chan struct{})
	go func() { Pipe(a2, b1, &in, &out); close(done) }()
	_ = aPeer.Close() // one side ends → Pipe must close the other and return
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Pipe did not return when one side closed")
	}
	if _, err := bPeer.Read(make([]byte, 1)); err == nil {
		t.Fatal("other side was not closed")
	}
}
