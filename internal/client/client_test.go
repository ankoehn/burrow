package client

import (
	"context"
	"crypto/x509"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/ankoehn/burrow/internal/devcert"
	"github.com/ankoehn/burrow/internal/server"
	"github.com/ankoehn/burrow/internal/testutil"
)

func startServer(t *testing.T, dir, token string) (*server.Server, context.CancelFunc) {
	t.Helper()
	if err := devcert.Generate(dir, true); err != nil {
		t.Fatal(err)
	}
	s, err := server.New(server.Options{
		Listen: "127.0.0.1:0", TLSCert: filepath.Join(dir, "dev-server.pem"),
		TLSKey: filepath.Join(dir, "dev-server-key.pem"), Token: token,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = s.Serve(ctx) }()
	for i := 0; i < 100 && s.Addr() == ""; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	return s, cancel
}

func TestClientConnectsAndRegisters(t *testing.T) {
	defer testutil.AssertNoGoroutineLeak(t)()
	dir := t.TempDir()
	s, cancel := startServer(t, dir, "secret")
	defer func() { cancel(); s.Wait() }()
	caPEM, _ := os.ReadFile(filepath.Join(dir, "dev-ca.pem"))
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(caPEM)

	c := New(Options{
		Server: s.Addr(), Token: "secret", RootCAs: pool, ServerName: "localhost",
		Tunnels: []TunnelSpec{{Name: "web", Type: "tcp", RemotePort: 9000, LocalAddr: "127.0.0.1:3000"}},
	})
	ctx, cancel2 := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel2()
	go func() { _ = c.Run(ctx) }()

	if !waitTrue(func() bool { return c.Registered() }, 2*time.Second) {
		t.Fatal("client never registered a tunnel")
	}
}

func TestClientReconnectsAfterServerRestart(t *testing.T) {
	defer testutil.AssertNoGoroutineLeak(t)()
	dir := t.TempDir()
	s, cancel := startServer(t, dir, "secret")
	caPEM, _ := os.ReadFile(filepath.Join(dir, "dev-ca.pem"))
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(caPEM)
	addr := s.Addr()

	c := New(Options{Server: addr, Token: "secret", RootCAs: pool, ServerName: "localhost",
		Tunnels: []TunnelSpec{{Type: "tcp", RemotePort: 9000, LocalAddr: "127.0.0.1:3000"}}})
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go func() { _ = c.Run(ctx) }()
	if !waitTrue(c.Registered, 2*time.Second) {
		t.Fatal("initial register failed")
	}
	cancel() // kill server
	s.Wait()
	c.resetRegisteredForTest()
	// restart on the SAME addr
	s2, err := server.New(server.Options{Listen: addr, TLSCert: filepath.Join(dir, "dev-server.pem"),
		TLSKey: filepath.Join(dir, "dev-server-key.pem"), Token: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer func() { cancel2(); s2.Wait() }()
	go func() { _ = s2.Serve(ctx2) }()
	if !waitTrue(c.Registered, 5*time.Second) {
		t.Fatal("client did not reconnect within 5s")
	}
}

func waitTrue(f func() bool, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if f() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func TestClientBridgesDataEndToEnd(t *testing.T) {
	defer testutil.AssertNoGoroutineLeak(t)()
	// local echo service
	ls, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ls.Close()
	go func() {
		for {
			c, e := ls.Accept()
			if e != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				b := make([]byte, 5)
				if _, e := io.ReadFull(c, b); e == nil {
					_, _ = c.Write(append([]byte("R:"), b...))
				}
			}(c)
		}
	}()
	_, lport, _ := net.SplitHostPort(ls.Addr().String())

	dir := t.TempDir()
	s, cancel := startServer(t, dir, "secret") // Phase-2 helper; New() now defaults PublicBind/ports
	defer func() { cancel(); s.Wait() }()
	caPEM, _ := os.ReadFile(filepath.Join(dir, "dev-ca.pem"))
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(caPEM)

	c := New(Options{
		Server: s.Addr(), Token: "secret", RootCAs: pool, ServerName: "localhost",
		Tunnels: []TunnelSpec{{Name: "echo", Type: "tcp", RemotePort: 0, LocalAddr: "127.0.0.1:" + lport}},
	})
	ctx, c2 := context.WithCancel(context.Background())
	defer c2()
	go func() { _ = c.Run(ctx) }()
	if !waitTrue(c.Registered, 3*time.Second) {
		t.Fatal("client never registered")
	}
	port := c.lastRemotePortForTest()
	if port == 0 {
		t.Fatal("no remote port assigned")
	}
	vc, err := net.DialTimeout("tcp", "127.0.0.1:"+itoa(port), 3*time.Second)
	if err != nil {
		t.Fatalf("visitor dial :%d: %v", port, err)
	}
	defer vc.Close()
	_, _ = vc.Write([]byte("PINGS"))
	vc.SetReadDeadline(time.Now().Add(3 * time.Second))
	got := make([]byte, 7)
	if _, err := io.ReadFull(vc, got); err != nil || string(got) != "R:PINGS" {
		t.Fatalf("got %q err=%v", got, err)
	}
}

func itoa(i int) string { return strconv.Itoa(i) }
