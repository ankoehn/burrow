package route

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// StartHealthLoop starts a goroutine that probes every registered backend's
// /healthz at the configured interval. Returns a stop function that cancels
// the goroutine and waits for it to exit. Calling StartHealthLoop a second
// time without first stopping the previous loop is a no-op (the existing
// loop's stop function is returned).
//
// Probe semantics (spec Part C, "Health check"):
//   - GET <local_addr><health_path> (defaults to /healthz when empty).
//   - 5s timeout per probe.
//   - 2xx/3xx response → success; 4xx is also success (the upstream is
//     reachable; 4xx on the probe path is an operator concern, not a
//     routability concern); 5xx or network error → failure.
//   - Each result is recorded in the 30-bucket second-resolution ring.
//     If the rolling-window failure_pct exceeds the configured threshold,
//     the backend is tripped.
func (r *Router) StartHealthLoop(parent context.Context) func() {
	r.hlMu.Lock()
	if r.hlRunning && r.hlCancel != nil {
		cancel := r.hlCancel
		r.hlMu.Unlock()
		return cancel
	}
	ctx, cancel := context.WithCancel(parent)
	stopped := make(chan struct{})
	r.hlCancel = func() {
		cancel()
		<-stopped
	}
	r.hlRunning = true
	r.hlMu.Unlock()

	go func() {
		defer close(stopped)
		client := &http.Client{Timeout: r.healthTimeout}
		t := time.NewTicker(r.healthInterval)
		defer t.Stop()

		// Run one probe pass immediately so tests don't have to wait for
		// the first tick.
		r.probeAll(ctx, client)

		for {
			select {
			case <-ctx.Done():
				r.hlMu.Lock()
				r.hlRunning = false
				r.hlMu.Unlock()
				return
			case <-t.C:
				r.probeAll(ctx, client)
			}
		}
	}()
	return r.hlCancel
}

func (r *Router) probeAll(ctx context.Context, client *http.Client) {
	r.mu.RLock()
	snapshot := make([]*backendState, 0, len(r.backends))
	for _, st := range r.backends {
		snapshot = append(snapshot, st)
	}
	r.mu.RUnlock()

	for _, st := range snapshot {
		st.mu.Lock()
		rec := st.record
		st.mu.Unlock()
		ok := probeOne(ctx, client, rec)
		r.recordProbe(st, ok)
	}
}

// probeOne returns true on a successful probe (2xx/3xx/4xx response within
// the timeout), false on 5xx or any network/timeout error.
func probeOne(ctx context.Context, client *http.Client, rec BackendRecord) bool {
	if rec.LocalAddr == "" {
		return false
	}
	path := rec.HealthPath
	if path == "" {
		path = "/healthz"
	}
	url := rec.LocalAddr
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		url = "http://" + url
	}
	// Ensure exactly one slash between base and path: trim a trailing one
	// from the base and ensure the path starts with one.
	url = strings.TrimRight(url, "/")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	url += path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode < 500
}

// recordProbe inserts a sample into the rolling-window ring for the given
// backend and re-evaluates the breaker.
func (r *Router) recordProbe(st *backendState, ok bool) {
	st.mu.Lock()
	defer st.mu.Unlock()

	now := r.clock.Now().Unix()
	// Advance the ring head if we've moved into a new second.
	if st.probes.curSec == 0 {
		st.probes.curSec = now
	}
	for st.probes.curSec < now {
		st.probes.cur = (st.probes.cur + 1) % len(st.probes.buckets)
		// Subtract the about-to-be-overwritten bucket from the rolling totals.
		old := st.probes.buckets[st.probes.cur]
		st.probeCount -= old.probes
		st.failCount -= old.fails
		st.probes.buckets[st.probes.cur] = bucket{}
		st.probes.curSec++
	}
	cur := &st.probes.buckets[st.probes.cur]
	cur.probes++
	st.probeCount++
	if !ok {
		cur.fails++
		st.failCount++
	}

	// Restrict the rolling window to the configured windowSecs by
	// summarising only the most-recent windowSecs buckets. Cheap loop —
	// the ring is 30 entries.
	winProbes := 0
	winFails := 0
	w := r.windowSecs
	if w > len(st.probes.buckets) {
		w = len(st.probes.buckets)
	}
	for i := 0; i < w; i++ {
		idx := (st.probes.cur - i + len(st.probes.buckets)) % len(st.probes.buckets)
		winProbes += st.probes.buckets[idx].probes
		winFails += st.probes.buckets[idx].fails
	}

	// Evaluate the breaker. Require at least 3 samples to avoid tripping
	// on a single 5xx response — the spec test "after 3 consecutive
	// failures the backend is unhealthy" matches this threshold.
	if winProbes >= 3 && winFails*100 >= winProbes*r.failurePct {
		if !st.tripped {
			st.tripped = true
			st.trippedAt = r.clock.Now()
		}
	}
}
