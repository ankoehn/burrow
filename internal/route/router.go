// Package route implements Burrow's v0.4.0 model registry + upstream
// routing strategies + active health checks + circuit breaker.
//
// The Router is split into two responsibilities:
//
//   - Pick(): a pure function of the current health snapshot, the Policy
//     (operator-supplied), and the per-request RouteContext. Returns a
//     Pick describing the chosen backend and a Retry closure that lets
//     the caller (the proxy hot path, Task 10) walk to the next candidate
//     when the idempotency rule allows.
//
//   - HealthChecker: an active /healthz probe loop that owns the live
//     "healthy" flag and circuit-breaker state for each registered
//     backend. The Router itself implements HealthChecker so tests can
//     swap a stub in; the real binary uses the in-process implementation.
//
// Pinned invariants (Spec Part C):
//   - Five strategies: single, failover, weighted, header_based, sticky.
//   - Failover idempotency: the *caller* decides whether to invoke
//     Pick.Retry; the Router only supplies the next candidate. (Spec:
//     retry only when a connection error occurred before any byte was
//     streamed back, OR Idempotency-Key is set AND no body bytes streamed.)
//   - Sticky uses consistent-hash (FNV-1a 64) so adding backends shifts
//     only the new ranges instead of reshuffling every session.
//   - Circuit breaker trips when rolling-window failure_pct exceeds the
//     configured threshold; it reopens once cool_down_seconds have elapsed.
package route

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	mrand "math/rand/v2"
	"sort"
	"sync"
	"time"
)

// Strategy is one of the five upstream routing strategies (spec Part C.2).
type Strategy string

// Strategy enum constants.
const (
	StrategySingle      Strategy = "single"
	StrategyFailover    Strategy = "failover"
	StrategyWeighted    Strategy = "weighted"
	StrategyHeaderBased Strategy = "header_based"
	StrategySticky      Strategy = "sticky"
)

// Backend is one routing target — a (service_id, weight, concrete_model)
// triple that the proxy will direct an upstream request to.
type Backend struct {
	ServiceID     string
	Weight        int    // weighted-strategy only; 0 means "equal share"
	ConcreteModel string // exposed to the upstream Director / aliased away from the client-facing model name
}

// Policy is the operator-supplied routing rule for a single ai-endpoint
// (the v0.4.0 ServiceAIConfig.routing block, spec Part B.7).
type Policy struct {
	Strategy             Strategy
	ModelAlias           string
	Backends             []Backend
	HeaderName           string // header_based: the request header to inspect (default "X-Burrow-Model")
	CircuitFailurePct    int    // breaker tripping threshold (0–100)
	CircuitWindowSeconds int    // rolling-window size; default 60s
	CircuitCoolDownSecs  int    // cool-down before re-evaluating
	TranslateTo          string // "none"|"openai"|"anthropic"
}

// Pick is the outcome of a single Pick() invocation. ServiceID + ConcreteModel
// identify the chosen upstream backend; Retry is a closure that returns the
// next candidate when the caller decides to fail over (idempotency rule).
//
// NewStickyID is non-empty only for the sticky strategy when the caller
// supplied an empty StickySessionID — in that case Pick generates a fresh
// session id and the proxy is expected to Set-Cookie it on the response.
type Pick struct {
	ServiceID     string
	ConcreteModel string
	Retry         func() (Pick, bool)
	NewStickyID   string
}

// RouteContext is the per-request input to Pick. The proxy populates it from
// the inbound request after alias resolution and after the access middleware
// has run.
type RouteContext struct {
	Kind            string // openai|anthropic|mcp|unknown
	Model           string // post-alias resolution
	Streaming       bool
	IdempotencyKey  string
	StickySessionID string // value of burrow_route_session cookie, "" if absent
	APIKeyID        string
	HeaderValues    map[string]string // for header_based strategy
}

// BackendRecord is what's read from services + service_ai_config when the
// Router needs to actually probe an upstream. The Lookup interface returns
// these per service id.
type BackendRecord struct {
	ServiceID  string
	HealthPath string // default "/healthz" when empty
	LocalAddr  string // upstream local address (v0.3.0 service.LocalAddr or equivalent)
}

// Lookup is the read-side surface the Router needs to materialise a
// BackendRecord from a service id. *db.DB (or a thin adapter) is expected
// to satisfy this in production; tests provide a stub.
type Lookup interface {
	GetBackend(ctx context.Context, serviceID string) (BackendRecord, bool, error)
}

// HealthChecker is the contract Pick consults to decide whether a backend
// is currently routable. The Router itself implements this for the simple
// in-process case; tests can swap a stub.
type HealthChecker interface {
	Healthy(serviceID string) bool
	Trip(serviceID string)
	ReportSuccess(serviceID string)
}

// Clock abstracts time.Now so the circuit-breaker cool-down logic is
// deterministic in tests. The default realClock is wall-clock.
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// ErrBackendUnavailable is the sentinel error returned when the routing
// strategy has no healthy candidates. The header_based strategy returns
// this when its specifically-targeted backend is currently tripped (spec:
// 503 {"error":"backend unavailable"}); other strategies return it when
// every backend in the policy is tripped.
var ErrBackendUnavailable = errors.New("backend unavailable")

// ErrNoBackends is returned when a Policy carries no Backends at all
// (operator misconfiguration).
var ErrNoBackends = errors.New("policy has no backends")

// Router is the shared instance for one Burrow process. It owns:
//   - the in-process HealthChecker state (per-backend healthy flag,
//     rolling-window failure counter, breaker tripped-at timestamp);
//   - the active health-check goroutine, when StartHealthLoop is called;
//   - lookups via the injected Lookup.
//
// Routers are safe for concurrent use. Pick is fully lock-free except for
// the breaker's atomic read of the per-backend state.
type Router struct {
	lookup Lookup
	log    *slog.Logger
	clock  Clock

	// hcOverride, when non-nil, replaces the Router's own HealthChecker.
	// Tests use this to script per-backend health without a real probe
	// loop.
	hcOverride HealthChecker

	mu       sync.RWMutex
	backends map[string]*backendState // serviceID → state (Router-owned HealthChecker)

	// Tunables. Defaults are spec-mandated; SetHealthInterval and
	// SetBreakerConfig let cmd/server override from settings before
	// StartHealthLoop runs.
	healthInterval time.Duration
	healthTimeout  time.Duration
	failurePct     int // tripping threshold (0–100)
	windowSecs     int // rolling-window size
	coolDownSecs   int // cool-down before reopening

	// Health-loop control.
	hlMu      sync.Mutex
	hlCancel  context.CancelFunc
	hlRunning bool
}

// backendState is the per-backend mutable bookkeeping the Router-owned
// HealthChecker maintains. The fields are guarded by mu (the per-backend
// mutex, not the Router's map mutex) — keep critical sections short.
type backendState struct {
	mu          sync.Mutex
	record      BackendRecord
	tripped     bool
	trippedAt   time.Time
	probes      ringBuffer // rolling-window success/failure samples
	probeCount  int
	failCount   int
}

// ringBuffer is a fixed 30-bucket second-resolution success/failure ring.
// Each bucket records (probes, failures) for one second of wall-clock.
type ringBuffer struct {
	buckets [30]bucket
	cur     int
	curSec  int64 // unix-second when cur was last written
}

type bucket struct {
	probes int
	fails  int
}

// New constructs a Router. hc, if non-nil, is the HealthChecker the Router
// will use for Pick decisions; nil means "use the Router's own impl".
func New(lookup Lookup, hc HealthChecker, log *slog.Logger) *Router {
	return NewWithClock(lookup, hc, log, realClock{})
}

// NewWithClock is New with an injected clock for tests.
func NewWithClock(lookup Lookup, hc HealthChecker, log *slog.Logger, clock Clock) *Router {
	if log == nil {
		log = slog.Default()
	}
	r := &Router{
		lookup:         lookup,
		log:            log,
		clock:          clock,
		hcOverride:     hc,
		backends:       map[string]*backendState{},
		healthInterval: 10 * time.Second,
		healthTimeout:  5 * time.Second,
		failurePct:     50,
		windowSecs:     60,
		coolDownSecs:   30,
	}
	return r
}

// SetHealthInterval overrides the probe period. The spec range is 1–300s;
// callers reading from operator config (cmd/server) are responsible for
// clamping. Sub-second values are accepted so tests can drive the loop
// quickly. Non-positive values become a 1s floor here as a final safety
// net (a zero ticker would panic).
func (r *Router) SetHealthInterval(d time.Duration) {
	if d <= 0 {
		d = time.Second
	}
	if d > 300*time.Second {
		d = 300 * time.Second
	}
	r.healthInterval = d
}

// SetBreakerConfig sets the rolling-window breaker tunables.
//
// failurePct (0–100): tripping threshold; >= this fraction of failures in
// the window flips the backend tripped.
//
// windowSeconds (1–30): rolling window size (capped at the 30-bucket ring).
//
// coolDownSecs (>= 1): how long the breaker stays tripped before reopening.
func (r *Router) SetBreakerConfig(failurePct, windowSeconds, coolDownSecs int) {
	if failurePct < 0 {
		failurePct = 0
	}
	if failurePct > 100 {
		failurePct = 100
	}
	if windowSeconds < 1 {
		windowSeconds = 1
	}
	if windowSeconds > 30 {
		windowSeconds = 30
	}
	if coolDownSecs < 1 {
		coolDownSecs = 1
	}
	r.failurePct = failurePct
	r.windowSecs = windowSeconds
	r.coolDownSecs = coolDownSecs
}

// RegisterBackend adds a backend to the in-process HealthChecker registry.
// Idempotent: a subsequent call with the same id updates the BackendRecord
// in place but keeps the existing breaker state.
func (r *Router) RegisterBackend(id string, rec BackendRecord) {
	r.mu.Lock()
	defer r.mu.Unlock()
	st, ok := r.backends[id]
	if !ok {
		r.backends[id] = &backendState{record: rec}
		return
	}
	st.mu.Lock()
	st.record = rec
	st.mu.Unlock()
}

// UnregisterBackend removes a backend from the registry. Used when an
// ai-endpoint is reconfigured to drop a target.
func (r *Router) UnregisterBackend(id string) {
	r.mu.Lock()
	delete(r.backends, id)
	r.mu.Unlock()
}

// Healthy implements HealthChecker. For backends not registered with the
// Router, returns true (the caller is in tests-with-scripted-health mode
// and uses the hcOverride for those ids).
func (r *Router) Healthy(serviceID string) bool {
	if r.hcOverride != nil {
		return r.hcOverride.Healthy(serviceID)
	}
	r.mu.RLock()
	st, ok := r.backends[serviceID]
	r.mu.RUnlock()
	if !ok {
		// Unregistered backends are assumed healthy (the Router is not the
		// source of truth for them — Pick gets called with hcOverride or
		// with a Policy whose backends have not been registered yet, which
		// can happen during startup).
		return true
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if !st.tripped {
		return true
	}
	// Tripped → check cool-down.
	if r.clock.Now().Sub(st.trippedAt) >= time.Duration(r.coolDownSecs)*time.Second {
		// Cool-down elapsed: reopen.
		st.tripped = false
		st.failCount = 0
		st.probeCount = 0
		return true
	}
	return false
}

// Trip implements HealthChecker — forces the breaker to the tripped state
// (used by the proxy when it observes a 5xx storm or a network error).
func (r *Router) Trip(serviceID string) {
	if r.hcOverride != nil {
		r.hcOverride.Trip(serviceID)
		return
	}
	r.mu.RLock()
	st, ok := r.backends[serviceID]
	r.mu.RUnlock()
	if !ok {
		// Trip on an unregistered backend is a no-op; the proxy may call
		// us before RegisterBackend has run.
		return
	}
	st.mu.Lock()
	st.tripped = true
	st.trippedAt = r.clock.Now()
	st.mu.Unlock()
}

// ReportSuccess implements HealthChecker — resets the failure window and,
// if currently tripped, clears the tripped flag immediately. Used by the
// proxy when it sees a successful upstream response after a transient blip.
func (r *Router) ReportSuccess(serviceID string) {
	if r.hcOverride != nil {
		r.hcOverride.ReportSuccess(serviceID)
		return
	}
	r.mu.RLock()
	st, ok := r.backends[serviceID]
	r.mu.RUnlock()
	if !ok {
		return
	}
	st.mu.Lock()
	st.tripped = false
	st.failCount = 0
	st.probeCount = 0
	st.mu.Unlock()
}

// Pick returns a chosen backend per Policy + RouteContext. Behaviour by
// strategy is documented inline; the caller is responsible for honouring
// the idempotency rule when invoking Pick.Retry.
func (r *Router) Pick(_ context.Context, p Policy, rc RouteContext) (Pick, error) {
	if len(p.Backends) == 0 {
		return Pick{}, ErrNoBackends
	}
	hc := r.healthChecker()

	switch p.Strategy {
	case StrategySingle, "":
		// Default to single when strategy is empty (safest fallback).
		return r.pickSingle(p, hc)
	case StrategyFailover:
		return r.pickFailover(p, hc)
	case StrategyWeighted:
		return r.pickWeighted(p, hc)
	case StrategyHeaderBased:
		return r.pickHeaderBased(p, rc, hc)
	case StrategySticky:
		return r.pickSticky(p, rc, hc)
	}
	return Pick{}, fmt.Errorf("unknown strategy %q", p.Strategy)
}

func (r *Router) healthChecker() HealthChecker {
	if r.hcOverride != nil {
		return r.hcOverride
	}
	return r
}

// pickSingle: return the first backend, regardless of health (a "single"
// strategy operator opted out of failover).
func (r *Router) pickSingle(p Policy, _ HealthChecker) (Pick, error) {
	b := p.Backends[0]
	return Pick{
		ServiceID:     b.ServiceID,
		ConcreteModel: b.ConcreteModel,
		Retry:         func() (Pick, bool) { return Pick{}, false },
	}, nil
}

// pickFailover: walk the Backends list in order, skipping tripped ones;
// the returned Retry walks to the next healthy one.
func (r *Router) pickFailover(p Policy, hc HealthChecker) (Pick, error) {
	healthy := make([]Backend, 0, len(p.Backends))
	for _, b := range p.Backends {
		if hc.Healthy(b.ServiceID) {
			healthy = append(healthy, b)
		}
	}
	if len(healthy) == 0 {
		return Pick{}, ErrBackendUnavailable
	}
	return failoverPickFromList(healthy, 0), nil
}

// failoverPickFromList builds a Pick that points at healthy[idx] and whose
// Retry closure walks to healthy[idx+1]. When idx is past the end, Retry
// returns (Pick{}, false) to signal exhaustion.
func failoverPickFromList(healthy []Backend, idx int) Pick {
	b := healthy[idx]
	return Pick{
		ServiceID:     b.ServiceID,
		ConcreteModel: b.ConcreteModel,
		Retry: func() (Pick, bool) {
			if idx+1 >= len(healthy) {
				return Pick{}, false
			}
			return failoverPickFromList(healthy, idx+1), true
		},
	}
}

// pickWeighted: cumulative-weight random draw across the healthy subset.
// Skips tripped backends; the weights of healthy backends still sum to
// the *configured* total only if none are tripped — a tripped backend is
// excluded from the draw entirely.
func (r *Router) pickWeighted(p Policy, hc HealthChecker) (Pick, error) {
	type wb struct {
		b   Backend
		cum int64
	}
	cum := []wb{}
	var total int64
	for _, b := range p.Backends {
		if !hc.Healthy(b.ServiceID) {
			continue
		}
		w := int64(b.Weight)
		if w <= 0 {
			w = 1
		}
		total += w
		cum = append(cum, wb{b: b, cum: total})
	}
	if len(cum) == 0 {
		return Pick{}, ErrBackendUnavailable
	}
	x := mrand.Int64N(total)
	for _, c := range cum {
		if x < c.cum {
			return Pick{
				ServiceID:     c.b.ServiceID,
				ConcreteModel: c.b.ConcreteModel,
				Retry:         func() (Pick, bool) { return Pick{}, false },
			}, nil
		}
	}
	// Unreachable — cum[-1].cum == total and x < total.
	last := cum[len(cum)-1].b
	return Pick{ServiceID: last.ServiceID, ConcreteModel: last.ConcreteModel,
		Retry: func() (Pick, bool) { return Pick{}, false }}, nil
}

// pickHeaderBased: index into Backends by ConcreteModel == rc.HeaderValues[HeaderName].
// Absent header → first backend (single fallback per spec). Target tripped → 503.
func (r *Router) pickHeaderBased(p Policy, rc RouteContext, hc HealthChecker) (Pick, error) {
	headerName := p.HeaderName
	if headerName == "" {
		headerName = "X-Burrow-Model"
	}
	v := rc.HeaderValues[headerName]
	if v == "" {
		// Fallback to single (first backend) — must still be healthy.
		first := p.Backends[0]
		if !hc.Healthy(first.ServiceID) {
			return Pick{}, ErrBackendUnavailable
		}
		return Pick{
			ServiceID:     first.ServiceID,
			ConcreteModel: first.ConcreteModel,
			Retry:         func() (Pick, bool) { return Pick{}, false },
		}, nil
	}
	for _, b := range p.Backends {
		if b.ConcreteModel == v {
			if !hc.Healthy(b.ServiceID) {
				return Pick{}, ErrBackendUnavailable
			}
			return Pick{
				ServiceID:     b.ServiceID,
				ConcreteModel: b.ConcreteModel,
				Retry:         func() (Pick, bool) { return Pick{}, false },
			}, nil
		}
	}
	// Header value didn't match any backend's ConcreteModel — fall back
	// to first (single) behaviour. Spec is ambiguous here; "absent header
	// falls back to `single`" is the closest documented behaviour, and
	// the same logic is the safest extension to "unknown value".
	first := p.Backends[0]
	if !hc.Healthy(first.ServiceID) {
		return Pick{}, ErrBackendUnavailable
	}
	return Pick{
		ServiceID:     first.ServiceID,
		ConcreteModel: first.ConcreteModel,
		Retry:         func() (Pick, bool) { return Pick{}, false },
	}, nil
}

// pickSticky uses a real consistent-hash ring: each backend gets
// `vnodesPerWeight * weight` virtual nodes hashed onto a 64-bit ring;
// each session id is hashed to a position and routed to the next vnode
// in sorted order. Adding a backend only "steals" the ranges
// immediately adjacent to its new vnodes — the rest of the sessions
// keep mapping to the same backend.
func (r *Router) pickSticky(p Policy, rc RouteContext, hc HealthChecker) (Pick, error) {
	// Sort backends by ServiceID for deterministic ordering across
	// processes — operators can rely on the same session id mapping the
	// same way on every node.
	sorted := append([]Backend(nil), p.Backends...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ServiceID < sorted[j].ServiceID })

	// Filter to healthy.
	healthy := make([]Backend, 0, len(sorted))
	for _, b := range sorted {
		if hc.Healthy(b.ServiceID) {
			healthy = append(healthy, b)
		}
	}
	if len(healthy) == 0 {
		return Pick{}, ErrBackendUnavailable
	}

	// Generate a fresh session id when the caller has none — the proxy
	// will Set-Cookie it on the response.
	newID := ""
	sid := rc.StickySessionID
	if sid == "" {
		var err error
		sid, err = NewStickySessionID()
		if err != nil {
			return Pick{}, fmt.Errorf("generate sticky session: %w", err)
		}
		newID = sid
	}

	// Build the consistent-hash ring out of virtual nodes. Using 64
	// vnodes per unit weight gives a smooth distribution and bounds the
	// reshuffle on adding/removing a backend to ~1/N of sessions.
	const vnodesPerWeight = 64
	type vnode struct {
		pos uint64
		idx int // index into healthy[]
	}
	ring := make([]vnode, 0, len(healthy)*vnodesPerWeight)
	for i, b := range healthy {
		w := b.Weight
		if w <= 0 {
			w = 1
		}
		n := vnodesPerWeight * w
		for j := 0; j < n; j++ {
			h := fnv.New64a()
			_, _ = h.Write([]byte(b.ServiceID))
			_, _ = h.Write([]byte{'#'})
			var jb [8]byte
			for k := 0; k < 8; k++ {
				jb[k] = byte(j >> (k * 8))
			}
			_, _ = h.Write(jb[:])
			ring = append(ring, vnode{pos: h.Sum64(), idx: i})
		}
	}
	sort.Slice(ring, func(i, j int) bool { return ring[i].pos < ring[j].pos })

	// Hash the session id onto the ring; pick the first vnode with
	// position >= the session's hash (wrap to ring[0] when past the end).
	h := fnv.New64a()
	_, _ = h.Write([]byte(sid))
	x := h.Sum64()
	pickIdx := sort.Search(len(ring), func(i int) bool { return ring[i].pos >= x })
	if pickIdx == len(ring) {
		pickIdx = 0
	}
	b := healthy[ring[pickIdx].idx]
	return Pick{
		ServiceID:     b.ServiceID,
		ConcreteModel: b.ConcreteModel,
		NewStickyID:   newID,
		Retry:         func() (Pick, bool) { return Pick{}, false },
	}, nil
}

// NewStickySessionID returns a fresh 16-byte random sticky session id,
// base64url-encoded (no padding). Suitable for the `burrow_route_session`
// cookie value (spec C.2 sticky).
func NewStickySessionID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// fakeClock is the test seam for Clock. It is defined here (not in a
// _test.go file) so cmd/server-style tests in other packages can use it
// too if needed — it's exported via NewWithClock.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (f *fakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}
