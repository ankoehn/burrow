package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/ankoehn/burrow/internal/db"
)

// ListClients returns live clients grouped with aggregate traffic (admin only).
// GET /api/v1/clients
func (d Deps) ListClients(w http.ResponseWriter, r *http.Request) {
	var out []ClientView
	if d.Clients != nil {
		out = d.Clients.ListClients()
	}
	if out == nil {
		out = []ClientView{}
	}
	writeJSON(w, http.StatusOK, out)
}

// GetClient returns one client + its services (admin only).
// GET /api/v1/clients/{sessionID}
func (d Deps) GetClient(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "sessionID")
	if d.Clients == nil {
		writeErr(w, http.StatusNotFound, "client not found")
		return
	}
	cd, ok := d.Clients.GetClient(id)
	if !ok {
		writeErr(w, http.StatusNotFound, "client not found")
		return
	}
	if cd.Services == nil {
		cd.Services = []ClientServiceView{}
	}
	writeJSON(w, http.StatusOK, cd)
}

type accessModeReq struct {
	AccessMode string `json:"access_mode"`
}

// SetAccessMode sets a service's per-service access mode. PUT
// /api/v1/tunnels/{id}/access-mode. Scoped to the caller (own tunnels);
// 'open' is runtime-effective, 'api_key'/'burrow_login' are accepted and
// persisted but inert in v0.2.0.
func (d Deps) SetAccessMode(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	var in accessModeReq
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.AccessMode == "" {
		writeErr(w, http.StatusBadRequest, "access_mode is required")
		return
	}
	err := d.AccessModes.SetTunnelAccessMode(r.Context(), id, userID(r.Context()), in.AccessMode)
	if errors.Is(err, db.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "tunnel not found")
		return
	}
	if err != nil {
		// store.ErrInvalidAccessMode surfaces as a 400 (its message contains
		// the allowed set); any other error is a 500.
		if strings.Contains(err.Error(), "access_mode must be") {
			writeErr(w, http.StatusBadRequest, "access_mode must be 'open', 'api_key', or 'burrow_login'")
			return
		}
		writeErr(w, http.StatusInternalServerError, "set access mode failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
