package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ankoehn/burrow/internal/db"
)

type roleResp struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

type roleDetailResp struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	Permissions []string  `json:"permissions"`
}

// ListRoles returns the built-in roles (admin only). GET /api/v1/roles
func (d Deps) ListRoles(w http.ResponseWriter, r *http.Request) {
	rs, err := d.Roles.ListRoles(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "list roles failed")
		return
	}
	out := make([]roleResp, 0, len(rs))
	for _, x := range rs {
		out = append(out, roleResp{Name: x.Name, Description: x.Description, CreatedAt: x.CreatedAt})
	}
	writeJSON(w, http.StatusOK, out)
}

// GetRole returns a role + its code-defined permissions. GET /api/v1/roles/{name}
func (d Deps) GetRole(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	rd, err := d.Roles.GetRole(r.Context(), name)
	if errors.Is(err, db.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "role not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "get role failed")
		return
	}
	perms := rd.Permissions
	if perms == nil {
		perms = []string{}
	}
	writeJSON(w, http.StatusOK, roleDetailResp{
		Name: rd.Name, Description: rd.Description, CreatedAt: rd.CreatedAt, Permissions: perms,
	})
}
