package api

import "net/http"

// ListTunnels returns the caller's live tunnels with byte counters.
func (d Deps) ListTunnels(w http.ResponseWriter, r *http.Request) {
	var out []TunnelView
	if d.Tunnels != nil {
		out = d.Tunnels.ListUserTunnels(userID(r.Context()))
	}
	if out == nil {
		out = []TunnelView{}
	}
	writeJSON(w, http.StatusOK, out)
}
