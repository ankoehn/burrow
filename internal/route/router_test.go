// Package route_test exercises the Router + HealthChecker contract.
package route

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func discardLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// staticLookup is a tiny Lookup stub: a map of serviceID → BackendRecord.
type staticLookup struct {
	rows map[string]BackendRecord
}

func (s *staticLookup) GetBackend(_ context.Context, id string) (BackendRecord, bool, error) {
	r, ok := s.rows[id]
	return r, ok, nil
}

// scriptedHealth lets a test pin the healthy/tripped state per backend.
type scriptedHealth struct {
	mu      sync.Mutex
	healthy map[string]bool // default: true when absent
	tripped map[string]bool
}

func newScriptedHealth() *scriptedHealth {
	return &scriptedHealth{healthy: map[string]bool{}, tripped: map[string]bool{}}
}
func (s *scriptedHealth) Healthy(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v, ok := s.tripped[id]; ok && v {
		return false
	}
	if v, ok := s.healthy[id]; ok {
		return v
	}
	return true
}
func (s *scriptedHealth) Trip(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tripped[id] = true
}
func (s *scriptedHealth) ReportSuccess(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tripped[id] = false
	s.healthy[id] = true
}

func newRouterForTest(t *testing.T, hc HealthChecker) *Router {
	t.Helper()
	return New(&staticLookup{rows: map[string]BackendRecord{}}, hc, discardLog())
}

// TestPickWeightedDistribution: 10k draws, ±5% tolerance on a 90/10 split.
func TestPickWeightedDistribution(t *testing.T) {
	r := newRouterForTest(t, newScriptedHealth())
	policy := Policy{
		Strategy: StrategyWeighted,
		Backends: []Backend{
			{ServiceID: "A", Weight: 90, ConcreteModel: "a"},
			{ServiceID: "B", Weight: 10, ConcreteModel: "b"},
		},
	}
	counts := map[string]int{}
	const n = 10000
	for i := 0; i < n; i++ {
		p, err := r.Pick(context.Background(), policy, RouteContext{})
		if err != nil {
			t.Fatalf("pick %d: %v", i, err)
		}
		counts[p.ServiceID]++
	}
	// Expect ~9000 / 1000; loose ±5% tolerance.
	wantA, wantB := 9000.0, 1000.0
	if math.Abs(float64(counts["A"])-wantA) > 0.05*wantA {
		t.Errorf("A draws = %d; want %g ± 5%%", counts["A"], wantA)
	}
	if math.Abs(float64(counts["B"])-wantB) > 0.05*wantB+50 {
		// allow a small absolute slack on the small bucket too
		t.Errorf("B draws = %d; want %g ± 5%% (+50 abs)", counts["B"], wantB)
	}
}

// TestPickFailoverSkipsTripped: head backend's tripped=true; Pick returns next.
func TestPickFailoverSkipsTripped(t *testing.T) {
	hc := newScriptedHealth()
	hc.Trip("A")
	r := newRouterForTest(t, hc)
	policy := Policy{
		Strategy: StrategyFailover,
		Backends: []Backend{
			{ServiceID: "A", Weight: 1, ConcreteModel: "a"},
			{ServiceID: "B", Weight: 1, ConcreteModel: "b"},
		},
	}
	p, err := r.Pick(context.Background(), policy, RouteContext{})
	if err != nil {
		t.Fatalf("pick: %v", err)
	}
	if p.ServiceID != "B" {
		t.Fatalf("first pick = %q; want B (A is tripped)", p.ServiceID)
	}
	// Retry should iterate the remaining (none after B).
	if next, ok := p.Retry(); ok {
		t.Errorf("Retry returned %+v ok=true; want exhausted", next)
	}
}

// TestPickFailoverAllTripped: every backend tripped → error.
func TestPickFailoverAllTripped(t *testing.T) {
	hc := newScriptedHealth()
	hc.Trip("A")
	hc.Trip("B")
	r := newRouterForTest(t, hc)
	policy := Policy{
		Strategy: StrategyFailover,
		Backends: []Backend{
			{ServiceID: "A", Weight: 1, ConcreteModel: "a"},
			{ServiceID: "B", Weight: 1, ConcreteModel: "b"},
		},
	}
	_, err := r.Pick(context.Background(), policy, RouteContext{})
	if err == nil {
		t.Fatalf("want error when all tripped; got nil")
	}
}

// TestPickFailoverRetryWalksList: untripped order yields each in turn.
func TestPickFailoverRetryWalksList(t *testing.T) {
	r := newRouterForTest(t, newScriptedHealth())
	policy := Policy{
		Strategy: StrategyFailover,
		Backends: []Backend{
			{ServiceID: "A", ConcreteModel: "a"},
			{ServiceID: "B", ConcreteModel: "b"},
			{ServiceID: "C", ConcreteModel: "c"},
		},
	}
	p, err := r.Pick(context.Background(), policy, RouteContext{})
	if err != nil || p.ServiceID != "A" {
		t.Fatalf("first pick = %+v err=%v; want A", p, err)
	}
	p2, ok := p.Retry()
	if !ok || p2.ServiceID != "B" {
		t.Fatalf("retry 2 = %+v ok=%v; want B", p2, ok)
	}
	p3, ok := p2.Retry()
	if !ok || p3.ServiceID != "C" {
		t.Fatalf("retry 3 = %+v ok=%v; want C", p3, ok)
	}
	p4, ok := p3.Retry()
	if ok {
		t.Fatalf("retry 4 = %+v ok=true; want exhausted", p4)
	}
}

// TestBreakerReopensAfterCooldown: trip, advance fake clock past cool_down,
// assert Pick selects the recovered backend.
func TestBreakerReopensAfterCooldown(t *testing.T) {
	fc := &fakeClock{now: time.Unix(1000, 0)}
	r := NewWithClock(
		&staticLookup{rows: map[string]BackendRecord{}},
		nil, // use Router's internal HealthChecker
		discardLog(),
		fc,
	)
	r.RegisterBackend("A", BackendRecord{ServiceID: "A"})
	r.SetBreakerConfig(50, 30, 60) // failure_pct=50%, window=30s, cooldown=60s

	// Manually trip the backend (mimics what the health loop / proxy does
	// when it sees an explicit 5xx storm).
	r.Trip("A")
	if r.Healthy("A") {
		t.Fatalf("backend should be unhealthy after Trip")
	}
	// Before cooldown: still tripped.
	fc.now = fc.now.Add(30 * time.Second)
	if r.Healthy("A") {
		t.Fatalf("backend should still be tripped at 30s (< 60s cooldown)")
	}
	// After cooldown: reopens.
	fc.now = fc.now.Add(31 * time.Second)
	if !r.Healthy("A") {
		t.Fatalf("backend should have reopened past cool_down")
	}
}

// TestPickHeaderBased: header value matches a backend's ConcreteModel; absent
// header falls back to first backend.
func TestPickHeaderBased(t *testing.T) {
	r := newRouterForTest(t, newScriptedHealth())
	policy := Policy{
		Strategy:   StrategyHeaderBased,
		HeaderName: "X-Burrow-Model",
		Backends: []Backend{
			{ServiceID: "ollama", ConcreteModel: "ollama-fast"},
			{ServiceID: "anthropic", ConcreteModel: "claude-3-5"},
		},
	}
	// Header points at the second backend.
	p, err := r.Pick(context.Background(), policy, RouteContext{
		HeaderValues: map[string]string{"X-Burrow-Model": "claude-3-5"},
	})
	if err != nil || p.ServiceID != "anthropic" {
		t.Fatalf("header-based pick: %+v err=%v", p, err)
	}
	// Header points at the first backend.
	p, err = r.Pick(context.Background(), policy, RouteContext{
		HeaderValues: map[string]string{"X-Burrow-Model": "ollama-fast"},
	})
	if err != nil || p.ServiceID != "ollama" {
		t.Fatalf("header-based pick #2: %+v err=%v", p, err)
	}
	// Absent header → first backend (single fallback).
	p, err = r.Pick(context.Background(), policy, RouteContext{})
	if err != nil || p.ServiceID != "ollama" {
		t.Fatalf("absent-header fallback: %+v err=%v", p, err)
	}
}

// TestPickHeaderBasedTrippedTarget: header points at a tripped backend → 503.
func TestPickHeaderBasedTrippedTarget(t *testing.T) {
	hc := newScriptedHealth()
	hc.Trip("anthropic")
	r := newRouterForTest(t, hc)
	policy := Policy{
		Strategy:   StrategyHeaderBased,
		HeaderName: "X-Burrow-Model",
		Backends: []Backend{
			{ServiceID: "ollama", ConcreteModel: "ollama-fast"},
			{ServiceID: "anthropic", ConcreteModel: "claude-3-5"},
		},
	}
	_, err := r.Pick(context.Background(), policy, RouteContext{
		HeaderValues: map[string]string{"X-Burrow-Model": "claude-3-5"},
	})
	if err == nil || err != ErrBackendUnavailable {
		t.Fatalf("want ErrBackendUnavailable; got %v", err)
	}
}

// TestPickSingle: simplest strategy — first backend, always.
func TestPickSingle(t *testing.T) {
	r := newRouterForTest(t, newScriptedHealth())
	policy := Policy{
		Strategy: StrategySingle,
		Backends: []Backend{{ServiceID: "only"}},
	}
	p, err := r.Pick(context.Background(), policy, RouteContext{})
	if err != nil || p.ServiceID != "only" {
		t.Fatalf("single: %+v err=%v", p, err)
	}
}

// TestPickSticky: same sticky_session_id maps to the same backend across
// calls; adding a backend shifts a small fraction of sessions.
func TestPickSticky(t *testing.T) {
	r := newRouterForTest(t, newScriptedHealth())
	twoBackends := Policy{
		Strategy: StrategySticky,
		Backends: []Backend{
			{ServiceID: "A", Weight: 1},
			{ServiceID: "B", Weight: 1},
		},
	}
	threeBackends := Policy{
		Strategy: StrategySticky,
		Backends: []Backend{
			{ServiceID: "A", Weight: 1},
			{ServiceID: "B", Weight: 1},
			{ServiceID: "C", Weight: 1},
		},
	}

	// Determinism: same session id → same pick across calls.
	const sid = "sess-abc"
	first, _ := r.Pick(context.Background(), twoBackends, RouteContext{StickySessionID: sid})
	for i := 0; i < 50; i++ {
		next, _ := r.Pick(context.Background(), twoBackends, RouteContext{StickySessionID: sid})
		if next.ServiceID != first.ServiceID {
			t.Fatalf("sticky non-deterministic: iter %d picked %s (want %s)", i, next.ServiceID, first.ServiceID)
		}
	}

	// Minimal reshuffle: adding a backend should move only ~33% of sessions
	// (the new range), and well under 100%. We assert <60% as a soft bound.
	const N = 1000
	moved := 0
	for i := 0; i < N; i++ {
		s := fmt.Sprintf("s-%d", i)
		a, _ := r.Pick(context.Background(), twoBackends, RouteContext{StickySessionID: s})
		b, _ := r.Pick(context.Background(), threeBackends, RouteContext{StickySessionID: s})
		if a.ServiceID != b.ServiceID {
			moved++
		}
	}
	if moved > N*60/100 {
		t.Errorf("adding a 3rd backend moved %d/%d sessions (>60%%); want minimal reshuffle", moved, N)
	}
	if moved == 0 {
		t.Errorf("adding a 3rd backend moved 0 sessions; expected ~33%%")
	}
}

// TestPickStickyFreshSession: empty StickySessionID → Pick returns a fresh
// 16-byte id in NewStickyID so the caller can Set-Cookie it.
func TestPickStickyFreshSession(t *testing.T) {
	r := newRouterForTest(t, newScriptedHealth())
	policy := Policy{
		Strategy: StrategySticky,
		Backends: []Backend{
			{ServiceID: "A"},
			{ServiceID: "B"},
		},
	}
	p, err := r.Pick(context.Background(), policy, RouteContext{StickySessionID: ""})
	if err != nil {
		t.Fatal(err)
	}
	if p.NewStickyID == "" {
		t.Fatalf("want NewStickyID populated when session id absent")
	}
	if p.ServiceID == "" {
		t.Fatalf("want a chosen backend; got empty")
	}
}

// TestHealthLoopMarksUnhealthy: stub HTTP server returns 500; the health
// loop trips the backend's circuit breaker and Pick skips it.
func TestHealthLoopMarksUnhealthy(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		http.Error(w, "no", http.StatusInternalServerError)
	}))
	defer srv.Close()

	r := New(
		&staticLookup{rows: map[string]BackendRecord{
			"X": {ServiceID: "X", LocalAddr: srv.URL, HealthPath: "/healthz"},
		}},
		nil,
		discardLog(),
	)
	// Trip threshold = 50% in a small window so 3 consecutive failures
	// definitively trip it; interval = 20ms for a snappy test.
	r.SetBreakerConfig(50, 5, 60)
	r.SetHealthInterval(20 * time.Millisecond)

	r.RegisterBackend("X", BackendRecord{
		ServiceID: "X", LocalAddr: srv.URL, HealthPath: "/healthz",
	})

	stop := r.StartHealthLoop(context.Background())
	defer stop()

	// Wait until the backend is marked unhealthy or 1.5s expires.
	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if !r.Healthy("X") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if r.Healthy("X") {
		t.Fatalf("backend X should be unhealthy after repeated 500s; hits=%d", atomic.LoadInt32(&hits))
	}

	// And Pick should skip it for failover.
	_, err := r.Pick(context.Background(), Policy{
		Strategy: StrategyFailover,
		Backends: []Backend{{ServiceID: "X"}},
	}, RouteContext{})
	if err == nil {
		t.Fatalf("Pick should error when only backend is unhealthy")
	}
}

// TestPickEmptyBackends: empty backends list → error.
func TestPickEmptyBackends(t *testing.T) {
	r := newRouterForTest(t, newScriptedHealth())
	_, err := r.Pick(context.Background(), Policy{Strategy: StrategySingle}, RouteContext{})
	if err == nil {
		t.Fatalf("want error on empty backends")
	}
}

// TestStickyAllTrippedFallback: every backend tripped → error (no fallback).
func TestStickyAllTrippedFallback(t *testing.T) {
	hc := newScriptedHealth()
	hc.Trip("A")
	hc.Trip("B")
	r := newRouterForTest(t, hc)
	policy := Policy{
		Strategy: StrategySticky,
		Backends: []Backend{{ServiceID: "A"}, {ServiceID: "B"}},
	}
	_, err := r.Pick(context.Background(), policy, RouteContext{StickySessionID: "sid"})
	if err == nil {
		t.Fatalf("want error when all sticky backends tripped")
	}
}
