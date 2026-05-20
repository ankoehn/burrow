package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/ankoehn/burrow/internal/authz"
	"github.com/ankoehn/burrow/internal/cache/exact"
	"github.com/ankoehn/burrow/internal/db"
)

// CacheEngine is the narrow interface the cache handlers consume. The
// concrete *exact.Cache satisfies it; tests provide a fake.
type CacheEngine interface {
	Clear(ctx context.Context, scope string) error
	Stats(ctx context.Context) (entries int, onDiskBytes int64, hitRate float64, err error)
}

// CacheSettingsStore is the read/write surface for the global cache.settings
// JSON row in the `settings` table. The concrete *store.Store satisfies it via
// GetSettings/SaveSettings; for v0.4.0 we keep the wire blob as a single JSON
// value under the key "cache.settings".
type CacheSettingsStore interface {
	GetSettings(ctx context.Context) (map[string]string, error)
	SaveSettings(ctx context.Context, kv map[string]string) error
}

// CacheServiceLookup is the narrow surface used by the per-service variants
// (DELETE /services/{id}/cache/entries). It returns the owning user id of the
// service plus its per-service AI config blob (so future GET-list of
// per_service cache settings — Task 24 — can render overrides). Returns
// db.ErrNotFound for unknown service ids.
type CacheServiceLookup interface {
	// GetServiceOwner returns the user_id of the service with the given id,
	// or db.ErrNotFound.
	GetServiceOwner(ctx context.Context, serviceID string) (string, error)
	// GetServiceAIConfig returns the JSON blob from service_ai_config.config
	// for the given service id, or db.ErrNotFound when no row exists. The
	// caller decodes the blob and extracts the .cache sub-object.
	GetServiceAIConfig(ctx context.Context, serviceID string) ([]byte, error)
	// ListAllServiceAIConfigs returns every (service_id, config_json) pair so
	// the GET /cache/settings handler can render per-service overrides.
	ListAllServiceAIConfigs(ctx context.Context) ([]CacheServiceConfigRow, error)
}

// CacheServiceConfigRow is one (service_id, raw json) pair returned by
// ListAllServiceAIConfigs. Decoded by the handler into the per_service
// list.
type CacheServiceConfigRow struct {
	ServiceID string
	Config    []byte
}

// cacheSettingsKey is the row key in the settings table where the global
// cache.settings JSON blob is persisted. One row, one JSON value.
const cacheSettingsKey = "cache.settings"

// cacheSettingsResp is the JSON shape of GET /api/v1/cache/settings (spec
// Part B.3). global is the typed cache.Settings JSON; per_service is one
// row per service that has a stored AI-config blob, with the override flag
// set when the per-service cache block is non-empty.
type cacheSettingsResp struct {
	Global     cacheSettingsJSON         `json:"global"`
	PerService []cachePerServiceSettings `json:"per_service"`
}

// cacheSettingsJSON is the wire shape of the cache settings block. Mirrors
// the engine's exact.Settings exactly, but lives at the api layer so the
// JSON tags stay close to the handler.
type cacheSettingsJSON struct {
	Enabled       bool   `json:"enabled"`
	AppliesPer    string `json:"applies_per"`
	TTLSeconds    int    `json:"ttl_seconds"`
	MaxEntries    int    `json:"max_entries"`
	MaxPerEntryKB int    `json:"max_per_entry_kb"`
}

// cachePerServiceSettings is one row of the per_service array in
// cacheSettingsResp. Override is true when the service has a non-default
// cache block in its service_ai_config blob.
type cachePerServiceSettings struct {
	ServiceID     string `json:"service_id"`
	Enabled       bool   `json:"enabled"`
	AppliesPer    string `json:"applies_per"`
	TTLSeconds    int    `json:"ttl_seconds"`
	MaxEntries    int    `json:"max_entries"`
	MaxPerEntryKB int    `json:"max_per_entry_kb"`
	Override      bool   `json:"override"`
}

// cacheStatsResp is the JSON shape of GET /api/v1/cache/stats.
type cacheStatsResp struct {
	Entries     int     `json:"entries"`
	OnDiskBytes int64   `json:"on_disk_bytes"`
	HitRate24h  float64 `json:"hit_rate_24h"`
}

// settingsFromJSON converts the wire JSON blob (or an empty/nil row) into the
// engine's typed Settings, falling back to defaults for missing keys.
func settingsFromBlob(raw []byte) exact.Settings {
	if len(raw) == 0 {
		return exact.DefaultSettings
	}
	s, err := exact.SettingsFromJSON(raw)
	if err != nil {
		// Corrupt/legacy row: serve defaults rather than 500.
		return exact.DefaultSettings
	}
	return s
}

func toCacheSettingsJSON(s exact.Settings) cacheSettingsJSON {
	return cacheSettingsJSON{
		Enabled:       s.Enabled,
		AppliesPer:    s.AppliesPer,
		TTLSeconds:    s.TTLSeconds,
		MaxEntries:    s.MaxEntries,
		MaxPerEntryKB: s.MaxPerEntryKB,
	}
}

// GetCacheSettings handles GET /api/v1/cache/settings.
// Any session-authed user may read settings (spec Part B.3); the response
// shape is {global: {...}, per_service: [...]}. Per-service rows are derived
// from service_ai_config.config[.cache] for every service that has an AI
// config row (Task 24 will write these; for v0.4.0 task 4 the list may be
// empty, which is correct).
func (d Deps) GetCacheSettings(w http.ResponseWriter, r *http.Request) {
	resp := cacheSettingsResp{
		PerService: []cachePerServiceSettings{},
	}

	// Global: load the JSON blob from settings[cache.settings] and decode.
	if d.Settings != nil {
		m, err := d.Settings.GetSettings(r.Context())
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "get cache settings failed")
			return
		}
		resp.Global = toCacheSettingsJSON(settingsFromBlob([]byte(m[cacheSettingsKey])))
	} else {
		resp.Global = toCacheSettingsJSON(exact.DefaultSettings)
	}

	// Per-service: iterate every service_ai_config row, extract .cache (or
	// fall back to the global default), set override=true when the per-service
	// block differs from the global default.
	if d.CacheServices != nil {
		rows, err := d.CacheServices.ListAllServiceAIConfigs(r.Context())
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "list service ai configs failed")
			return
		}
		for _, row := range rows {
			perSvc, override := perServiceCacheFromConfig(row.Config)
			resp.PerService = append(resp.PerService, cachePerServiceSettings{
				ServiceID:     row.ServiceID,
				Enabled:       perSvc.Enabled,
				AppliesPer:    perSvc.AppliesPer,
				TTLSeconds:    perSvc.TTLSeconds,
				MaxEntries:    perSvc.MaxEntries,
				MaxPerEntryKB: perSvc.MaxPerEntryKB,
				Override:      override,
			})
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// perServiceCacheFromConfig decodes a service_ai_config.config JSON blob and
// returns the .cache sub-object. ok is true when the blob has a non-empty
// cache block (i.e. the service overrides the global defaults). Falls back to
// exact.DefaultSettings on missing / malformed input.
func perServiceCacheFromConfig(blob []byte) (exact.Settings, bool) {
	if len(blob) == 0 {
		return exact.DefaultSettings, false
	}
	var outer map[string]json.RawMessage
	if err := json.Unmarshal(blob, &outer); err != nil {
		return exact.DefaultSettings, false
	}
	cacheRaw, ok := outer["cache"]
	if !ok || len(cacheRaw) == 0 || string(cacheRaw) == "null" {
		return exact.DefaultSettings, false
	}
	s, err := exact.SettingsFromJSON(cacheRaw)
	if err != nil {
		return exact.DefaultSettings, false
	}
	return s, true
}

// PutCacheSettings handles PUT /api/v1/cache/settings.
// Admin or ai:configure:any only — gating is enforced by the router (this
// runs inside the admin-or-ai-configure group). 400 on unknown applies_per
// (via exact.SettingsFromJSON). The validated blob is persisted as a single
// JSON value under the settings key cacheSettingsKey.
func (d Deps) PutCacheSettings(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	parsed, err := exact.SettingsFromJSON(raw)
	if err != nil {
		// "invalid setting" matches the spec error string (B.3).
		writeErr(w, http.StatusBadRequest, "invalid setting")
		return
	}
	if d.Settings == nil {
		writeErr(w, http.StatusInternalServerError, "settings store unavailable")
		return
	}
	if err := d.Settings.SaveSettings(r.Context(), map[string]string{
		cacheSettingsKey: string(raw),
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "save cache settings failed")
		return
	}
	// Push the new max_entries into the engine so auto-eviction in Store
	// uses the updated cap without a process restart. Type-asserted so the
	// narrow CacheEngine interface (Clear/Stats only) stays unchanged and
	// test fakes don't need to implement the setter.
	if cc, ok := d.CacheEngine.(interface{ SetMaxEntries(int) }); ok {
		cc.SetMaxEntries(parsed.MaxEntries)
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetCacheStats handles GET /api/v1/cache/stats.
// Session-authed (any user may see aggregate stats). hit_rate_24h is derived
// from in-process atomic counters in the engine — they reset on process
// restart (documented limitation; the field name is preserved for wire
// stability).
func (d Deps) GetCacheStats(w http.ResponseWriter, r *http.Request) {
	if d.CacheEngine == nil {
		writeJSON(w, http.StatusOK, cacheStatsResp{})
		return
	}
	entries, bytes, rate, err := d.CacheEngine.Stats(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "cache stats failed")
		return
	}
	writeJSON(w, http.StatusOK, cacheStatsResp{
		Entries:     entries,
		OnDiskBytes: bytes,
		HitRate24h:  rate,
	})
}

// DeleteCacheEntries handles DELETE /api/v1/cache/entries (clear all).
// Admin or ai:configure:any only — gating is enforced by the router (this
// runs inside the admin-or-ai-configure group). 204 on success.
func (d Deps) DeleteCacheEntries(w http.ResponseWriter, r *http.Request) {
	if d.CacheEngine == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := d.CacheEngine.Clear(r.Context(), ""); err != nil {
		writeErr(w, http.StatusInternalServerError, "clear cache failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DeleteServiceCacheEntries handles DELETE /api/v1/services/{serviceID}/cache/entries.
// Permission: service owner OR ai:configure:any (admin) OR ai:configure:own
// (own with ownership check). The clear pattern matches every cache entry
// whose scope_key starts with "endpoint:<service_id>:" (covers per_endpoint
// scope; global/per_api_key clears are not service-scoped).
func (d Deps) DeleteServiceCacheEntries(w http.ResponseWriter, r *http.Request) {
	serviceID := chi.URLParam(r, "serviceID")
	if serviceID == "" {
		writeErr(w, http.StatusBadRequest, "service id is required")
		return
	}
	// Load caller role for the authz check.
	role, err := d.callerRole(r)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	uid := userID(r.Context())

	// :any short-circuits ownership.
	if !authz.Can(role, authz.PermAIConfigureAny) {
		// Must be an :own caller and own the service.
		if !authz.Can(role, authz.PermAIConfigureOwn) {
			writeErr(w, http.StatusForbidden, "forbidden")
			return
		}
		if d.CacheServices == nil {
			writeErr(w, http.StatusInternalServerError, "service lookup unavailable")
			return
		}
		owner, err := d.CacheServices.GetServiceOwner(r.Context(), serviceID)
		if errors.Is(err, db.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "service not found")
			return
		}
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "service lookup failed")
			return
		}
		if owner != uid {
			writeErr(w, http.StatusForbidden, "forbidden")
			return
		}
	} else if d.CacheServices != nil {
		// Even for :any callers, surface 404 cleanly when the service does
		// not exist so the client gets a meaningful error.
		if _, err := d.CacheServices.GetServiceOwner(r.Context(), serviceID); errors.Is(err, db.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "service not found")
			return
		}
	}

	if d.CacheEngine == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	// Clear scope: "endpoint:<service_id>:" matches every per_endpoint scope
	// for this service (the engine's Clear treats this as a LIKE prefix).
	if err := d.CacheEngine.Clear(r.Context(), "endpoint:"+serviceID+":"); err != nil {
		writeErr(w, http.StatusInternalServerError, "clear cache failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// requireAdminOrAIConfigureAny is the middleware applied to PUT /cache/settings
// and DELETE /cache/entries (global). It must run AFTER RequireSession so
// unauthenticated requests get 401 before this 403 check.
//
// Allows: role admin OR role holds PermAIConfigureAny.
func (d Deps) requireAdminOrAIConfigureAny(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uid := userID(r.Context())
		u, err := d.Users.GetUserByID(r.Context(), uid)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				writeErr(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			writeErr(w, http.StatusInternalServerError, "lookup failed")
			return
		}
		if u.Role == "admin" || authz.Can(u.Role, authz.PermAIConfigureAny) {
			next.ServeHTTP(w, r)
			return
		}
		writeErr(w, http.StatusForbidden, "ai:configure:any required")
	})
}
