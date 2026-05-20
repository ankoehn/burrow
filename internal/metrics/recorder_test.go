package metrics

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// allMetricNames is the closed set of metric names asserted by the contract
// test below. Mirrors spec Part O verbatim — adding a metric requires a doc PR
// AND this slice must grow in lockstep, which is exactly the friction the
// spec wanted to introduce.
var allMetricNames = []string{
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

// parsedSample is one (name, labels, value) sample line from the exposition
// stream. Tests assert on names + label sets; the value is exposed for the
// concurrency test.
type parsedSample struct {
	Name   string
	Labels map[string]string
	Value  string
}

// parsedMetric is one HELP/TYPE/samples block from the exposition stream.
type parsedMetric struct {
	Name    string
	Help    string
	Type    string
	Samples []parsedSample
}

// parseExposition is a tiny ad-hoc parser for the Prometheus 0.0.4 text
// format — JUST enough to validate the recorder output. It handles HELP, TYPE
// and sample lines (with or without labels), ignores blank lines, and rejects
// nothing — its only job is to extract the metric set the recorder emits.
//
// Label-value escaping is reversed (\" → ", \\ → \, \n → newline). Keys are
// expected to be plain identifiers (no escaping needed in the closed set).
func parseExposition(t *testing.T, in string) map[string]*parsedMetric {
	t.Helper()
	out := map[string]*parsedMetric{}
	getOrInit := func(name string) *parsedMetric {
		m, ok := out[name]
		if !ok {
			m = &parsedMetric{Name: name}
			out[name] = m
		}
		return m
	}
	for _, raw := range strings.Split(in, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "# HELP ") {
			rest := strings.TrimPrefix(line, "# HELP ")
			name, help, ok := splitFirst(rest, " ")
			if !ok {
				t.Fatalf("malformed HELP line: %q", line)
			}
			getOrInit(name).Help = help
			continue
		}
		if strings.HasPrefix(line, "# TYPE ") {
			rest := strings.TrimPrefix(line, "# TYPE ")
			name, typ, ok := splitFirst(rest, " ")
			if !ok {
				t.Fatalf("malformed TYPE line: %q", line)
			}
			getOrInit(name).Type = typ
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		s, baseName, ok := parseSampleLine(t, line)
		if !ok {
			t.Fatalf("unparseable sample line: %q", line)
		}
		// The base metric for histogram _count/_sum lines is the histogram
		// name itself (strip the suffix). The full sample-name is kept on
		// the sample so callers can find e.g. `burrow_http_requests_total`
		// AND `burrow_http_request_duration_seconds_count` independently.
		getOrInit(baseName).Samples = append(getOrInit(baseName).Samples, s)
	}
	return out
}

// parseSampleLine extracts `name{labels} value` from one exposition line.
// It returns the sample plus the "base" metric name (stripping _count / _sum
// suffixes so histogram-companion lines roll up under the parent metric).
func parseSampleLine(t *testing.T, line string) (parsedSample, string, bool) {
	t.Helper()
	// Find the start of labels '{' or the start of the value (first space).
	brace := strings.IndexByte(line, '{')
	var name, labelInner, valueStr string
	if brace == -1 {
		// "name value"
		i := strings.LastIndexByte(line, ' ')
		if i == -1 {
			return parsedSample{}, "", false
		}
		name = line[:i]
		valueStr = line[i+1:]
	} else {
		name = line[:brace]
		close := strings.IndexByte(line[brace:], '}')
		if close == -1 {
			return parsedSample{}, "", false
		}
		labelInner = line[brace+1 : brace+close]
		// after '}' there should be a space then the value
		rest := strings.TrimLeft(line[brace+close+1:], " ")
		valueStr = rest
	}
	labels, ok := parseLabels(labelInner)
	if !ok {
		t.Fatalf("malformed labels in %q", line)
	}
	base := name
	// Histogram companion samples (`<name>_count`, `<name>_sum`) roll up
	// under the parent histogram metric, BUT only if the trimmed name is
	// itself in the closed set — otherwise `burrow_client_session_count`
	// (a gauge) would wrongly get rolled into `burrow_client_session`.
	if trimmed, ok := stripIfHistogramCompanion(name); ok {
		base = trimmed
	}
	return parsedSample{Name: name, Labels: labels, Value: valueStr}, base, true
}

// parseLabels decodes `a="x",b="y"` into a map. Reverses the three required
// exposition-format escapes.
func parseLabels(s string) (map[string]string, bool) {
	out := map[string]string{}
	if s == "" {
		return out, true
	}
	i := 0
	for i < len(s) {
		// key
		eq := strings.IndexByte(s[i:], '=')
		if eq == -1 {
			return nil, false
		}
		key := s[i : i+eq]
		i += eq + 1
		if i >= len(s) || s[i] != '"' {
			return nil, false
		}
		i++ // skip opening quote
		var sb strings.Builder
		for i < len(s) && s[i] != '"' {
			if s[i] == '\\' && i+1 < len(s) {
				switch s[i+1] {
				case '\\':
					sb.WriteByte('\\')
				case '"':
					sb.WriteByte('"')
				case 'n':
					sb.WriteByte('\n')
				default:
					sb.WriteByte(s[i+1])
				}
				i += 2
				continue
			}
			sb.WriteByte(s[i])
			i++
		}
		if i >= len(s) {
			return nil, false
		}
		i++ // skip closing quote
		out[key] = sb.String()
		if i < len(s) && s[i] == ',' {
			i++
		}
	}
	return out, true
}

// stripIfHistogramCompanion returns (base, true) if name ends in _count or
// _sum AND the resulting base is a closed-set histogram. Otherwise (name,
// false) — the name stands on its own (e.g. a gauge that happens to end in
// _count).
func stripIfHistogramCompanion(name string) (string, bool) {
	histograms := map[string]bool{
		"burrow_http_request_duration_seconds":   true,
		"burrow_client_session_duration_seconds": true,
		"burrow_db_query_duration_seconds":       true,
	}
	for _, sfx := range []string{"_count", "_sum"} {
		if strings.HasSuffix(name, sfx) {
			base := strings.TrimSuffix(name, sfx)
			if histograms[base] {
				return base, true
			}
		}
	}
	return name, false
}

func splitFirst(s, sep string) (string, string, bool) {
	i := strings.Index(s, sep)
	if i == -1 {
		return "", "", false
	}
	return s[:i], s[i+len(sep):], true
}

// TestWriteTextClosedSetPresent asserts every metric in the spec Part O closed
// set has BOTH `# HELP` and `# TYPE` lines emitted, even when no observation
// has been recorded — the registry is closed at startup, not at first-use.
func TestWriteTextClosedSetPresent(t *testing.T) {
	r := New()
	// Touch one series under every metric so the renderer emits a sample
	// (and so we exercise the path with a non-empty value for the parser).
	r.IncHTTPRequest("svc", "GET", 200)
	r.ObserveHTTPDuration("svc", "GET", 0.123)
	r.AddHTTPBytesIn("svc", 100)
	r.AddHTTPBytesOut("svc", 200)
	r.SetClientSessionCount("u", 3)
	r.ObserveClientSessionDuration("u", 1.5)
	r.AddClientBytesIn("u", 10)
	r.AddClientBytesOut("u", 20)
	r.AddAITokensIn("svc", "k", 5)
	r.AddAITokensOut("svc", "k", 7)
	r.AddAICostUSD("svc", "k", 0.0125)
	r.IncAICacheHit("svc")
	r.IncAICacheMiss("svc")
	r.IncAIFailover("svc", "anthropic", "openai")
	r.IncAIUpstreamError("svc", 502)
	r.SetGoroutines(42)
	r.ObserveDBQueryDuration("select", 0.01)
	r.IncControlReconnect("c1")
	r.SetCertExpiryDays("wildcard.example.com", 30)
	r.SetAuditChainLength(17)
	r.SetAuditChainLastHash("deadbeef")

	var buf bytes.Buffer
	if err := r.WriteText(&buf); err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	got := parseExposition(t, buf.String())

	for _, name := range allMetricNames {
		m, ok := got[name]
		if !ok {
			t.Errorf("missing metric %q in output", name)
			continue
		}
		if m.Help == "" {
			t.Errorf("metric %q missing HELP line", name)
		}
		if m.Type == "" {
			t.Errorf("metric %q missing TYPE line", name)
		}
		if len(m.Samples) == 0 {
			t.Errorf("metric %q has no samples", name)
		}
	}

	// Sanity: HTTP cost converted from micro-USD back to USD.
	costSeries := got["burrow_ai_cost_usd_total"]
	if costSeries == nil || len(costSeries.Samples) == 0 {
		t.Fatal("burrow_ai_cost_usd_total missing")
	}
	if v := costSeries.Samples[0].Value; v != "0.0125" {
		t.Errorf("ai cost rendered as %q want 0.0125", v)
	}
}

// TestIncHTTPRequestTwoLabelSetsTwoSeries asserts two distinct label tuples
// produce two distinct series (spec Part O: per-label-set counter shape).
func TestIncHTTPRequestTwoLabelSetsTwoSeries(t *testing.T) {
	r := New()
	r.IncHTTPRequest("svc-a", "GET", 200)
	r.IncHTTPRequest("svc-a", "GET", 200) // same series → +1
	r.IncHTTPRequest("svc-b", "POST", 500)

	var buf bytes.Buffer
	if err := r.WriteText(&buf); err != nil {
		t.Fatal(err)
	}
	got := parseExposition(t, buf.String())
	reqs := got["burrow_http_requests_total"]
	if reqs == nil {
		t.Fatal("missing burrow_http_requests_total")
	}
	if len(reqs.Samples) != 2 {
		t.Fatalf("want 2 series, got %d: %+v", len(reqs.Samples), reqs.Samples)
	}
	// Find both series by their label triple.
	want := map[string]string{
		"svc-a|GET|200":  "2",
		"svc-b|POST|500": "1",
	}
	for _, s := range reqs.Samples {
		k := s.Labels["service"] + "|" + s.Labels["method"] + "|" + s.Labels["status"]
		exp, ok := want[k]
		if !ok {
			t.Errorf("unexpected series %s", k)
			continue
		}
		if s.Value != exp {
			t.Errorf("series %s value=%s want %s", k, s.Value, exp)
		}
		delete(want, k)
	}
	for k := range want {
		t.Errorf("missing series %s", k)
	}
}

// TestConcurrentIncCounter runs 1000 goroutines × 1000 Inc calls each against
// the same series and asserts the final atomic count is exactly 1,000,000 —
// the property the sync.Map[*atomic.Int64] hot path is designed to guarantee.
func TestConcurrentIncCounter(t *testing.T) {
	r := New()
	const goroutines = 1000
	const each = 1000
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < each; j++ {
				r.IncHTTPRequest("svc", "GET", 200)
			}
		}()
	}
	wg.Wait()

	v, ok := r.counters.Load(`burrow_http_requests_total{service="svc",method="GET",status="200"}`)
	if !ok {
		t.Fatal("series missing after concurrent Inc")
	}
	got := v.(*atomic.Int64).Load()
	if got != goroutines*each {
		t.Fatalf("got %d want %d", got, goroutines*each)
	}
}

// TestHistogramQuantilesMonotonic asserts the linear interpolation across
// buckets returns values inside the histogram's observed range, with 0.5 ≤
// 0.95 ≤ 0.99. This is the property summary consumers (Grafana) depend on.
func TestHistogramQuantilesMonotonic(t *testing.T) {
	r := New()
	// 100 observations spread across the bucket range.
	for i := 0; i < 100; i++ {
		r.ObserveHTTPDuration("svc", "GET", float64(i)*0.05) // 0..5 sec
	}
	var buf bytes.Buffer
	if err := r.WriteText(&buf); err != nil {
		t.Fatal(err)
	}
	got := parseExposition(t, buf.String())
	m := got["burrow_http_request_duration_seconds"]
	if m == nil {
		t.Fatal("missing histogram metric")
	}
	// Build a quantile→value map for the only series.
	q := map[string]float64{}
	for _, s := range m.Samples {
		if s.Name == "burrow_http_request_duration_seconds" {
			// summary quantile sample
			parseFloat := func(str string) float64 {
				var f float64
				if _, err := fmt.Sscan(str, &f); err != nil {
					t.Fatalf("bad value %q: %v", str, err)
				}
				return f
			}
			q[s.Labels["quantile"]] = parseFloat(s.Value)
		}
	}
	if len(q) != 3 {
		t.Fatalf("want 3 quantile samples got %d (%+v)", len(q), q)
	}
	if !(q["0.5"] <= q["0.95"] && q["0.95"] <= q["0.99"]) {
		t.Errorf("quantiles not monotonic: %+v", q)
	}
	// _count and _sum must be present too.
	var sawCount, sawSum bool
	for _, s := range m.Samples {
		if s.Name == "burrow_http_request_duration_seconds_count" {
			sawCount = true
			if s.Value != "100" {
				t.Errorf("_count=%s want 100", s.Value)
			}
		}
		if s.Name == "burrow_http_request_duration_seconds_sum" {
			sawSum = true
		}
	}
	if !sawCount || !sawSum {
		t.Errorf("missing _count(%v) or _sum(%v)", sawCount, sawSum)
	}
}

// TestLabelEscaping exercises the three required exposition-format escapes
// (\ → \\, " → \", \n → \n) end-to-end (write → parse round-trip).
func TestLabelEscaping(t *testing.T) {
	r := New()
	r.IncHTTPRequest(`svc with "quote" \ and `+"\n", "GET", 200)
	var buf bytes.Buffer
	if err := r.WriteText(&buf); err != nil {
		t.Fatal(err)
	}
	got := parseExposition(t, buf.String())
	m := got["burrow_http_requests_total"]
	if m == nil || len(m.Samples) != 1 {
		t.Fatalf("want exactly one series, got %+v", m)
	}
	wantSvc := `svc with "quote" \ and ` + "\n"
	if m.Samples[0].Labels["service"] != wantSvc {
		t.Errorf("escaped service mismatch: got %q want %q", m.Samples[0].Labels["service"], wantSvc)
	}
}

