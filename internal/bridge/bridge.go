// Package bridge wires two net.Conns together for a tunnel data stream.
package bridge

import (
	"io"
	"net"
	"sync"
	"sync/atomic"
)

// Pipe copies a→b and b→a concurrently, adding the a→b byte count to in and
// the b→a count to out. When either direction finishes (EOF or error), BOTH
// conns are closed so the other Copy unblocks; Pipe returns once both are done.
// (yamux.Stream has no CloseWrite; close-both-on-first-EOF is the correct,
// panic-free TCP-proxy semantics — see spec DP3.)
func Pipe(a, b net.Conn, in, out *atomic.Uint64) {
	var once sync.Once
	closeBoth := func() { once.Do(func() { _ = a.Close(); _ = b.Close() }) }

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		n, _ := io.Copy(b, a)
		in.Add(uint64(n))
		closeBoth()
	}()
	go func() {
		defer wg.Done()
		n, _ := io.Copy(a, b)
		out.Add(uint64(n))
		closeBoth()
	}()
	wg.Wait()
}
