package api

import "net/http"

// Healthz is an unauthenticated liveness probe: the process is up.
func (d Deps) Healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// Readyz is an unauthenticated readiness probe: 200 only when the database is
// reachable. Returns 503 when the DB ping fails or no Pinger is configured.
func (d Deps) Readyz(w http.ResponseWriter, r *http.Request) {
	if d.DB == nil {
		writeErr(w, http.StatusServiceUnavailable, "not ready: database not configured")
		return
	}
	if err := d.DB.PingContext(r.Context()); err != nil {
		writeErr(w, http.StatusServiceUnavailable, "not ready: database unreachable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}
