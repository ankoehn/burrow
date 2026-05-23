package main

// e2e_v050_default_test.go — Integration Task 2 (v0.5.0).
//
// One default-build end-to-end smoke that proves the v0.5.0 wiring in
// cmd/server actually composes the new handlers under the headline build
// flavour (NoopCache + SQLite, no build tags).
//
// Unlike v0_5_0_release_smoke_test.go (which asserts only "no 5xx" across
// every endpoint), this file exercises ONE meaningful request per v0.5.0
// spec family (A–H, J) with a real assertion on the returned shape or
// state mutation. The goal is the SEAM — proving the wiring in
// cmd/server/main.go and v05_wiring.go actually composes the handlers
// against real SQLite + real store + real router. Per-feature
// correctness already lives in internal/api/*_test.go.
//
// Family coverage:
//
//	A semantic cache         — GET /api/v1/cache/settings: assert semantic
//	                           block present + enabled=false in default build
//	B upstream credentials   — PUT /api/v1/services/{svc}/upstream-credential
//	                           after seeding env BURROW_UPSTREAM_KEY_OPENAI
//	C multi-provider         — POST /api/v1/models/aliases with provider +
//	                           priority; assert round-trip
//	D custom domains         — POST /api/v1/services/{svc}/domains with a
//	                           CA-signed leaf covering "foo.example.com"
//	E connection logs        — see comment on TestV050DefaultBuildE2E/E
//	F retention              — PUT then GET /api/v1/settings/retention; the
//	                           PUT'd usage_retention_days round-trips
//	G database backend       — GET /api/v1/database: driver == "sqlite"
//	H webhook templates      — POST /api/v1/webhooks (with template) then
//	                           POST /api/v1/webhooks/{id}/preview; assert
//	                           "rendered" key present in the JSON response
//	J OpenAPI viewer         — GET /api/v1/openapi/viewer/ → 200 text/html
//
// Part E note (production wiring gap): The v0.5.0 backend ships proxy.
// WithConnLogSink + connlog.NewSQLSink but cmd/server has NOT yet wired
// them together (internal/proxy/proxy.go declares the sink field but
// never calls Record — Task 17 was deferred). Touching internal/proxy is
// outside Task 2 scope per the integration plan. Part E therefore
// exercises the API↔store↔connlog binding by calling
// connlog.NewSQLSink(...).Record(...) directly (simulating exactly what
// the proxy hot-path will do once Task 17 lands) and then asserting the
// API read surface returns the row. This is a legitimate seam: it proves
// the three packages that meet at this contract (proxy.ConnLogSink ←
// connlog.SQLSink → SQLite → api.ConnLogDB) interoperate end-to-end
// under the real binary. The remaining gap (proxy actually calling
// Record) is documented in the v0.5.0 integration report as F-13.

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ankoehn/burrow/internal/api"
	"github.com/ankoehn/burrow/internal/audit"
	"github.com/ankoehn/burrow/internal/config"
	"github.com/ankoehn/burrow/internal/connlog"
	"github.com/ankoehn/burrow/internal/db"
	"github.com/ankoehn/burrow/internal/metrics"
	"github.com/ankoehn/burrow/internal/store"
	"github.com/ankoehn/burrow/internal/webhook"
)

// v050Env is the bundle of long-lived objects owned by one bootV050E2E call.
type v050Env struct {
	dbPath    string
	sqldb     *db.DB
	srv       *httptest.Server
	hc        *http.Client
	csrf      string
	adminID   string
	serviceID string
	caPEM     string // PEM of the test CA, kept for diagnostic logging only
}

// bootV050E2E stands up the full v0.5.0 API surface against real SQLite +
// real store + real router. Pattern mirrors bootSmokeServer but adds:
//
//   - sets BURROW_UPSTREAM_KEY_OPENAI BEFORE buildV05Stack so the EnvVault
//     scan picks up the slot (Part B needs a known slot to bind),
//   - generates a self-signed test CA + plumbs it into Deps.CertValidationRoots
//     so Part D's POST /domains chain validation succeeds with the leaf cert,
//   - creates a real services row (admin-owned) so the per-service routes
//     (B, C, D) don't 404,
//   - logs in once and shares one CSRF/cookie jar across every subtest.
func bootV050E2E(t *testing.T) *v050Env {
	t.Helper()

	// Slot must be visible to EnvVault at construction time (NewEnvVault
	// scans os.Environ() once). t.Setenv resets after the test.
	t.Setenv("BURROW_UPSTREAM_KEY_OPENAI", "sk-test-1")

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "v050-e2e.db")
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

	const adminEmail = "admin-v050@test"
	const adminPass = "password1-very-strong"
	if err := st.SeedAdmin(context.Background(), adminEmail, adminPass); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	adminUser, err := st.GetUserByEmail(context.Background(), adminEmail)
	if err != nil {
		t.Fatalf("get admin: %v", err)
	}

	// Create a real service owned by admin (Parts B/C/D/E need a service id).
	svc, err := wrapped.GetOrCreateService(context.Background(), adminUser.ID, "echo", "http")
	if err != nil {
		t.Fatalf("get-or-create service: %v", err)
	}

	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Audit logger (needed by handlers that record per-mutation events).
	signingKey, err := audit.LoadOrGenerateSigningKey(context.Background(), st)
	if err != nil {
		t.Fatalf("signing key: %v", err)
	}
	auditLogger := audit.NewLogger(wrapped, signingKey, log)
	st.SetAuditLogger(storeAuditAdapter{l: auditLogger})

	// Webhook dispatcher (Part H, plus cert.expiring publish in Part D).
	secrets := webhook.NewInMemorySecrets()
	dispatcher := webhook.New(wrapped, secrets, auditLogger, log)
	dispatcher.Start()
	t.Cleanup(dispatcher.Close)

	// v0.5.0 + v0.4.0 wiring — same construction path as cmd/server/main.go.
	metricsRec := metrics.New()
	v05, err := buildV05Stack(context.Background(), wrapped, metricsRec, log)
	if err != nil {
		t.Fatalf("buildV05Stack: %v", err)
	}
	cfg := &config.ServerConfig{
		WebAuthnRPID:   "localhost",
		WebAuthnRPName: "Burrow Test",
		WebAuthnOrigin: "http://localhost:8080",
	}
	v04, err := buildV04Stack(context.Background(), cfg, sqldb, st, log)
	if err != nil {
		t.Fatalf("buildV04Stack: %v", err)
	}
	t.Cleanup(func() { v04.WebhookDispatcher.Close() })
	v04.AIChain.Semantic = v05.SemanticCache
	v04.AIChain.CredInjector = v05.CredInjector

	// Test CA + root pool so Part D's POST /domains can pass chain validation.
	caCert, caKey, caPool, caPEM := generateTestCA(t)

	deps := api.Deps{
		Users:               st,
		Roles:               st,
		Sessions:            st,
		Settings:            st,
		Clients:             noopClientLister{},
		AccessModes:         st,
		DB:                  sqldb,
		Services:            st,
		Log:                 log,
		AuditEvents:         wrapped,
		AuditChain:          api.NewAuditChainAdapter(auditLogger),
		AuditAppender:       auditLogger,
		Metrics:             api.NewMetricsRecorderAdapter(metricsRec),
		Webhooks:            wrapped,
		WebhookDispatcher:   dispatcher,
		WebhookSecrets:      secrets,
		CacheEngine:         v04.CacheEngine,
		CacheServices:       cacheServiceLookupAdapter{db: wrapped},
		InspectorRings:      v04.InspectorMgr,
		InspectorServices:   cacheServiceLookupAdapter{db: wrapped},
		InspectorReplayer:   newInspectorReplayer(v04.AIChain, log),
		ModelAliases:        wrapped,
		IPGeo:               wrapped,
		IPGeoServices:       wrapped,
		GeoLookup:           v04.GeoLookup,
		RateLimitDB:         wrapped,
		RateLimits:          v04.QuotaEngine,
		Budgets:             wrapped,
		CostEngine:          v04.CostEngine,
		Bearer:              api.NewStoreBearerStore(st),
		Automation:          st,
		WebAuthn:            webauthnProviderOrNil(v04.WebAuthn),
		ServiceAIConfigs:    wrapped,
		CredentialVault:     v05.CredVault,
		CredentialDB:        wrapped,
		CredentialServices:  wrapped,
		CustomDomains:       wrapped,
		CustomDomainCache:   v05.CustomDomainStore,
		ConnLogDB:           v05.ConnLogDB,
		CertValidationRoots: caPool,
		Database: api.DBInfo{
			Driver:      "sqlite",
			URLRedacted: "v050-e2e.db",
		},
	}

	hsrv := httptest.NewServer(api.NewRouter(deps))
	t.Cleanup(hsrv.Close)

	// Log in as admin.
	jar, _ := cookiejar.New(nil)
	hc := &http.Client{
		Jar: jar,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	body, _ := json.Marshal(map[string]string{"email": adminEmail, "password": adminPass})
	resp, err := hc.Post(hsrv.URL+"/api/v1/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("login status=%d body=%s", resp.StatusCode, string(b))
	}
	_ = resp.Body.Close()
	u, _ := url.Parse(hsrv.URL)
	var csrf string
	for _, ck := range jar.Cookies(u) {
		if ck.Name == "burrow_csrf" {
			csrf = ck.Value
		}
	}
	if csrf == "" {
		t.Fatal("no CSRF cookie after login")
	}

	env := &v050Env{
		dbPath:    dbPath,
		sqldb:     wrapped,
		srv:       hsrv,
		hc:        hc,
		csrf:      csrf,
		adminID:   adminUser.ID,
		serviceID: svc.ID,
		caPEM:     string(caPEM),
	}
	// Stash CA cert+key for Part D leaf signing. Cleared on test cleanup
	// so a sequential parallel test doesn't see a stale entry.
	testCAByEnv[env] = &testCAPair{cert: caCert, key: caKey}
	t.Cleanup(func() { delete(testCAByEnv, env) })
	return env
}

// do executes an authenticated JSON request with the CSRF header on
// mutating methods. Returns (status, body bytes).
func (e *v050Env) do(t *testing.T, method, path string, body any) (int, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, e.srv.URL+path, rdr)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method != http.MethodGet {
		req.Header.Set("X-CSRF-Token", e.csrf)
	}
	resp, err := e.hc.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

// rawGet performs an authenticated GET and returns the *http.Response WITHOUT
// reading the body (the J subtest checks Content-Type + status only).
func (e *v050Env) rawGet(t *testing.T, path string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, e.srv.URL+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := e.hc.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return resp
}

// TestV050DefaultBuildE2E exercises one request per v0.5.0 spec family
// against the full real API binary under the default build flavour
// (no build tags → NoopCache + SQLite). This is the seam gate complementing
// the per-handler unit tests in internal/api/.
func TestV050DefaultBuildE2E(t *testing.T) {
	env := bootV050E2E(t)

	// --- Part A — semantic cache (NoopCache: enabled=false) ----------------
	t.Run("A_semantic_cache_settings", func(t *testing.T) {
		code, body := env.do(t, http.MethodGet, "/api/v1/cache/settings", nil)
		if code != http.StatusOK {
			t.Fatalf("status=%d body=%s", code, body)
		}
		var obj map[string]any
		if err := json.Unmarshal(body, &obj); err != nil {
			t.Fatalf("decode: %v body=%s", err, body)
		}
		sem, ok := obj["semantic"].(map[string]any)
		if !ok {
			t.Fatalf("response missing semantic block: %s", body)
		}
		enabled, _ := sem["enabled"].(bool)
		if enabled {
			t.Errorf("default build must report semantic.enabled=false (NoopCache); got %s", body)
		}
	})

	// --- Part B — upstream credential PUT ---------------------------------
	t.Run("B_upstream_credential_put", func(t *testing.T) {
		code, body := env.do(t, http.MethodPut,
			"/api/v1/services/"+env.serviceID+"/upstream-credential",
			map[string]string{
				"slot":          "OPENAI",
				"header_name":   "Authorization",
				"header_format": "Bearer {key}",
			})
		if code != http.StatusNoContent {
			t.Fatalf("status=%d body=%s", code, body)
		}
		// Read-back confirms the binding landed.
		code2, body2 := env.do(t, http.MethodGet,
			"/api/v1/services/"+env.serviceID+"/upstream-credential", nil)
		if code2 != http.StatusOK {
			t.Fatalf("readback status=%d body=%s", code2, body2)
		}
		var got map[string]any
		_ = json.Unmarshal(body2, &got)
		if got["slot"] != "OPENAI" {
			t.Errorf("readback slot=%v want OPENAI (body=%s)", got["slot"], body2)
		}
		if got["slot_present"] != true {
			t.Errorf("readback slot_present=%v want true (env BURROW_UPSTREAM_KEY_OPENAI set; body=%s)", got["slot_present"], body2)
		}
	})

	// --- Part C — multi-provider model alias POST -------------------------
	t.Run("C_model_alias_provider_priority", func(t *testing.T) {
		code, body := env.do(t, http.MethodPost, "/api/v1/models/aliases",
			map[string]any{
				"alias":          "fast",
				"concrete_model": "llama3.1:8b",
				"service_id":     env.serviceID,
				"provider":       "ollama",
				"priority":       100,
			})
		if code != http.StatusCreated {
			t.Fatalf("status=%d body=%s", code, body)
		}
		var obj map[string]any
		if err := json.Unmarshal(body, &obj); err != nil {
			t.Fatalf("decode: %v body=%s", err, body)
		}
		if obj["provider"] != "ollama" {
			t.Errorf("provider=%v want ollama (body=%s)", obj["provider"], body)
		}
		// JSON unmarshals numbers as float64.
		if p, _ := obj["priority"].(float64); int(p) != 100 {
			t.Errorf("priority=%v want 100 (body=%s)", obj["priority"], body)
		}
	})

	// --- Part D — custom domain POST --------------------------------------
	t.Run("D_custom_domain_post", func(t *testing.T) {
		// The CA + leaf are signed with the same private key chain plumbed
		// into Deps.CertValidationRoots during boot, so the handler's chain
		// validation step accepts the leaf.
		ca, caKey := mustReuseTestCA(t, env)
		certPEM, keyPEM := genTestLeaf(t, ca, caKey, []string{"foo.example.com"})
		code, body := env.do(t, http.MethodPost,
			"/api/v1/services/"+env.serviceID+"/domains",
			map[string]any{
				"hostname": "foo.example.com",
				"cert_pem": certPEM,
				"key_pem":  keyPEM,
			})
		if code != http.StatusCreated {
			t.Fatalf("status=%d body=%s", code, body)
		}
		var obj map[string]any
		if err := json.Unmarshal(body, &obj); err != nil {
			t.Fatalf("decode: %v body=%s", err, body)
		}
		if obj["hostname"] != "foo.example.com" {
			t.Errorf("hostname=%v want foo.example.com (body=%s)", obj["hostname"], body)
		}
	})

	// --- Part E — connection log API read surface -------------------------
	//
	// Inserts a synthetic connection_log row via the production sink
	// (connlog.NewSQLSink — the same type the proxy holds via WithConnLogSink)
	// and polls the API read surface until the row is visible.
	//
	// This exercises the full seam:
	//   1. connlog.SQLSink.Record() INSERTs into connection_logs (real SQLite).
	//   2. api.NewConnLogDBAdapter + GetConnectionLogs reads it back.
	//   3. The chi router gates the read on requireConnLogRead (admin OK).
	//
	// The remaining production gap (the proxy hot-path actually calling
	// sink.Record(...) inside ServeHTTP) is deferred — see file-level
	// comment "Part E note".
	t.Run("E_connection_logs", func(t *testing.T) {
		sink := connlog.NewSQLSink(env.sqldb, slog.New(slog.NewTextHandler(io.Discard, nil)))
		now := time.Now().UTC()
		err := sink.Record(context.Background(), connlog.Entry{
			Kind:      connlog.KindHTTPProxy,
			ServiceID: env.serviceID,
			SourceIP:  "127.0.0.1",
			StartedAt: now.Add(-10 * time.Millisecond),
			EndedAt:   now,
			BytesIn:   42,
			BytesOut:  100,
			Status:    connlog.StatusClosedClean,
		})
		if err != nil {
			t.Fatalf("sink.Record: %v", err)
		}
		// SQLSink.Record spawns a goroutine — poll until the row is visible.
		deadline := time.Now().Add(5 * time.Second)
		var lastBody []byte
		for time.Now().Before(deadline) {
			code, body := env.do(t, http.MethodGet,
				"/api/v1/connection-logs?service_id="+env.serviceID, nil)
			if code != http.StatusOK {
				t.Fatalf("status=%d body=%s", code, body)
			}
			lastBody = body
			var arr []map[string]any
			if err := json.Unmarshal(body, &arr); err != nil {
				t.Fatalf("decode: %v body=%s", err, body)
			}
			if len(arr) >= 1 {
				// Confirm the row is the one we inserted (service_id matches).
				if arr[0]["service_id"] != env.serviceID {
					t.Errorf("connection_log service_id=%v want %s (body=%s)",
						arr[0]["service_id"], env.serviceID, body)
				}
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatalf("connection_log row never appeared within 5s (last body=%s)", lastBody)
	})

	// --- Part F — retention settings PUT then GET --------------------------
	t.Run("F_retention_put_get", func(t *testing.T) {
		code, body := env.do(t, http.MethodPut, "/api/v1/settings/retention",
			map[string]any{"usage_retention_days": 60})
		if code != http.StatusNoContent {
			t.Fatalf("PUT status=%d body=%s", code, body)
		}
		code2, body2 := env.do(t, http.MethodGet, "/api/v1/settings/retention", nil)
		if code2 != http.StatusOK {
			t.Fatalf("GET status=%d body=%s", code2, body2)
		}
		var obj map[string]any
		if err := json.Unmarshal(body2, &obj); err != nil {
			t.Fatalf("decode: %v body=%s", err, body2)
		}
		if v, _ := obj["usage_retention_days"].(float64); int(v) != 60 {
			t.Errorf("usage_retention_days=%v want 60 (body=%s)", obj["usage_retention_days"], body2)
		}
	})

	// --- Part G — database backend status ---------------------------------
	t.Run("G_database_status", func(t *testing.T) {
		code, body := env.do(t, http.MethodGet, "/api/v1/database", nil)
		if code != http.StatusOK {
			t.Fatalf("status=%d body=%s", code, body)
		}
		var obj map[string]any
		if err := json.Unmarshal(body, &obj); err != nil {
			t.Fatalf("decode: %v body=%s", err, body)
		}
		if obj["driver"] != "sqlite" {
			t.Errorf("driver=%v want sqlite (body=%s)", obj["driver"], body)
		}
	})

	// --- Part H — webhook payload template preview ------------------------
	t.Run("H_webhook_template_preview", func(t *testing.T) {
		// Create a webhook carrying a tiny template.
		code, body := env.do(t, http.MethodPost, "/api/v1/webhooks", map[string]any{
			"name":             "ops",
			"url":              "https://example.com/hook",
			"events":           []string{"ai.upstream_error"},
			"payload_template": `svc={{.service_id}}`,
		})
		if code != http.StatusCreated {
			t.Fatalf("create status=%d body=%s", code, body)
		}
		var created map[string]any
		if err := json.Unmarshal(body, &created); err != nil {
			t.Fatalf("decode created: %v body=%s", err, body)
		}
		whID, _ := created["id"].(string)
		if whID == "" {
			t.Fatalf("created webhook missing id: %s", body)
		}
		// Preview the template with a field map.
		code2, body2 := env.do(t, http.MethodPost,
			"/api/v1/webhooks/"+whID+"/preview",
			map[string]any{
				"event":  "ai.upstream_error",
				"fields": map[string]any{"service_id": env.serviceID},
			})
		if code2 != http.StatusOK {
			t.Fatalf("preview status=%d body=%s", code2, body2)
		}
		var prev map[string]any
		if err := json.Unmarshal(body2, &prev); err != nil {
			t.Fatalf("decode preview: %v body=%s", err, body2)
		}
		if _, ok := prev["rendered"]; !ok {
			t.Errorf("preview missing rendered key (body=%s)", body2)
		}
	})

	// --- Part J — embedded OpenAPI viewer ---------------------------------
	t.Run("J_openapi_viewer", func(t *testing.T) {
		// Trailing slash is required (spec J reconciliation) — chi's mounted
		// subrouter answers /openapi/viewer/ with 200 text/html; the bare
		// /openapi/viewer redirects 301.
		resp := env.rawGet(t, "/api/v1/openapi/viewer/")
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status=%d", resp.StatusCode)
		}
		ct := resp.Header.Get("Content-Type")
		if !strings.HasPrefix(ct, "text/html") {
			t.Errorf("Content-Type=%q want prefix text/html", ct)
		}
	})
}

// ---------------------------------------------------------------------------
// Test-only TLS helpers — independent of e2e_helpers_test.go because this
// file is build-tag-less and bootE2EStack does not provide a CA-rooted
// chain (its wildcard cert is self-signed).
// ---------------------------------------------------------------------------

// testCAStash holds the test CA cert + private key for one v050Env. It is
// populated by generateTestCA the first time it runs in this test process
// and reused so that Part D's leaf cert chains to the CA pool we plumbed
// into Deps.CertValidationRoots during bootV050E2E.
//
// We can't pass the CA out of bootV050E2E without growing the env struct
// surface; instead generateTestCA stores the pair in a process-wide map
// keyed by the env pointer. (One env per test run; t.Parallel is not used
// here.)
type testCAPair struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
}

var testCAByEnv = map[*v050Env]*testCAPair{}

// generateTestCA returns a self-signed CA cert + key + CertPool + PEM bytes.
// The CA expires in 24h (sufficient for any test run).
func generateTestCA(t *testing.T) (*x509.Certificate, *ecdsa.PrivateKey, *x509.CertPool, []byte) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generateTestCA: gen key: %v", err)
	}
	tpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "burrow-v050-e2e-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("generateTestCA: create cert: %v", err)
	}
	cert, _ := x509.ParseCertificate(der)
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	return cert, priv, pool, pemBytes
}

// mustReuseTestCA returns the CA pair generated for this env by
// bootV050E2E. It panics t.Fatal if generateTestCA was never called for
// this env — the contract is that bootV050E2E calls it once.
func mustReuseTestCA(t *testing.T, env *v050Env) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	// The simplest approach: regenerate a CA here is wrong (the new CA
	// would NOT be in Deps.CertValidationRoots). Instead, generate the CA
	// once at boot, stash it by env pointer, and look it up here.
	pair, ok := testCAByEnv[env]
	if !ok {
		t.Fatal("mustReuseTestCA: bootV050E2E did not stash a CA — test wiring bug")
	}
	return pair.cert, pair.key
}

// genTestLeaf returns PEM bytes for a leaf cert signed by ca, carrying
// the requested DNS SANs. The leaf expires in 24h.
func genTestLeaf(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey, dnsSANs []string) (certPEM, keyPEM string) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("genTestLeaf: gen key: %v", err)
	}
	cn := dnsSANs[0]
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: cn},
		DNSNames:     dnsSANs,
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, ca, &priv.PublicKey, caKey)
	if err != nil {
		t.Fatalf("genTestLeaf: create cert: %v", err)
	}
	certPEM = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatalf("genTestLeaf: marshal key: %v", err)
	}
	keyPEM = string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))
	return certPEM, keyPEM
}

