package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ankoehn/burrow/internal/db"
)

// ModelAliasStore is the narrow CRUD surface the model-alias handlers
// consume. *db.DB satisfies it; tests provide a fake.
type ModelAliasStore interface {
	ListModelAliases(ctx context.Context) ([]db.ModelAlias, error)
	GetModelAlias(ctx context.Context, alias string) (db.ModelAlias, error)
	CreateModelAlias(ctx context.Context, m db.ModelAlias) error
	UpdateModelAlias(ctx context.Context, alias, concreteModel, serviceID string) error
	DeleteModelAlias(ctx context.Context, alias string) error
}

// modelAliasResp is the JSON wire shape for one alias (spec Part C.1).
type modelAliasResp struct {
	Alias         string    `json:"alias"`
	ConcreteModel string    `json:"concrete_model"`
	ServiceID     string    `json:"service_id"`
	CreatedAt     time.Time `json:"created_at"`
}

func toModelAliasResp(m db.ModelAlias) modelAliasResp {
	return modelAliasResp{
		Alias:         m.Alias,
		ConcreteModel: m.ConcreteModel,
		ServiceID:     m.ServiceID,
		CreatedAt:     m.CreatedAt,
	}
}

// modelAliasCreateReq is the POST body shape. The alias is required to be
// 1–128 chars, restricted to ASCII letters/digits/dot/dash/underscore so it
// can appear verbatim in upstream URL paths and in audit logs.
type modelAliasCreateReq struct {
	Alias         string `json:"alias"`
	ConcreteModel string `json:"concrete_model"`
	ServiceID     string `json:"service_id"`
}

// modelAliasUpdateReq is the PUT body shape. The {alias} path param keys
// the row; only the (concrete_model, service_id) pair is mutable.
type modelAliasUpdateReq struct {
	ConcreteModel string `json:"concrete_model"`
	ServiceID     string `json:"service_id"`
}

// validateAlias returns "" on success or a user-visible error string on
// invalid input.
func validateAlias(s string) string {
	if s == "" {
		return "alias is required"
	}
	if len(s) > 128 {
		return "alias too long (max 128 chars)"
	}
	for _, c := range s {
		ok := (c >= 'a' && c <= 'z') ||
			(c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') ||
			c == '.' || c == '-' || c == '_'
		if !ok {
			return "alias must be ASCII letters, digits, '.', '-', or '_'"
		}
	}
	return ""
}

// GetModelAliases handles GET /api/v1/models/aliases.
// Any session-authed user may read the list (spec C.1).
func (d Deps) GetModelAliases(w http.ResponseWriter, r *http.Request) {
	if d.ModelAliases == nil {
		writeJSON(w, http.StatusOK, []modelAliasResp{})
		return
	}
	rows, err := d.ModelAliases.ListModelAliases(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "list model aliases failed")
		return
	}
	out := make([]modelAliasResp, len(rows))
	for i, m := range rows {
		out[i] = toModelAliasResp(m)
	}
	writeJSON(w, http.StatusOK, out)
}

// PostModelAlias handles POST /api/v1/models/aliases.
// Admin OR ai:configure:any (router-applied middleware). Returns 201 with
// the created row; 409 on alias conflict; 400 on validation failure.
func (d Deps) PostModelAlias(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var in modelAliasCreateReq
	if err := json.Unmarshal(raw, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	in.Alias = strings.TrimSpace(in.Alias)
	in.ConcreteModel = strings.TrimSpace(in.ConcreteModel)
	in.ServiceID = strings.TrimSpace(in.ServiceID)
	if msg := validateAlias(in.Alias); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if in.ConcreteModel == "" {
		writeErr(w, http.StatusBadRequest, "concrete_model is required")
		return
	}
	if in.ServiceID == "" {
		writeErr(w, http.StatusBadRequest, "service_id is required")
		return
	}
	if d.ModelAliases == nil {
		writeErr(w, http.StatusInternalServerError, "alias store unavailable")
		return
	}
	row := db.ModelAlias{
		Alias:         in.Alias,
		ConcreteModel: in.ConcreteModel,
		ServiceID:     in.ServiceID,
	}
	if err := d.ModelAliases.CreateModelAlias(r.Context(), row); err != nil {
		if errors.Is(err, db.ErrAliasExists) {
			writeErr(w, http.StatusConflict, "alias already exists")
			return
		}
		writeErr(w, http.StatusInternalServerError, "create model alias failed")
		return
	}
	// Read the row back so created_at reflects the actual DB value.
	created, err := d.ModelAliases.GetModelAlias(r.Context(), in.Alias)
	if err != nil {
		// Row was just created — a missing row here is a server bug, but
		// degrade gracefully with the input echoed back.
		writeJSON(w, http.StatusCreated, toModelAliasResp(row))
		return
	}
	writeJSON(w, http.StatusCreated, toModelAliasResp(created))
}

// PutModelAlias handles PUT /api/v1/models/aliases/{alias}.
// Admin OR ai:configure:any (router-applied middleware). 204 on success;
// 404 when no row matches; 400 on validation failure.
func (d Deps) PutModelAlias(w http.ResponseWriter, r *http.Request) {
	alias := chi.URLParam(r, "alias")
	if msg := validateAlias(alias); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var in modelAliasUpdateReq
	if err := json.Unmarshal(raw, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	in.ConcreteModel = strings.TrimSpace(in.ConcreteModel)
	in.ServiceID = strings.TrimSpace(in.ServiceID)
	if in.ConcreteModel == "" {
		writeErr(w, http.StatusBadRequest, "concrete_model is required")
		return
	}
	if in.ServiceID == "" {
		writeErr(w, http.StatusBadRequest, "service_id is required")
		return
	}
	if d.ModelAliases == nil {
		writeErr(w, http.StatusInternalServerError, "alias store unavailable")
		return
	}
	if err := d.ModelAliases.UpdateModelAlias(r.Context(), alias, in.ConcreteModel, in.ServiceID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "alias not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "update model alias failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DeleteModelAlias handles DELETE /api/v1/models/aliases/{alias}.
// Admin OR ai:configure:any (router-applied middleware). 204 on success;
// 404 when no row matches.
func (d Deps) DeleteModelAlias(w http.ResponseWriter, r *http.Request) {
	alias := chi.URLParam(r, "alias")
	if msg := validateAlias(alias); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if d.ModelAliases == nil {
		writeErr(w, http.StatusInternalServerError, "alias store unavailable")
		return
	}
	if err := d.ModelAliases.DeleteModelAlias(r.Context(), alias); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "alias not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "delete model alias failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
