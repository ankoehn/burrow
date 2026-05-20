package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ankoehn/burrow/internal/authz"
	"github.com/ankoehn/burrow/internal/db"
	"github.com/ankoehn/burrow/internal/store"
)

// serviceResp is the JSON shape of one service in the list response (Part E).
type serviceResp struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Type         string `json:"type"`
	Subdomain    string `json:"subdomain"`
	Hostname     string `json:"hostname"`
	AccessMode   string `json:"access_mode"`
	APIKeyHeader string `json:"api_key_header"`
	Connected    bool   `json:"connected"`
	RemotePort   int    `json:"remote_port"`
	LocalAddr    string `json:"local_addr"`
}

// serviceDetailResp extends serviceResp with the single-service aggregate fields.
type serviceDetailResp struct {
	serviceResp
	APIKeyCount  int      `json:"api_key_count"`
	AccessPolicy []string `json:"access_policy"`
}

// apiKeyResp is the safe (no hash/plaintext) representation of a service API key.
type apiKeyResp struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	LastUsed  *time.Time `json:"last_used"`
	CreatedAt time.Time  `json:"created_at"`
}

// createAPIKeyResp is returned by POST /api-keys — plaintext shown once.
type createAPIKeyResp struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Key  string `json:"key"`
}

// accessPolicyResp is the JSON shape for GET /access-policy.
type accessPolicyResp struct {
	Roles []string `json:"roles"`
}

// composeHostname returns "<subdomain>.<authDomain>" when both are non-empty,
// and "" otherwise (degraded / tcp service / auth_domain not configured).
func composeHostname(subdomain, authDomain string) string {
	if subdomain == "" || authDomain == "" {
		return ""
	}
	return subdomain + "." + authDomain
}

// composeLive queries the LiveTunnelLookup for live state and returns the
// snapshot (or a zero-value snapshot when d.LiveTunnels is nil or miss).
func (d Deps) composeLive(serviceID string) LiveTunnelSnapshot {
	if d.LiveTunnels == nil {
		return LiveTunnelSnapshot{}
	}
	snap, _ := d.LiveTunnels.LookupByServiceID(serviceID)
	return snap
}

// callerRole loads the role for the authenticated user from the context.
// Returns "" on lookup failure; handlers should treat "" as an unprivileged role.
func (d Deps) callerRole(r *http.Request) (string, error) {
	uid := userID(r.Context())
	u, err := d.Users.GetUserByID(r.Context(), uid)
	if err != nil {
		return "", err
	}
	return u.Role, nil
}

// mapServiceErr maps a store/db error to the appropriate HTTP status + JSON body.
// Returns false when the error was not handled (caller should write 500).
func mapServiceErr(w http.ResponseWriter, err error, notFoundMsg string) bool {
	switch {
	case errors.Is(err, store.ErrForbidden):
		writeErr(w, http.StatusForbidden, "forbidden")
		return true
	case errors.Is(err, db.ErrNotFound):
		writeErr(w, http.StatusNotFound, notFoundMsg)
		return true
	case errors.Is(err, store.ErrInvalidAccessMode):
		writeErr(w, http.StatusBadRequest, "access_mode must be 'open', 'api_key', 'burrow_login', or 'mtls'")
		return true
	case errors.Is(err, store.ErrServiceNotHTTP):
		writeErr(w, http.StatusConflict, "api_key, burrow_login, and mtls require an http service")
		return true
	case errors.Is(err, store.ErrMTLSCARequired):
		writeErr(w, http.StatusBadRequest, "mtls access mode requires mtls_ca_pem")
		return true
	case errors.Is(err, store.ErrInvalidMTLSCAPEM):
		writeErr(w, http.StatusBadRequest, "invalid CA PEM")
		return true
	case errors.Is(err, store.ErrNameRequired):
		writeErr(w, http.StatusBadRequest, "name is required")
		return true
	case errors.Is(err, store.ErrUnknownRole):
		writeErr(w, http.StatusBadRequest, "unknown role")
		return true
	}
	return false
}

// ListServices handles GET /api/v1/services.
func (d Deps) ListServices(w http.ResponseWriter, r *http.Request) {
	role, err := d.callerRole(r)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	uid := userID(r.Context())
	svcs, err := d.Services.ListServices(r.Context(), uid, role)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]serviceResp, len(svcs))
	for i, sv := range svcs {
		snap := d.composeLive(sv.ID)
		out[i] = serviceResp{
			ID:           sv.ID,
			Name:         sv.Name,
			Type:         sv.Type,
			Subdomain:    sv.Subdomain,
			Hostname:     composeHostname(sv.Subdomain, d.AuthDomain),
			AccessMode:   sv.AccessMode,
			APIKeyHeader: sv.APIKeyHeader,
			Connected:    snap.Connected,
			RemotePort:   snap.RemotePort,
			LocalAddr:    snap.LocalAddr,
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// GetService handles GET /api/v1/services/{serviceID}.
func (d Deps) GetService(w http.ResponseWriter, r *http.Request) {
	serviceID := chi.URLParam(r, "serviceID")
	role, err := d.callerRole(r)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	uid := userID(r.Context())
	det, err := d.Services.GetService(r.Context(), uid, role, serviceID)
	if err != nil {
		if !mapServiceErr(w, err, "service not found") {
			writeErr(w, http.StatusInternalServerError, "internal error")
		}
		return
	}
	snap := d.composeLive(det.ID)
	policy := det.AccessPolicy
	if policy == nil {
		policy = []string{}
	}
	resp := serviceDetailResp{
		serviceResp: serviceResp{
			ID:           det.ID,
			Name:         det.Name,
			Type:         det.Type,
			Subdomain:    det.Subdomain,
			Hostname:     composeHostname(det.Subdomain, d.AuthDomain),
			AccessMode:   det.AccessMode,
			APIKeyHeader: det.APIKeyHeader,
			Connected:    snap.Connected,
			RemotePort:   snap.RemotePort,
			LocalAddr:    snap.LocalAddr,
		},
		APIKeyCount:  det.APIKeyCount,
		AccessPolicy: policy,
	}
	writeJSON(w, http.StatusOK, resp)
}

type setAccessModeReq struct {
	AccessMode   string `json:"access_mode"`
	APIKeyHeader string `json:"api_key_header"`
	// MTLSCAPEM is the operator-supplied PEM-encoded trust anchor used to
	// verify visitor client certs in mtls mode. Required when AccessMode
	// is "mtls"; ignored for any other mode. Burrow does NOT sign client
	// certs in v0.4.0.
	MTLSCAPEM string `json:"mtls_ca_pem"`
}

// SetServiceAccessMode handles PUT /api/v1/services/{serviceID}/access-mode.
//
// Accepts {access_mode: "open"|"api_key"|"burrow_login"|"mtls"}. For mtls
// the request body MUST carry a non-empty mtls_ca_pem with at least one
// CERTIFICATE block; the store layer validates the PEM and returns 400 on
// bad input.
func (d Deps) SetServiceAccessMode(w http.ResponseWriter, r *http.Request) {
	serviceID := chi.URLParam(r, "serviceID")
	// 16 KiB accommodates a multi-cert PEM bundle while still bounding
	// memory growth (a CA + a few intermediates fit easily).
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	var in setAccessModeReq
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.AccessMode == "" {
		writeErr(w, http.StatusBadRequest, "access_mode is required")
		return
	}
	// Spec Q4 RESOLVED: reject burrow_login when no auth_domain is configured.
	if in.AccessMode == "burrow_login" && d.AuthDomain == "" {
		writeErr(w, http.StatusConflict, "burrow_login requires a configured auth_domain")
		return
	}
	// api_key_header is only honored for api_key mode (spec Part C).
	header := ""
	if in.AccessMode == "api_key" {
		header = in.APIKeyHeader
	}
	// mtls_ca_pem is only honored for mtls mode.
	var caPEM []byte
	if in.AccessMode == "mtls" {
		caPEM = []byte(in.MTLSCAPEM)
	}
	role, err := d.callerRole(r)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	uid := userID(r.Context())
	if err := d.Services.SetServiceAccessMode(r.Context(), uid, role, serviceID, in.AccessMode, header, caPEM); err != nil {
		if !mapServiceErr(w, err, "service not found") {
			writeErr(w, http.StatusInternalServerError, "internal error")
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListAPIKeys handles GET /api/v1/services/{serviceID}/api-keys.
func (d Deps) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	serviceID := chi.URLParam(r, "serviceID")
	role, err := d.callerRole(r)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	uid := userID(r.Context())
	keys, err := d.Services.ListAPIKeys(r.Context(), uid, role, serviceID)
	if err != nil {
		if !mapServiceErr(w, err, "service not found") {
			writeErr(w, http.StatusInternalServerError, "internal error")
		}
		return
	}
	out := make([]apiKeyResp, len(keys))
	for i, k := range keys {
		out[i] = apiKeyResp{
			ID:        k.ID,
			Name:      k.Name,
			LastUsed:  k.LastUsed,
			CreatedAt: k.CreatedAt,
		}
	}
	writeJSON(w, http.StatusOK, out)
}

type createAPIKeyReq struct {
	Name string `json:"name"`
}

// CreateAPIKey handles POST /api/v1/services/{serviceID}/api-keys.
func (d Deps) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	serviceID := chi.URLParam(r, "serviceID")
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var in createAPIKeyReq
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	role, err := d.callerRole(r)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	uid := userID(r.Context())
	id, plaintext, err := d.Services.CreateAPIKey(r.Context(), uid, role, serviceID, in.Name)
	if err != nil {
		if !mapServiceErr(w, err, "service not found") {
			writeErr(w, http.StatusInternalServerError, "internal error")
		}
		return
	}
	writeJSON(w, http.StatusCreated, createAPIKeyResp{
		ID:   id,
		Name: in.Name,
		Key:  plaintext,
	})
}

// DeleteAPIKey handles DELETE /api/v1/services/{serviceID}/api-keys/{id}.
func (d Deps) DeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	serviceID := chi.URLParam(r, "serviceID")
	keyID := chi.URLParam(r, "id")
	role, err := d.callerRole(r)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	uid := userID(r.Context())
	if err := d.Services.DeleteAPIKey(r.Context(), uid, role, serviceID, keyID); err != nil {
		if !mapServiceErr(w, err, "api key not found") {
			writeErr(w, http.StatusInternalServerError, "internal error")
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetAccessPolicy handles GET /api/v1/services/{serviceID}/access-policy.
func (d Deps) GetAccessPolicy(w http.ResponseWriter, r *http.Request) {
	serviceID := chi.URLParam(r, "serviceID")
	role, err := d.callerRole(r)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	uid := userID(r.Context())
	roles, err := d.Services.GetAccessPolicy(r.Context(), uid, role, serviceID)
	if err != nil {
		if !mapServiceErr(w, err, "service not found") {
			writeErr(w, http.StatusInternalServerError, "internal error")
		}
		return
	}
	if roles == nil {
		roles = []string{}
	}
	writeJSON(w, http.StatusOK, accessPolicyResp{Roles: roles})
}

type setAccessPolicyReq struct {
	Roles []string `json:"roles"`
}

// SetAccessPolicy handles PUT /api/v1/services/{serviceID}/access-policy.
func (d Deps) SetAccessPolicy(w http.ResponseWriter, r *http.Request) {
	serviceID := chi.URLParam(r, "serviceID")
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	var in setAccessPolicyReq
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if in.Roles == nil {
		in.Roles = []string{}
	}
	// Spec Part D: pre-validate each role before calling the store so the error
	// body can include the offending role name: {"error":"unknown role \"<r>\""}
	for _, roleName := range in.Roles {
		if _, ok := authz.Get(roleName); !ok {
			writeErr(w, http.StatusBadRequest, fmt.Sprintf("unknown role %q", roleName))
			return
		}
	}
	role, err := d.callerRole(r)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	uid := userID(r.Context())
	if err := d.Services.SetAccessPolicy(r.Context(), uid, role, serviceID, in.Roles); err != nil {
		if !mapServiceErr(w, err, "service not found") {
			writeErr(w, http.StatusInternalServerError, "internal error")
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
