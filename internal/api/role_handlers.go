package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ankoehn/burrow/internal/db"
)

// roleResp is the list-item wire shape. v0.4.0 extends the v0.3.0 shape with
// builtin + default_for_new_users so the UI can flag built-in rows
// read-only and surface the "default for new users" badge inline.
type roleResp struct {
	Name               string    `json:"name"`
	Description        string    `json:"description"`
	CreatedAt          time.Time `json:"created_at"`
	Builtin            bool      `json:"builtin"`
	DefaultForNewUsers bool      `json:"default_for_new_users"`
}

// roleDetailResp is the single-role wire shape. v0.4.0 extends with builtin,
// permissions, and default_for_new_users — the same fields the editor PUT
// accepts. The permissions slice is always non-nil (encoded as []) so the
// UI doesn't have to special-case null.
type roleDetailResp struct {
	Name               string    `json:"name"`
	Description        string    `json:"description"`
	CreatedAt          time.Time `json:"created_at"`
	Permissions        []string  `json:"permissions"`
	Builtin            bool      `json:"builtin"`
	DefaultForNewUsers bool      `json:"default_for_new_users"`
}

// ListRoles returns every role row (builtin + custom). GET /api/v1/roles
func (d Deps) ListRoles(w http.ResponseWriter, r *http.Request) {
	rs, err := d.Roles.ListRoles(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "list roles failed")
		return
	}
	out := make([]roleResp, 0, len(rs))
	for _, x := range rs {
		out = append(out, roleResp{
			Name:               x.Name,
			Description:        x.Description,
			CreatedAt:          x.CreatedAt,
			Builtin:            x.Builtin,
			DefaultForNewUsers: x.DefaultForNewUsers,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// GetRole returns a role + its effective permissions (the code-defined set
// for builtins, the DB-stored set for custom roles). GET /api/v1/roles/{name}
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
		Name:               rd.Name,
		Description:        rd.Description,
		CreatedAt:          rd.CreatedAt,
		Permissions:        perms,
		Builtin:            rd.Builtin,
		DefaultForNewUsers: rd.DefaultForNewUsers,
	})
}
