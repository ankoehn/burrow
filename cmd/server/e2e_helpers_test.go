package main

// e2e_helpers_test.go — shared bootstrap for the v0.3.0 real-stack e2e tests.
//
// One bootE2EStack(t) call brings up:
//   - a temp sqlite DB (with migrations + a seeded admin + a minted client token)
//   - a real *server.Server (control, TLS, dev cert) on a random loopback port
//   - a real *internal/client.Client registering a single "http" tunnel against
//     a local test upstream
//   - a real *internal/proxy ingress on a random loopback port with a freshly
//     generated wildcard TLS cert for *.test.local + the burrow-login gate
//
// The visitor side is a single *http.Client whose Transport always dials the
// proxy port on loopback (so the test never needs a real DNS entry), with
// TLS verification skipped — exactly the "TLS verify disabled, SNI = the
// hostname" model the integration plan specifies.

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ankoehn/burrow/internal/api"
	"github.com/ankoehn/burrow/internal/client"
	"github.com/ankoehn/burrow/internal/db"
	"github.com/ankoehn/burrow/internal/devcert"
	"github.com/ankoehn/burrow/internal/proxy"
	"github.com/ankoehn/burrow/internal/server"
	"github.com/ankoehn/burrow/internal/store"
)

const (
	e2eAuthDomain    = "test.local"
	e2eAdminEmail    = "admin@test.local"
	e2eAdminPassword = "password1-very-strong"
	e2eTunnelName    = "echo"
	e2eClientReady   = 5 * time.Second
)

// e2eStack is the bundle of live components owned by one bootE2EStack call.
type e2eStack struct {
	ctx      context.Context
	cancel   context.CancelFunc
	log      *slog.Logger
	db       *sql.DB
	store    *store.Store
	server   *server.Server
	client   *client.Client
	proxySrv *http.Server
	gate     http.Handler

	// proxyAddr is the "host:port" of the proxy ingress (loopback).
	proxyAddr string
	proxyPort string

	// upstream is the local-side test HTTP server the client tunnel forwards to.
	upstream     *http.Server
	upstreamLn   net.Listener
	upstreamAddr string

	// upstreamHandler is the per-test hook the upstream serves. Reassign before
	// the visitor sends a request. The mux dispatches every request to it.
	upstreamHandler atomic.Value // func(http.ResponseWriter, *http.Request)

	// service identity (resolved at registration time).
	serviceID string
	subdomain string
	hostname  string // "<sub>.test.local"
	userID    string

	cleanupFns []func()
}

// hostWithPort returns "<sub>.test.local:<proxy-port>" — the visitor's apparent
// destination. Combined with proxyDialer, that name is never DNS-resolved.
func (s *e2eStack) hostWithPort() string {
	return s.hostname + ":" + s.proxyPort
}

// setUpstreamHandler swaps the upstream's request handler for the next test.
func (s *e2eStack) setUpstreamHandler(h func(http.ResponseWriter, *http.Request)) {
	s.upstreamHandler.Store(h)
}

// visitorClient builds an *http.Client that always dials the proxy ingress on
// loopback regardless of the URL's hostname (so .test.local needs no DNS),
// trusts no CA (TLS verify disabled — InsecureSkipVerify per the plan), and
// sets SNI from the URL hostname so the wildcard cert is matched correctly.
// Each call returns an independent client (cookie jar is fresh).
func (s *e2eStack) visitorClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	dialAddr := s.proxyAddr // closed over
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "tcp", dialAddr)
		},
		DialTLSContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
			var d net.Dialer
			conn, err := d.DialContext(ctx, "tcp", dialAddr)
			if err != nil {
				return nil, err
			}
			// Use the requested addr's host for SNI so the wildcard cert is matched.
			serverName, _, _ := net.SplitHostPort(addr)
			if serverName == "" {
				serverName = addr
			}
			tlsConn := tls.Client(conn, &tls.Config{
				ServerName:         serverName,
				InsecureSkipVerify: true, //nolint:gosec // e2e test: TLS verify disabled per plan
				MinVersion:         tls.VersionTLS12,
			})
			if err := tlsConn.HandshakeContext(ctx); err != nil {
				_ = conn.Close()
				return nil, err
			}
			return tlsConn, nil
		},
		ForceAttemptHTTP2:     false,
		ResponseHeaderTimeout: 5 * time.Second,
	}
	return &http.Client{
		Transport: tr,
		Jar:       jar,
		// Do not auto-follow redirects: tests need to inspect them.
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: 15 * time.Second,
	}
}

// bootE2EStack stands up the whole real-stack chain. Always uses TLS on the
// proxy (per plan: "dev wildcard cert + auth_domain test.local").
func bootE2EStack(t *testing.T) *e2eStack {
	t.Helper()
	s := &e2eStack{
		log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	// 1. DB + store + admin + client token.
	dir := t.TempDir()
	// Register shutdown AFTER TempDir: t.Cleanup is LIFO, so we want to run
	// the DB close (inside shutdown) BEFORE TempDir's RemoveAll (Windows holds
	// a lock on open files and the unlink fails otherwise).
	t.Cleanup(s.shutdown)
	dbPath := filepath.Join(dir, "e2e.db")
	d, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	s.cleanupFns = append(s.cleanupFns, func() { _ = d.Close() })
	if err := db.Migrate(d); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	s.db = d
	st := store.New(d)
	s.store = st
	if err := st.SeedAdmin(context.Background(), e2eAdminEmail, e2eAdminPassword); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	u, err := st.GetUserByEmail(context.Background(), e2eAdminEmail)
	if err != nil {
		t.Fatalf("admin lookup: %v", err)
	}
	s.userID = u.ID
	tok, err := st.IssueClientToken(context.Background(), u.ID, "e2e")
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}

	// 2. Dev certs for the control listener.
	if err := devcert.Generate(dir, true); err != nil {
		t.Fatalf("devcert: %v", err)
	}
	caPEM, err := os.ReadFile(filepath.Join(dir, "dev-ca.pem"))
	if err != nil {
		t.Fatal(err)
	}
	caPool := x509.NewCertPool()
	caPool.AppendCertsFromPEM(caPEM)

	// 3. Wildcard cert for *.test.local (proxy ingress).
	proxyCertPath, proxyKeyPath := generateWildcardCert(t, dir, "*."+e2eAuthDomain, e2eAuthDomain)

	// 4. Upstream test HTTP server (the "local app" the client tunnel fronts).
	mux := http.NewServeMux()
	s.setUpstreamHandler(func(w http.ResponseWriter, r *http.Request) {
		// Default echo handler; tests override.
		fmt.Fprintf(w, "default upstream got %s %s", r.Method, r.URL.Path)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		h := s.upstreamHandler.Load().(func(http.ResponseWriter, *http.Request))
		h(w, r)
	})
	upstreamLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("upstream listen: %v", err)
	}
	s.upstreamLn = upstreamLn
	s.upstreamAddr = upstreamLn.Addr().String()
	s.upstream = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = s.upstream.Serve(upstreamLn) }()

	// 5. Real control server with v0.3.0 wiring.
	resolver := serviceResolverAdapter{db: db.Wrap(d)}
	srv, err := server.New(server.Options{
		Listen:     "127.0.0.1:0",
		TLSCert:    filepath.Join(dir, "dev-server.pem"),
		TLSKey:     filepath.Join(dir, "dev-server-key.pem"),
		Auth:       st,
		Tunnels:    tunnelStoreAdapter{st},
		Services:   resolver,
		AuthDomain: e2eAuthDomain,
		PublicBind: "127.0.0.1",
		Logger:     s.log,
	})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	s.server = srv

	ctx, cancel := context.WithCancel(context.Background())
	s.ctx = ctx
	s.cancel = cancel

	go func() { _ = srv.Serve(ctx) }()

	// Wait for control listener to bind.
	deadline := time.Now().Add(2 * time.Second)
	for srv.Addr() == "" && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if srv.Addr() == "" {
		t.Fatal("control server never bound")
	}

	// 6. Real proxy ingress with gate, TLS wildcard cert.
	proxyLn, err := tls.Listen("tcp", "127.0.0.1:0", func() *tls.Config {
		cert, err := tls.LoadX509KeyPair(proxyCertPath, proxyKeyPath)
		if err != nil {
			t.Fatalf("load proxy cert: %v", err)
		}
		return &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	}())
	if err != nil {
		t.Fatalf("proxy listen: %v", err)
	}
	s.proxyAddr = proxyLn.Addr().String()
	_, s.proxyPort, _ = net.SplitHostPort(s.proxyAddr)
	checker := proxy.NewAccessCheckerWithSessionsAndLogger(st, st, e2eAuthDomain, s.log)
	gate := proxy.NewGate(st, e2eAuthDomain, true, s.log)
	s.gate = gate
	dialer := proxyDialerAdapter{st: st, srv: srv}
	handler := proxy.New(
		dialer, checker, e2eAuthDomain, s.log,
		proxy.WithGate(gate),
		proxy.WithIngressPort(s.proxyPort),
	)
	s.proxySrv = &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() { _ = s.proxySrv.Serve(proxyLn) }()

	// 7. Real client with one http tunnel.
	c := client.New(client.Options{
		Server:     srv.Addr(),
		Token:      tok,
		RootCAs:    caPool,
		ServerName: "localhost",
		Tunnels: []client.TunnelSpec{
			{Name: e2eTunnelName, Type: "http", LocalAddr: s.upstreamAddr},
		},
		Logger: s.log,
	})
	s.client = c
	go func() { _ = c.Run(ctx) }()

	// 8. Wait for the client to register the http tunnel + service row to land.
	deadline = time.Now().Add(e2eClientReady)
	for time.Now().Before(deadline) {
		if c.Registered() {
			tns := srv.HTTPTunnels()
			if len(tns) > 0 && tns[0].Subdomain != "" {
				s.serviceID = tns[0].ServiceID
				s.subdomain = tns[0].Subdomain
				s.hostname = s.subdomain + "." + e2eAuthDomain
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if s.subdomain == "" {
		t.Fatal("http tunnel never resolved a subdomain")
	}

	return s
}

// shutdown reverses bootE2EStack. Idempotent.
func (s *e2eStack) shutdown() {
	if s.cancel != nil {
		s.cancel()
	}
	if s.proxySrv != nil {
		_ = s.proxySrv.Shutdown(context.Background())
	}
	if s.upstream != nil {
		_ = s.upstream.Shutdown(context.Background())
	}
	if s.upstreamLn != nil {
		_ = s.upstreamLn.Close()
	}
	if s.server != nil {
		s.server.Wait()
	}
	for i := len(s.cleanupFns) - 1; i >= 0; i-- {
		s.cleanupFns[i]()
	}
}

// generateWildcardCert writes a wildcard cert + key to dir, returns their paths.
// SANs include both the wildcard and the bare auth domain (so the gate URL
// https://test.local/__burrow/login resolves to a valid cert too).
func generateWildcardCert(t *testing.T, dir, wildcardName, bareName string) (certPath, keyPath string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(42),
		Subject:      pkix.Name{CommonName: wildcardName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{wildcardName, bareName},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPath = filepath.Join(dir, "proxy-cert.pem")
	keyPath = filepath.Join(dir, "proxy-key.pem")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath
}

// must keeps the small assertion site tidy.
func must(t *testing.T, err error, msg string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", msg, err)
	}
}

// readAllString returns the response body string and closes the body.
func readAllString(t *testing.T, r *http.Response) string {
	t.Helper()
	defer r.Body.Close()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}

// _ keeps the imports honest under future refactors.
var _ = sync.WaitGroup{}

// _ silences "imported and not used" for api when individual test files do
// not import it directly (the helper above doesn't currently call api.X but
// keeps the import marker live for downstream test growth).
var _ = api.JSONHandlerTimeout

// _ silences strings if no downstream test imports it via the helper.
var _ = strings.Contains
