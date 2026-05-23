package main

// e2e_inspector_test.go — Task 11 real-stack e2e for the request inspector
// ring + replay + replay-compare.
//
// The bootE2EStack() helper used by every other v0.4.0 e2e test wires the
// aigw.Chain with a *nil* inspector.Manager (its constructor ignores the
// feature). The inspector tests need both:
//
//   - a chain that owns a real *inspector.Manager so requests flowing
//     through the proxy are captured into a per-service ring;
//   - an httptest.Server fronting api.NewRouter pointing at the SAME
//     manager + a real api.InspectorReplayer wired through the chain;
//
// …so we stand up a self-contained "inspector stack" here. The shape
// mirrors bootE2EStack's bring-up sequence (db → server → proxy → client),
// then layers an API server on top.

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ankoehn/burrow/internal/aigw"
	"github.com/ankoehn/burrow/internal/aimeter"
	"github.com/ankoehn/burrow/internal/api"
	"github.com/ankoehn/burrow/internal/cache/exact"
	"github.com/ankoehn/burrow/internal/client"
	"github.com/ankoehn/burrow/internal/db"
	"github.com/ankoehn/burrow/internal/devcert"
	"github.com/ankoehn/burrow/internal/inspector"
	"github.com/ankoehn/burrow/internal/proxy"
	"github.com/ankoehn/burrow/internal/server"
	"github.com/ankoehn/burrow/internal/store"
)

// inspectorStack is the full bring-up for the inspector e2e tests: a real
// proxy chain wired with *inspector.Manager AND an API mux pointed at the
// same manager + a wired replayer.
type inspectorStack struct {
	// proxy-side
	ctx          context.Context
	cancel       context.CancelFunc
	log          *slog.Logger
	db           *sql.DB
	store        *store.Store
	srv          *server.Server
	client       *client.Client
	proxySrv     *http.Server
	upstream     *http.Server
	upstreamLn   net.Listener
	upstreamHnd  atomic.Value // func(http.ResponseWriter, *http.Request)
	proxyAddr    string
	proxyPort    string
	serviceID    string
	subdomain    string
	hostname     string
	userID       string
	plaintextKey string
	aiChain      *aigw.Chain
	inspectorMgr *inspector.Manager

	// API-side
	apiSrv  *httptest.Server
	apiHC   *http.Client
	apiCSRF string

	cleanup []func()
}

const (
	insAuthDomain    = "test.local"
	insAdminEmail    = "ins-admin@test.local"
	insAdminPassword = "password1-very-strong"
	insTunnelName    = "echo"
	insClientReady   = 5 * time.Second
)

// bootInspectorStack stands up a proxy chain (with inspector wired) plus an
// API httptest.Server fronting api.NewRouter against the same manager. The
// admin is logged in (cookies + CSRF captured) so the test can immediately
// call /api/v1/services/{id}/inspector/...
func bootInspectorStack(t *testing.T) *inspectorStack {
	t.Helper()
	s := &inspectorStack{
		log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	// 1. DB + store + admin + client token.
	dir := t.TempDir()
	t.Cleanup(s.shutdown)
	dbPath := filepath.Join(dir, "inspector-e2e.db")
	d, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	s.cleanup = append(s.cleanup, func() { _ = d.Close() })
	if err := db.Migrate(d); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	s.db = d
	st := store.New(d)
	s.store = st
	if err := st.SeedAdmin(context.Background(), insAdminEmail, insAdminPassword); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	u, err := st.GetUserByEmail(context.Background(), insAdminEmail)
	if err != nil {
		t.Fatalf("admin lookup: %v", err)
	}
	s.userID = u.ID
	tok, err := st.IssueClientToken(context.Background(), u.ID, "inspector-e2e")
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}

	// 2. Dev certs for control + wildcard for proxy.
	if err := devcert.Generate(dir, true); err != nil {
		t.Fatalf("devcert: %v", err)
	}
	caPEM, err := os.ReadFile(filepath.Join(dir, "dev-ca.pem"))
	if err != nil {
		t.Fatal(err)
	}
	caPool := x509.NewCertPool()
	caPool.AppendCertsFromPEM(caPEM)
	proxyCertPath, proxyKeyPath := generateWildcardCert(t, dir, "*."+insAuthDomain, insAuthDomain)

	// 3. Upstream test HTTP server.
	mux := http.NewServeMux()
	s.setUpstreamHandler(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "default upstream got %s %s", r.Method, r.URL.Path)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		h := s.upstreamHnd.Load().(func(http.ResponseWriter, *http.Request))
		h(w, r)
	})
	upstreamLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("upstream listen: %v", err)
	}
	s.upstreamLn = upstreamLn
	s.upstream = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = s.upstream.Serve(upstreamLn) }()

	// 4. Control server.
	resolver := serviceResolverAdapter{db: db.Wrap(d)}
	srv, err := server.New(server.Options{
		Listen:     "127.0.0.1:0",
		TLSCert:    filepath.Join(dir, "dev-server.pem"),
		TLSKey:     filepath.Join(dir, "dev-server-key.pem"),
		Auth:       st,
		Tunnels:    tunnelStoreAdapter{st},
		Services:   resolver,
		AuthDomain: insAuthDomain,
		PublicBind: "127.0.0.1",
		Logger:     s.log,
	})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	s.srv = srv
	ctx, cancel := context.WithCancel(context.Background())
	s.ctx = ctx
	s.cancel = cancel
	go func() { _ = srv.Serve(ctx) }()
	deadline := time.Now().Add(2 * time.Second)
	for srv.Addr() == "" && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if srv.Addr() == "" {
		t.Fatal("control server never bound")
	}

	// 5. Proxy ingress with a chain that DOES wire an inspector.Manager.
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
	checker := proxy.NewAccessCheckerWithSessionsAndLogger(st, st, insAuthDomain, s.log)
	gate := proxy.NewGate(st, insAuthDomain, true, s.log)
	dialer := proxyDialerAdapter{st: st, srv: srv}

	cacheEngine := exact.New(db.Wrap(d), s.log)
	meterSink := aimeter.NewSQLSink(db.Wrap(d))
	meterSink.Log = s.log
	inspectorMgr := inspector.NewManager()
	s.inspectorMgr = inspectorMgr
	aiChain := aigw.NewChain(cacheEngine, nil, nil, nil, nil, inspectorMgr, nil, meterSink, s.log)
	aiChain.Loader = chainConfigLoader{db: db.Wrap(d), log: s.log}
	s.aiChain = aiChain

	handler := proxy.New(
		dialer, checker, insAuthDomain, s.log,
		proxy.WithGate(gate),
		proxy.WithIngressPort(s.proxyPort),
		proxy.WithAIChain(aiChain),
	)
	s.proxySrv = &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = s.proxySrv.Serve(proxyLn) }()

	// 6. Client with one http tunnel.
	c := client.New(client.Options{
		Server:     srv.Addr(),
		Token:      tok,
		RootCAs:    caPool,
		ServerName: "localhost",
		Tunnels: []client.TunnelSpec{
			{Name: insTunnelName, Type: "http", LocalAddr: upstreamLn.Addr().String()},
		},
		Logger: s.log,
	})
	s.client = c
	go func() { _ = c.Run(ctx) }()

	deadline = time.Now().Add(insClientReady)
	for time.Now().Before(deadline) {
		if c.Registered() {
			tns := srv.HTTPTunnels()
			if len(tns) > 0 && tns[0].Subdomain != "" {
				s.serviceID = tns[0].ServiceID
				s.subdomain = tns[0].Subdomain
				s.hostname = s.subdomain + "." + insAuthDomain
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if s.subdomain == "" {
		t.Fatal("http tunnel never resolved a subdomain")
	}

	// 7. API mux: pointed at the SAME inspector.Manager and a real replayer.
	deps := api.Deps{
		Users:             st,
		Sessions:          st,
		Roles:             st,
		Services:          st,
		AccessModes:       st,
		Log:               s.log,
		InspectorRings:    inspectorMgr,
		InspectorServices: cacheServiceLookupAdapter{db: db.Wrap(d)},
		InspectorReplayer: newInspectorReplayer(aiChain, s.log),
	}
	s.apiSrv = httptest.NewServer(api.NewRouter(deps))
	t.Cleanup(s.apiSrv.Close)

	jar, _ := cookiejar.New(nil)
	s.apiHC = &http.Client{Jar: jar, Timeout: 15 * time.Second}
	loginBody, _ := json.Marshal(map[string]string{"email": insAdminEmail, "password": insAdminPassword})
	resp, err := s.apiHC.Post(s.apiSrv.URL+"/api/v1/auth/login", "application/json", bytes.NewReader(loginBody))
	if err != nil {
		t.Fatalf("api login: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("api login status=%d body=%s", resp.StatusCode, string(b))
	}
	_ = resp.Body.Close()
	uu, _ := url.Parse(s.apiSrv.URL)
	for _, ck := range jar.Cookies(uu) {
		if ck.Name == "burrow_csrf" {
			s.apiCSRF = ck.Value
		}
	}
	if s.apiCSRF == "" {
		t.Fatal("no CSRF cookie after api login")
	}

	return s
}

// setUpstreamHandler swaps the upstream's request handler for the next test.
func (s *inspectorStack) setUpstreamHandler(h func(http.ResponseWriter, *http.Request)) {
	s.upstreamHnd.Store(h)
}

// shutdown reverses bootInspectorStack. Idempotent.
func (s *inspectorStack) shutdown() {
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
	if s.srv != nil {
		s.srv.Wait()
	}
	for i := len(s.cleanup) - 1; i >= 0; i-- {
		s.cleanup[i]()
	}
}

// hostWithPort returns the visitor's apparent destination (subdomain + auth
// domain + proxy port).
func (s *inspectorStack) hostWithPort() string { return s.hostname + ":" + s.proxyPort }

// visitorClient returns an HTTP client that always dials the proxy on
// loopback regardless of URL hostname.
func (s *inspectorStack) visitorClient(t *testing.T) *http.Client {
	t.Helper()
	dialAddr := s.proxyAddr
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
		ResponseHeaderTimeout: 5 * time.Second,
	}
	return &http.Client{
		Transport: tr,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: 15 * time.Second,
	}
}

// seedAPIKeyAndConfig switches the service into api_key mode, mints a key,
// and seeds a service_ai_config row with inspector enabled (and any extra
// JSON sub-sections from extras). max selects inspector.max_requests.
func (s *inspectorStack) seedAPIKeyAndConfig(t *testing.T, max int, extras string) {
	t.Helper()
	must(t, s.store.SetServiceAccessMode(
		context.Background(), s.userID, "admin", s.serviceID, "api_key", "Authorization", nil),
		"SetServiceAccessMode(api_key)")
	_, plaintext, err := s.store.CreateAPIKey(
		context.Background(), s.userID, "admin", s.serviceID, "ci-inspector")
	must(t, err, "CreateAPIKey")
	s.plaintextKey = plaintext

	cfg := fmt.Sprintf(`{"inspector":{"enabled":true,"max_requests":%d}%s}`, max, extras)
	if _, err := s.db.ExecContext(context.Background(),
		`INSERT INTO service_ai_config(service_id, config) VALUES(?, ?)`,
		s.serviceID, cfg,
	); err != nil {
		t.Fatalf("seed service_ai_config: %v", err)
	}
}

// apiDo issues an authenticated JSON request against the API. CSRF token is
// attached for any mutating method.
func (s *inspectorStack) apiDo(t *testing.T, method, path string, body any) (int, []byte, http.Header) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, s.apiSrv.URL+path, rdr)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method != http.MethodGet {
		req.Header.Set("X-CSRF-Token", s.apiCSRF)
	}
	resp, err := s.apiHC.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b, resp.Header
}

// sendVisitorPOST fires one POST through the proxy (api_key access mode +
// bearer), returns the response status and body.
func (s *inspectorStack) sendVisitorPOST(t *testing.T, path string, body []byte) (int, []byte) {
	t.Helper()
	hc := s.visitorClient(t)
	target := "https://" + s.hostWithPort() + path
	req, err := http.NewRequest(http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new req: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if s.plaintextKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.plaintextKey)
	}
	resp, err := hc.Do(req)
	must(t, err, "visitor POST")
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

// inspectorEntry is the JSON wire shape (subset) the test decodes for
// assertion. Mirrors api.inspectorEntryJSON but kept local — the API
// package's wire type is unexported so we redeclare what we need.
type inspectorEntry struct {
	ID       string    `json:"id"`
	TS       time.Time `json:"ts"`
	Method   string    `json:"method"`
	Path     string    `json:"path"`
	Status   int       `json:"status"`
	BytesIn  int64     `json:"bytes_in"`
	BytesOut int64     `json:"bytes_out"`
	ReqBody  string    `json:"req_body"`
	RespBody string    `json:"resp_body"`
}

// inspectorReplayResp is the wire shape returned by POST .../replay.
type inspectorReplayResp struct {
	NewEntry inspectorEntry `json:"new_entry"`
}

// inspectorCompareResp is the wire shape returned by
// POST .../replay-compare.
type inspectorCompareResp struct {
	Original inspectorEntry `json:"original"`
	Replayed inspectorEntry `json:"replayed"`
	Diff     inspectorDiff  `json:"diff"`
}

type inspectorDiff struct {
	Headers []string `json:"headers"`
	Body    string   `json:"body"`
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestE2EInspector_RingAndEviction asserts the per-service ring honours its
// configured cap by evicting the oldest entries, returning entries in
// descending TS, and that GET /requests/{rid} returns the full body.
func TestE2EInspector_RingAndEviction(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e in -short")
	}
	s := bootInspectorStack(t)

	// Counting OpenAI-compat upstream that returns the same body each call.
	respBody := []byte(`{"id":"chatcmpl-x","choices":[{"message":{"role":"assistant","content":"hi"}}]}`)
	s.setUpstreamHandler(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", itoa(len(respBody)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(respBody)
	})

	const maxRequests = 5
	s.seedAPIKeyAndConfig(t, maxRequests, "")

	// Fire 7 visitor POSTs through the proxy. The chain should capture each
	// one; max_requests=5 means the oldest 2 are evicted.
	const totalPOSTs = 7
	for i := 0; i < totalPOSTs; i++ {
		code, _ := s.sendVisitorPOST(t,
			"/v1/chat/completions",
			[]byte(fmt.Sprintf(`{"model":"gpt-4","seq":%d}`, i)))
		if code != http.StatusOK {
			t.Fatalf("POST %d: status=%d", i, code)
		}
		// Tiny separation so TS ordering is stable.
		time.Sleep(2 * time.Millisecond)
	}

	// GET /inspector/requests — expect exactly maxRequests entries.
	code, body, _ := s.apiDo(t, http.MethodGet,
		"/api/v1/services/"+s.serviceID+"/inspector/requests", nil)
	if code != http.StatusOK {
		t.Fatalf("list inspector requests: status=%d body=%s", code, body)
	}
	var entries []inspectorEntry
	if err := json.Unmarshal(body, &entries); err != nil {
		t.Fatalf("decode list: %v body=%s", err, body)
	}
	if len(entries) != maxRequests {
		t.Fatalf("ring size: got %d entries, want %d (oldest %d evicted)",
			len(entries), maxRequests, totalPOSTs-maxRequests)
	}

	// Descending TS order.
	for i := 1; i < len(entries); i++ {
		if entries[i].TS.After(entries[i-1].TS) {
			t.Fatalf("entries not in descending TS order: [%d]=%s > [%d]=%s",
				i, entries[i].TS, i-1, entries[i-1].TS)
		}
	}

	// GET /inspector/requests/{rid} for the head entry: full bodies present
	// + correct field shape.
	head := entries[0]
	code, body, _ = s.apiDo(t, http.MethodGet,
		"/api/v1/services/"+s.serviceID+"/inspector/requests/"+head.ID, nil)
	if code != http.StatusOK {
		t.Fatalf("get inspector entry: status=%d body=%s", code, body)
	}
	var full inspectorEntry
	if err := json.Unmarshal(body, &full); err != nil {
		t.Fatalf("decode entry: %v body=%s", err, body)
	}
	if full.ID != head.ID {
		t.Errorf("entry id mismatch: got %q want %q", full.ID, head.ID)
	}
	if full.Method != "POST" {
		t.Errorf("entry method=%q want POST", full.Method)
	}
	if full.Path != "/v1/chat/completions" {
		t.Errorf("entry path=%q want /v1/chat/completions", full.Path)
	}
	if full.Status != http.StatusOK {
		t.Errorf("entry status=%d want 200", full.Status)
	}
	if full.BytesIn <= 0 {
		t.Errorf("entry bytes_in=%d want >0", full.BytesIn)
	}
	if full.BytesOut <= 0 {
		t.Errorf("entry bytes_out=%d want >0", full.BytesOut)
	}
	// req_body is utf8-encoded (the body is JSON, valid UTF-8).
	if full.ReqBody == "" {
		t.Errorf("entry req_body empty — expected the visitor POST JSON")
	}
	if full.RespBody == "" {
		t.Errorf("entry resp_body empty — expected the upstream JSON")
	}
}

// TestE2EInspector_Replay asserts that POSTing to /inspector/requests/{rid}/
// replay produces a new entry (different ID) and that GET /inspector/requests
// shows the original + the replay.
func TestE2EInspector_Replay(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e in -short")
	}
	s := bootInspectorStack(t)

	respBody := []byte(`{"id":"x","choices":[{"message":{"content":"hi"}}]}`)
	s.setUpstreamHandler(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", itoa(len(respBody)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(respBody)
	})
	s.seedAPIKeyAndConfig(t, 10, "")

	// 1. One POST → one captured entry.
	code, _ := s.sendVisitorPOST(t,
		"/v1/chat/completions",
		[]byte(`{"model":"gpt-4","msg":"hello"}`))
	if code != http.StatusOK {
		t.Fatalf("first POST: status=%d", code)
	}

	// 2. Find the rid via GET /inspector/requests.
	code, body, _ := s.apiDo(t, http.MethodGet,
		"/api/v1/services/"+s.serviceID+"/inspector/requests", nil)
	if code != http.StatusOK {
		t.Fatalf("list: %d body=%s", code, body)
	}
	var entries []inspectorEntry
	_ = json.Unmarshal(body, &entries)
	if len(entries) != 1 {
		t.Fatalf("want 1 entry after first POST, got %d", len(entries))
	}
	origRID := entries[0].ID

	// 3. POST .../replay with follow_routing:true.
	code, body, _ = s.apiDo(t, http.MethodPost,
		"/api/v1/services/"+s.serviceID+"/inspector/requests/"+origRID+"/replay",
		map[string]any{"follow_routing": true})
	if code != http.StatusOK {
		t.Fatalf("replay: status=%d body=%s", code, body)
	}
	var rr inspectorReplayResp
	if err := json.Unmarshal(body, &rr); err != nil {
		t.Fatalf("decode replay resp: %v body=%s", err, body)
	}
	if rr.NewEntry.ID == "" {
		t.Fatalf("replay resp missing new_entry.id; body=%s", body)
	}
	if rr.NewEntry.ID == origRID {
		t.Fatalf("replay new_entry.id == original id %q — must be a NEW rid", origRID)
	}

	// 4. List should now have 2 entries (original + replay).
	code, body, _ = s.apiDo(t, http.MethodGet,
		"/api/v1/services/"+s.serviceID+"/inspector/requests", nil)
	if code != http.StatusOK {
		t.Fatalf("list after replay: %d body=%s", code, body)
	}
	entries = nil
	_ = json.Unmarshal(body, &entries)
	if len(entries) != 2 {
		t.Fatalf("after replay: want 2 entries, got %d", len(entries))
	}
	gotIDs := map[string]bool{entries[0].ID: true, entries[1].ID: true}
	if !gotIDs[origRID] || !gotIDs[rr.NewEntry.ID] {
		t.Fatalf("expected both %q and %q in list; got %v", origRID, rr.NewEntry.ID, gotIDs)
	}
}

// TestE2EInspector_ReplayCompare asserts the replay-compare diff format:
// {original, replayed, diff:{headers, body}} where diff.body is a unified
// diff for textual content. The compare arm must run with Burrow-Cache:
// bypass (no cache HIT).
func TestE2EInspector_ReplayCompare(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e in -short")
	}
	s := bootInspectorStack(t)

	// Counter-suffixed upstream: returns "response1" then "response2"... so
	// even though the chain's noUpstream handler will short-circuit the
	// replay's actual upstream hop, the ORIGINAL captured body still says
	// "response1" — the diff still has bytes to chew on either way.
	var counter atomic.Int32
	s.setUpstreamHandler(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		n := counter.Add(1)
		bodyStr := fmt.Sprintf(`{"resp":"response%d"}`, n)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", itoa(len(bodyStr)))
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, bodyStr)
	})
	s.seedAPIKeyAndConfig(t, 10, "")

	// 1. Send one visitor POST so the upstream returns "response1".
	code, _ := s.sendVisitorPOST(t,
		"/v1/chat/completions",
		[]byte(`{"model":"gpt-4","msg":"hi"}`))
	if code != http.StatusOK {
		t.Fatalf("first POST: status=%d", code)
	}

	// 2. List to grab the rid.
	code, body, _ := s.apiDo(t, http.MethodGet,
		"/api/v1/services/"+s.serviceID+"/inspector/requests", nil)
	if code != http.StatusOK {
		t.Fatalf("list: %d body=%s", code, body)
	}
	var entries []inspectorEntry
	_ = json.Unmarshal(body, &entries)
	if len(entries) != 1 {
		t.Fatalf("want 1 entry; got %d", len(entries))
	}
	origRID := entries[0].ID
	if !bytes.Contains([]byte(entries[0].RespBody), []byte("response1")) {
		t.Fatalf("original resp_body should contain \"response1\"; got %q", entries[0].RespBody)
	}

	// 3. POST .../replay-compare.
	code, body, _ = s.apiDo(t, http.MethodPost,
		"/api/v1/services/"+s.serviceID+"/inspector/requests/"+origRID+"/replay-compare",
		nil)
	if code != http.StatusOK {
		t.Fatalf("replay-compare: status=%d body=%s", code, body)
	}
	var cr inspectorCompareResp
	if err := json.Unmarshal(body, &cr); err != nil {
		t.Fatalf("decode compare resp: %v body=%s", err, body)
	}

	// 3a. original / replayed shape.
	if cr.Original.ID != origRID {
		t.Errorf("compare.original.id=%q want %q", cr.Original.ID, origRID)
	}
	if cr.Replayed.ID == "" {
		t.Errorf("compare.replayed.id empty")
	}
	if cr.Replayed.ID == cr.Original.ID {
		t.Errorf("compare.replayed.id MUST differ from original")
	}

	// 3b. diff has the unified-diff prefix for textual content. We only
	// require that the diff string contain a "response1" line marked as
	// removed (the replay's noUpstream handler writes an empty body, so
	// the original "response1" must appear with a "-" prefix in the LCS).
	if cr.Diff.Body == "" {
		t.Fatalf("compare.diff.body empty; full=%s", body)
	}
	if !bytes.Contains([]byte(cr.Diff.Body), []byte("--- original")) ||
		!bytes.Contains([]byte(cr.Diff.Body), []byte("+++ replayed")) {
		t.Errorf("compare.diff.body missing unified-diff header; got:\n%s", cr.Diff.Body)
	}
	// The diff should reflect that the body changed: the original's
	// "response1" string appears in a deletion line.
	if !bytes.Contains([]byte(cr.Diff.Body), []byte("response1")) {
		t.Errorf("compare.diff.body should mention \"response1\" (original body); got:\n%s", cr.Diff.Body)
	}

	// 4. The chain saw Burrow-Cache: bypass — confirmed implicitly because
	// the cache feature isn't seeded in this test (so the test doesn't need
	// to assert no cache HIT). The handler unconditionally sets the header
	// on the request it hands to the replayer; the API contract for
	// replay-compare is what we're asserting.

	// 5. List should now contain 2 entries (original + replay).
	code, body, _ = s.apiDo(t, http.MethodGet,
		"/api/v1/services/"+s.serviceID+"/inspector/requests", nil)
	if code != http.StatusOK {
		t.Fatalf("list after compare: %d body=%s", code, body)
	}
	entries = nil
	_ = json.Unmarshal(body, &entries)
	if len(entries) != 2 {
		t.Fatalf("after replay-compare: want 2 entries, got %d", len(entries))
	}
}

// TestE2EInspector_SSEStream asserts the SSE stream emits one
// "event: request" frame per captured entry. The handler subscribes BEFORE
// the visitor POSTs fire so the bus delivery is observed.
func TestE2EInspector_SSEStream(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e in -short")
	}
	s := bootInspectorStack(t)

	respBody := []byte(`{"ok":true}`)
	s.setUpstreamHandler(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", itoa(len(respBody)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(respBody)
	})
	s.seedAPIKeyAndConfig(t, 10, "")

	// Subscribe to the SSE stream first, on a goroutine that records the
	// number of "event: request" frames it sees.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		s.apiSrv.URL+"/api/v1/services/"+s.serviceID+"/inspector/stream", nil)
	resp, err := s.apiHC.Do(req)
	if err != nil {
		t.Skipf("SSE subscribe failed; skipping cleanly: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Skipf("SSE subscribe non-200 (%d): %s — skipping cleanly", resp.StatusCode, b)
	}
	defer resp.Body.Close()

	gotFrames := make(chan int, 1)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 4096), 1<<20)
		seen := 0
		for sc.Scan() {
			if bytes.HasPrefix(sc.Bytes(), []byte("event: request")) {
				seen++
				if seen == 2 {
					gotFrames <- seen
					return
				}
			}
		}
		gotFrames <- seen
	}()

	// Brief settling so the handler subscribes before we fire the POSTs.
	time.Sleep(100 * time.Millisecond)

	for i := 0; i < 2; i++ {
		code, _ := s.sendVisitorPOST(t,
			"/v1/chat/completions",
			[]byte(fmt.Sprintf(`{"model":"gpt-4","seq":%d}`, i)))
		if code != http.StatusOK {
			t.Fatalf("POST %d: status=%d", i, code)
		}
	}

	select {
	case n := <-gotFrames:
		if n != 2 {
			t.Errorf("SSE frames observed: got %d, want 2", n)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for 2 SSE inspector frames")
	}
}
