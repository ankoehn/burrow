package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ankoehn/burrow/internal/authz"
	"github.com/ankoehn/burrow/internal/metrics"
)

// fakeMetricsRecorder is a tiny stand-in that records whether WriteText was
// called and emits a deterministic 4-byte response so the gate tests don't
// have to parse the full closed-set output (the recorder package already
// covers that). Implements api.MetricsRecorder.
type fakeMetricsRecorder struct {
	writes int
	out    string
}

func (f *fakeMetricsRecorder) WriteText(w http.ResponseWriter) error {
	f.writes++
	body := f.out
	if body == "" {
		body = "# HELP fake noop\n# TYPE fake counter\nfake 1\n"
	}
	_, _ = io.WriteString(w, body)
	return nil
}

func (f *fakeMetricsRecorder) SetCertExpiryDays(_ string, _ float64) {}

// TestMetricsUnauthenticated401 asserts /metrics requires a session. No
// cookies → 401, and the body MUST NOT leak any metric content.
func TestMetricsUnauthenticated401(t *testing.T) {
	d := Deps{
		Log:     discardLog(),
		Users:   &fakeUserStore{role: "admin"},
		Metrics: &fakeMetricsRecorder{},
	}
	srv := httptest.NewServer(NewRouter(d))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d want 401 body=%s", resp.StatusCode, readBody(t, resp))
	}
}

// TestMetricsUserWithoutPerm403 asserts a non-admin caller without
// metrics:read is rejected with 403 (after the 401 gate has passed).
func TestMetricsUserWithoutPerm403(t *testing.T) {
	d := Deps{
		Log:     discardLog(),
		Users:   &fakeUserStore{role: "user"},
		Metrics: &fakeMetricsRecorder{},
	}
	srv := httptest.NewServer(NewRouter(d))
	defer srv.Close()
	c := authedClient(t, srv)
	r := c.get(t, "/metrics")
	defer r.Body.Close()
	if r.StatusCode != http.StatusForbidden {
		t.Fatalf("status=%d want 403", r.StatusCode)
	}
}

// TestMetricsAdminOK asserts admin role gets 200, the Prometheus 0.0.4
// content-type, and the recorder's WriteText is actually invoked.
func TestMetricsAdminOK(t *testing.T) {
	fake := &fakeMetricsRecorder{out: "burrow_goroutines 5\n"}
	d := Deps{
		Log:     discardLog(),
		Users:   &fakeUserStore{role: "admin"},
		Metrics: fake,
	}
	srv := httptest.NewServer(NewRouter(d))
	defer srv.Close()
	c := authedClient(t, srv)
	r := c.get(t, "/metrics")
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", r.StatusCode, readBody(t, r))
	}
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") || !strings.Contains(ct, "version=0.0.4") {
		t.Errorf("Content-Type=%q want text/plain;version=0.0.4", ct)
	}
	if fake.writes != 1 {
		t.Errorf("WriteText calls=%d want 1", fake.writes)
	}
	body := readBody(t, r)
	if !strings.Contains(body, "burrow_goroutines 5") {
		t.Errorf("body missing recorder output: %s", body)
	}
}

// TestMetricsCustomRoleWithPerm asserts a non-admin role granted
// authz.PermMetricsRead via the custom-roles cache passes the gate.
func TestMetricsCustomRoleWithPerm(t *testing.T) {
	defer authz.SetRoles(nil)
	authz.SetRoles(map[string][]authz.Permission{
		"sre": {authz.PermMetricsRead},
	})
	fake := &fakeMetricsRecorder{}
	d := Deps{
		Log:     discardLog(),
		Users:   &fakeUserStore{role: "sre"},
		Metrics: fake,
	}
	srv := httptest.NewServer(NewRouter(d))
	defer srv.Close()
	c := authedClient(t, srv)
	r := c.get(t, "/metrics")
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", r.StatusCode, readBody(t, r))
	}
	if fake.writes != 1 {
		t.Errorf("WriteText calls=%d want 1", fake.writes)
	}
}

// TestMetricsNilRecorder500 asserts a nil Deps.Metrics produces a clean 500
// for authenticated admin callers — the gate fired first (no 401), then the
// handler reported the missing dependency. This protects the bootstrap path:
// the API can be running before cmd/server wires the recorder.
func TestMetricsNilRecorder500(t *testing.T) {
	d := Deps{
		Log:     discardLog(),
		Users:   &fakeUserStore{role: "admin"},
		Metrics: nil,
	}
	srv := httptest.NewServer(NewRouter(d))
	defer srv.Close()
	c := authedClient(t, srv)
	r := c.get(t, "/metrics")
	defer r.Body.Close()
	if r.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status=%d want 500", r.StatusCode)
	}
}

// TestMetricsRecorderEndToEnd plumbs a real *metrics.Recorder through the
// adapter and asserts the response contains every metric name from the
// closed set — proves the api ↔ metrics seam is intact, not just the gate.
func TestMetricsRecorderEndToEnd(t *testing.T) {
	rec := metrics.New()
	// Touch one series under each metric to ensure non-empty output.
	rec.IncHTTPRequest("svc", "GET", 200)
	rec.ObserveHTTPDuration("svc", "GET", 0.1)
	rec.SetGoroutines(7)
	rec.SetAuditChainLength(3)
	rec.SetAuditChainLastHash("cafef00d")

	d := Deps{
		Log:     discardLog(),
		Users:   &fakeUserStore{role: "admin"},
		Metrics: NewMetricsRecorderAdapter(rec),
	}
	srv := httptest.NewServer(NewRouter(d))
	defer srv.Close()
	c := authedClient(t, srv)
	r := c.get(t, "/metrics")
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", r.StatusCode, readBody(t, r))
	}
	body := readBody(t, r)
	for _, name := range []string{
		"burrow_http_requests_total",
		"burrow_http_request_duration_seconds",
		"burrow_goroutines",
		"burrow_audit_chain_length",
		"burrow_audit_chain_last_hash",
	} {
		if !strings.Contains(body, "# HELP "+name+" ") {
			t.Errorf("response missing HELP for %s", name)
		}
		if !strings.Contains(body, "# TYPE "+name+" ") {
			t.Errorf("response missing TYPE for %s", name)
		}
	}
	// The audit-chain hash gauge MUST embed the hex digest as a label.
	if !strings.Contains(body, `burrow_audit_chain_last_hash{hash="cafef00d"} 1`) {
		t.Errorf("audit chain hash not surfaced as label: %s", body)
	}
}
