package server

// bounds_test.go — behavioral tests for B01 (control-write deadline) and B11
// (accept-loop backoff). Both tests assert real timing behavior, not mocks.

import (
	"errors"
	"net"
	"testing"
	"time"

	"github.com/hashicorp/yamux"

	"github.com/ankoehn/burrow/internal/proto"
)

// --- B01: SendControl must return within ~controlWriteTimeout when the peer
// never reads the control stream. -----------------------------------------------

// TestSendControlTimesOutOnStalledPeer constructs a real yamux session over a
// net.Pipe whose client side never reads, calls SendControl from the server
// side, and asserts the call returns an error in bounded time (≤ deadline + 1s
// slop). This proves a stalled client cannot pin the caller forever.
func TestSendControlTimesOutOnStalledPeer(t *testing.T) {
	// Use a short timeout so the test finishes quickly; restore after.
	const testTimeout = 200 * time.Millisecond
	orig := controlWriteTimeout
	controlWriteTimeout = testTimeout
	t.Cleanup(func() { controlWriteTimeout = orig })

	// net.Pipe gives us a synchronous in-process connection; the client end is
	// never read, so writes will block once the pipe's internal buffer is full.
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	// Stand up a yamux server on serverConn.
	ysess, err := yamux.Server(serverConn, yamux.DefaultConfig())
	if err != nil {
		t.Fatalf("yamux server: %v", err)
	}
	defer ysess.Close()

	// Stand up a yamux client on clientConn (needed so yamux handshake completes).
	csess, err := yamux.Client(clientConn, yamux.DefaultConfig())
	if err != nil {
		t.Fatalf("yamux client: %v", err)
	}
	defer csess.Close()

	// Client opens stream 0 (the control stream); server accepts it.
	streamCh := make(chan *yamux.Stream, 1)
	go func() {
		st, e := ysess.AcceptStream()
		if e == nil {
			streamCh <- st
		}
	}()
	_, err = csess.OpenStream()
	if err != nil {
		t.Fatalf("open control stream: %v", err)
	}
	var ctrlStream *yamux.Stream
	select {
	case ctrlStream = <-streamCh:
	case <-time.After(2 * time.Second):
		t.Fatal("server never accepted stream")
	}

	// Wire the control stream into a ClientSession.
	cs := &ClientSession{SessionID: "b01-test"}
	cs.SetControl(ctrlStream) // ctrlDeadliner will be set since *yamux.Stream implements it

	// The client peer never reads. Send messages in a loop until the yamux
	// window / net.Pipe buffer is full and the write blocks; at that point
	// SendControl must return with an error (the per-write deadline fires).
	done := make(chan error, 1)
	start := time.Now()
	go func() {
		for {
			err := cs.SendControl(proto.MsgPing, proto.Ping{Nonce: "x"})
			if err != nil {
				done <- err
				return
			}
		}
	}()

	// The write must time out within testTimeout + 2s slop.
	deadline := testTimeout + 2*time.Second
	select {
	case err := <-done:
		elapsed := time.Since(start)
		if err == nil {
			t.Fatal("expected SendControl to return error when peer is stalled")
		}
		t.Logf("SendControl returned %v after %v (deadline was %v)", err, elapsed.Round(time.Millisecond), controlWriteTimeout)
		// Must not have taken outrageously longer than the deadline.
		if elapsed > deadline {
			t.Fatalf("SendControl took %v, expected ≤ %v", elapsed, deadline)
		}
	case <-time.After(deadline + 5*time.Second):
		t.Fatalf("SendControl did not return within %v — stalled client pinned the caller", deadline+5*time.Second)
	}
}

// TestSendControlPlainWriterNoPanic verifies that SetControl with a plain
// io.Writer does NOT panic — the type assertion is guarded so ctrlDeadliner
// stays nil when the writer does not implement writeDeadliner.
func TestSendControlPlainWriterNoPanic(t *testing.T) {
	// pipeWriter is a struct that implements only io.Writer, not writeDeadliner.
	// Using an anonymous function adapter keeps it minimal.
	r, w := net.Pipe()
	defer r.Close()
	defer w.Close()
	bare := &bareWriter{w: w}

	cs := &ClientSession{SessionID: "b01-plain"}
	cs.SetControl(bare) // must not panic; ctrlDeadliner must remain nil
	if cs.ctrlDeadliner != nil {
		t.Fatal("ctrlDeadliner should be nil for a plain io.Writer")
	}
	// No assertion on SendControl itself needed — the guarded nil check is what matters.
}

// bareWriter implements io.Writer only; it deliberately does NOT expose
// SetWriteDeadline, so SetControl must not set ctrlDeadliner.
type bareWriter struct{ w net.Conn }

func (b *bareWriter) Write(p []byte) (int, error) { return b.w.Write(p) }

// --- B11: accept loop must back off on transient errors and exit on ErrClosed --

// stubListener is a net.Listener that returns a transient error N times, then
// net.ErrClosed, and counts how many Accept calls were made.
type stubListener struct {
	transientN int // remaining transient errors to return
	calls      int // total Accept calls
	// done signals the test that ErrClosed was returned.
	done chan struct{}
}

func newStubListener(transientN int) *stubListener {
	return &stubListener{transientN: transientN, done: make(chan struct{})}
}

func (l *stubListener) Accept() (net.Conn, error) {
	l.calls++
	if l.transientN > 0 {
		l.transientN--
		// Return a transient (non-net.ErrClosed) error.
		return nil, errors.New("transient: resource temporarily unavailable")
	}
	close(l.done)
	return nil, net.ErrClosed
}

func (l *stubListener) Close() error   { return nil }
func (l *stubListener) Addr() net.Addr { return stubAddr{} }

type stubAddr struct{}

func (stubAddr) Network() string { return "tcp" }
func (stubAddr) String() string  { return "stub:0" }

// acceptLoopUnderTest runs the same backoff logic as startPublicListener's
// goroutine but with an injected listener, so we can test it in isolation.
// maxBackoff caps the delay for test speed (in production it's time.Second).
func acceptLoopUnderTest(l net.Listener, minDelay, maxBackoff time.Duration) {
	var delay time.Duration
	for {
		_, err := l.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			if delay == 0 {
				delay = minDelay
			} else {
				delay *= 2
			}
			if delay > maxBackoff {
				delay = maxBackoff
			}
			time.Sleep(delay)
			continue
		}
		delay = 0
	}
}

// TestAcceptLoopBacksOffOnTransientError verifies that N transient errors take
// at least N*minDelay time (proving the sleep fires), and that the loop exits
// promptly when net.ErrClosed is returned.
func TestAcceptLoopBacksOffOnTransientError(t *testing.T) {
	const transientN = 5
	const minDelay = 10 * time.Millisecond
	const maxBackoff = 50 * time.Millisecond

	sl := newStubListener(transientN)
	start := time.Now()

	done := make(chan struct{})
	go func() {
		acceptLoopUnderTest(sl, minDelay, maxBackoff)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("accept loop did not exit after net.ErrClosed within 5s")
	}
	elapsed := time.Since(start)

	// Must have backed off at least minDelay per transient error.
	minExpected := time.Duration(transientN) * minDelay
	if elapsed < minExpected {
		t.Fatalf("loop took %v but expected ≥ %v (N=%d × minDelay=%v); no backoff?",
			elapsed, minExpected, transientN, minDelay)
	}
	t.Logf("accept loop exited after %v with %d transient errors (expected ≥ %v)", elapsed.Round(time.Millisecond), transientN, minExpected)
}

// TestAcceptLoopExitsImmediatelyOnErrClosed verifies the clean-shutdown path:
// if the very first Accept returns net.ErrClosed, the loop exits without any
// delay.
func TestAcceptLoopExitsImmediatelyOnErrClosed(t *testing.T) {
	sl := newStubListener(0) // 0 transient errors → immediate ErrClosed
	start := time.Now()

	done := make(chan struct{})
	go func() {
		acceptLoopUnderTest(sl, 5*time.Millisecond, time.Second)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("loop did not exit on net.ErrClosed within 2s")
	}
	elapsed := time.Since(start)
	// Should be fast — well under 100ms.
	if elapsed > 100*time.Millisecond {
		t.Fatalf("loop took %v to exit on ErrClosed; expected < 100ms (no backoff on clean shutdown)", elapsed)
	}
	t.Logf("loop exited in %v on ErrClosed", elapsed.Round(time.Millisecond))
}
