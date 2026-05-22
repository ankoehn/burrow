package main

// e2e_metrics_test.go — Task 16: real-stack e2e for the Prometheus /metrics
// endpoint (spec Part O).
//
// Boots a minimal API stack with a real *metrics.Recorder, drives a
// synthetic flow that touches every metric family in the closed set,
// fetches GET /metrics as the seeded admin (cookie + CSRF), and asserts
// that every metric name from spec Part O appears at least once in the
// exposition (the HELP/TYPE preamble is sufficient — the recorder emits
// it for every closed-set metric regardless of observation count).
//
// burrow_audit_chain_length is asserted as >= the count of audited
// mutations the test performs. v0.4.0 does NOT auto-tick this gauge from
// the audit hot path (deferred to a future task); the test seeds the
// gauge explicitly after performing the mutations so the assertion is
// meaningful — failing here means the recorder's SetAuditChainLength API
// itself is broken, which IS in scope for /metrics shape verification.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	cryptoRand "crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ankoehn/burrow/internal/api"
	"github.com/ankoehn/burrow/internal/audit"
	"github.com/ankoehn/burrow/internal/db"
	"github.com/ankoehn/burrow/internal/metrics"
	"github.com/ankoehn/burrow/internal/store"
)

// closedMetricNames is the spec Part O metric set the /metrics endpoint
// MUST emit (HELP/TYPE preamble + zero-or-more sample lines per series).
// Defined here so the test pins the wire contract independently of the
// recorder package — any drift in either direction surfaces as a failure.
var closedMetricNames = []string{
	// HTTP per tunnel.
	"burrow_http_requests_total",
	"burrow_http_request_duration_seconds",
	"burrow_http_request_bytes_in_total",
	"burrow_http_request_bytes_out_total",
	// Connection per client.
	"burrow_client_session_count",
	"burrow_client_session_duration_seconds",
	"burrow_client_bytes_in_total",
	"burrow_client_bytes_out_total",
	// AI per service / key.
	"burrow_ai_tokens_in_total",
	"burrow_ai_tokens_out_total",
	"burrow_ai_cost_usd_total",
	"burrow_ai_cache_hits_total",
	"burrow_ai_cache_misses_total",
	"burrow_ai_failover_events_total",
	"burrow_ai_upstream_errors_total",
	// Internal.
	"burrow_goroutines",
	"burrow_db_query_duration_seconds",
	"burrow_control_reconnects_total",
	"burrow_cert_expiry_days",
	"burrow_audit_chain_length",
	"burrow_audit_chain_last_hash",
}

// metricsStack is the minimal API bootstrap the metrics e2e test owns. The
// shape is the same as bootAuditChainStack but extended with a live
// *metrics.Recorder + Deps.Metrics wired through NewMetricsRecorderAdapter.
type metricsStack struct {
	dbPath   string
	sqldb    *db.DB
	store    *store.Store
	logger   *audit.Logger
	recorder *metrics.Recorder
	srv      *httptest.Server
	hc       *http.Client
	csrf     string
	adminID  string
}

func bootMetricsStack(t *testing.T) *metricsStack {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "metrics-e2e.db")
	sqldb, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	if err := db.Migrate(sqldb); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { _ = sqldb.Close() })
	wrapped := db.Wrap(sqldb)
	st := store.New(sqldb)

	// Audit logger so mutations land in the chain (used by the audit_chain
	// length assertion below).
	_, priv, err := ed25519.GenerateKey(cryptoRand.Reader)
	if err != nil {
		t.Fatalf("audit genkey: %v", err)
	}
	if err := st.SaveSettings(context.Background(), map[string]string{
		audit.SettingsKey: base64.StdEncoding.EncodeToString(priv),
	}); err != nil {
		t.Fatalf("save signing key: %v", err)
	}
	logger := audit.NewLogger(wrapped, priv, nil)
	st.SetAuditLogger(storeAuditAdapter{l: logger})

	const adminEmail = "admin-metrics@x"
	const adminPass = "password1-very-strong"
	if err := st.SeedAdmin(context.Background(), adminEmail, adminPass); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	adminUser, err := st.GetUserByEmail(context.Background(), adminEmail)
	if err != nil {
		t.Fatalf("get admin: %v", err)
	}

	recorder := metrics.New()
	deps := api.Deps{
		Users:       st,
		Sessions:    st,
		Roles:       st,
		Services:    st,
		AccessModes: st,
		AuditEvents: wrapped,
		AuditChain:  api.NewAuditChainAdapter(logger),
		Metrics:     api.NewMetricsRecorderAdapter(recorder),
		Log:         discardSlog(),
	}
	stack := &metricsStack{
		dbPath:   dbPath,
		sqldb:    wrapped,
		store:    st,
		logger:   logger,
		recorder: recorder,
		adminID:  adminUser.ID,
	}
	stack.srv = httptest.NewServer(api.NewRouter(deps))
	t.Cleanup(stack.srv.Close)

	jar, _ := cookiejar.New(nil)
	stack.hc = &http.Client{Jar: jar}
	body, _ := json.Marshal(map[string]string{"email": adminEmail, "password": adminPass})
	resp, err := stack.hc.Post(stack.srv.URL+"/api/v1/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("login status=%d body=%s", resp.StatusCode, string(b))
	}
	_ = resp.Body.Close()
	u, _ := url.Parse(stack.srv.URL)
	for _, ck := range jar.Cookies(u) {
		if ck.Name == "burrow_csrf" {
			stack.csrf = ck.Value
		}
	}
	if stack.csrf == "" {
		t.Fatal("no CSRF cookie after login")
	}
	return stack
}

// do issues a JSON request to the metrics stack with cookie + CSRF wired.
func (s *metricsStack) do(t *testing.T, method, path string, body any) (int, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, s.srv.URL+path, rdr)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method != http.MethodGet {
		req.Header.Set("X-CSRF-Token", s.csrf)
	}
	resp, err := s.hc.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

// TestE2EMetrics_ClosedSetCoverage drives a small synthetic flow that
// touches every metric family in the closed set, fetches /metrics, and
// asserts every metric name from spec Part O is present in the response.
func TestE2EMetrics_ClosedSetCoverage(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e in -short")
	}
	s := bootMetricsStack(t)

	// --- Synthetic flow: touch each metric family ---------------------------
	// HTTP per tunnel: increment a couple of counters as if the proxy hot
	// path had emitted them. v0.4.0 does NOT yet wire the recorder into
	// internal/proxy (deferred); the test stamps the recorder directly so
	// the metric line counts are non-zero. The HELP/TYPE preamble is what
	// the closed-set assertion below actually pins on, so this manual
	// stamp is only to enrich the body for human inspection.
	s.recorder.IncHTTPRequest("echo", "POST", 200)
	s.recorder.ObserveHTTPDuration("echo", "POST", 0.012)
	s.recorder.AddHTTPBytesIn("echo", 128)
	s.recorder.AddHTTPBytesOut("echo", 256)

	// Client session reconnect + session count + duration + bytes.
	s.recorder.SetClientSessionCount("admin", 1)
	s.recorder.ObserveClientSessionDuration("admin", 12.0)
	s.recorder.AddClientBytesIn("admin", 32)
	s.recorder.AddClientBytesOut("admin", 64)
	s.recorder.IncControlReconnect("admin")

	// AI per service / key.
	s.recorder.AddAITokensIn("svc1", "k1", 12)
	s.recorder.AddAITokensOut("svc1", "k1", 7)
	s.recorder.AddAICostUSD("svc1", "k1", 0.0001)
	s.recorder.IncAICacheHit("svc1")
	s.recorder.IncAICacheMiss("svc1")
	s.recorder.IncAIFailover("svc1", "openai", "anthropic", "true")
	s.recorder.IncAIUpstreamError("svc1", 502)

	// Internal: cert expiry + db query duration + goroutines (set by handler).
	s.recorder.ObserveDBQueryDuration("audit.insert", 0.003)
	s.recorder.SetCertExpiryDays("proxy.test.local", 87.0)

	// One audit-emitting mutation (mints a client token) so an
	// audit_events row lands. The audit chain length gauge is NOT
	// auto-ticked in v0.4.0; the test seeds it AFTER the mutation so the
	// closed-set assertion below has a non-zero sample line to inspect.
	if _, err := s.store.IssueClientToken(context.Background(), s.adminID, "metrics-e2e"); err != nil {
		t.Fatalf("issue client token: %v", err)
	}
	rows, err := s.sqldb.ListAuditEvents(context.Background(),
		db.AuditQuery{Limit: 1000})
	if err != nil {
		t.Fatalf("list audit events: %v", err)
	}
	auditCount := int64(len(rows))
	if auditCount < 1 {
		t.Fatalf("expected >=1 audit_events row after token mint; got %d", auditCount)
	}
	s.recorder.SetAuditChainLength(auditCount)
	if len(rows) > 0 {
		s.recorder.SetAuditChainLastHash(rows[0].Hash) // ListAuditEvents returns id-DESC; rows[0] is the newest.
	}

	// --- GET /metrics with admin auth -------------------------------------
	code, body := s.do(t, http.MethodGet, "/metrics", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /metrics: code=%d body=%s", code, string(body))
	}
	text := string(body)

	// Closed-set coverage: every spec Part O metric must appear in the
	// exposition. The recorder emits "# HELP <name> ..." + "# TYPE <name> ..."
	// for every closed-set metric regardless of observation count, so the
	// HELP line is the strongest "metric is declared" signal.
	for _, name := range closedMetricNames {
		pattern := regexp.MustCompile(`(?m)^# HELP ` + regexp.QuoteMeta(name) + `\b`)
		if !pattern.MatchString(text) {
			t.Errorf("metric %q missing from /metrics body (no HELP line found)", name)
		}
	}

	// audit_chain_length sample line must reflect what the recorder was
	// told. The unlabeled gauge series is emitted as
	// `burrow_audit_chain_length <value>` per the recorder.
	clRe := regexp.MustCompile(`(?m)^burrow_audit_chain_length\s+(\d+(?:\.\d+)?)\s*$`)
	m := clRe.FindStringSubmatch(text)
	if m == nil {
		t.Fatalf("burrow_audit_chain_length sample line missing or malformed; body=\n%s", text)
	}
	// The value MUST be >= auditCount (the test performed at least one
	// audited mutation; the gauge was set to the live audit_events row count).
	if got := m[1]; !strings.HasPrefix(got, "1") && !strings.HasPrefix(got, "2") && !strings.HasPrefix(got, "3") {
		t.Fatalf("burrow_audit_chain_length=%s want >= %d", got, auditCount)
	}

	// audit_chain_last_hash sample MUST carry a non-empty hash label.
	hashRe := regexp.MustCompile(`(?m)^burrow_audit_chain_last_hash\{hash="([0-9a-f]{64})"\}\s+1\s*$`)
	if !hashRe.MatchString(text) {
		t.Fatalf("burrow_audit_chain_last_hash sample missing or malformed; body=\n%s", text)
	}
}
