package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ankoehn/burrow/internal/db"
	"github.com/ankoehn/burrow/internal/store"
)

type fakeRoles struct{}

func (fakeRoles) ListRoles(context.Context) ([]db.Role, error) {
	return []db.Role{{Name: "admin", Description: "all"}, {Name: "user", Description: "own"}}, nil
}
func (fakeRoles) GetRole(_ context.Context, name string) (store.RoleDetail, error) {
	if name != "admin" {
		return store.RoleDetail{}, db.ErrNotFound
	}
	return store.RoleDetail{Name: "admin", Description: "all", Permissions: []string{"users:manage"}, Builtin: true}, nil
}

// v0.4.0 Task 15 stubs — this fake is read-only; the existing assertions in
// TestRolesEndpoints only exercise the GET surface. Mutation tests use the
// full-featured fakeRoleStore from role_admin_test.go.
func (fakeRoles) CreateRole(context.Context, string, string, []string, bool) error {
	return store.ErrRoleBuiltin
}
func (fakeRoles) UpdateRole(context.Context, string, store.RoleUpdate) error {
	return store.ErrRoleBuiltin
}
func (fakeRoles) DeleteRole(context.Context, string) ([]string, error) {
	return nil, store.ErrRoleBuiltin
}

func TestRolesEndpoints(t *testing.T) {
	d := Deps{Log: discardLog(), Roles: fakeRoles{}, Users: &fakeUserStore{role: "admin"}}
	srv := httptest.NewServer(NewRouter(d))
	defer srv.Close()
	c := authedClient(t, srv)

	// list
	r := c.get(t, "/api/v1/roles")
	if r.StatusCode != http.StatusOK {
		t.Fatalf("list roles status=%d", r.StatusCode)
	}
	var list []roleResp
	json.NewDecoder(r.Body).Decode(&list)
	r.Body.Close()
	if len(list) != 2 {
		t.Fatalf("want 2 roles, got %d", len(list))
	}

	// detail with permissions
	r = c.get(t, "/api/v1/roles/admin")
	if r.StatusCode != http.StatusOK {
		t.Fatalf("role detail status=%d", r.StatusCode)
	}
	var rd roleDetailResp
	json.NewDecoder(r.Body).Decode(&rd)
	r.Body.Close()
	if rd.Name != "admin" || len(rd.Permissions) == 0 {
		t.Fatalf("role detail: %+v", rd)
	}

	// unknown -> 404
	r = c.get(t, "/api/v1/roles/ghost")
	if r.StatusCode != http.StatusNotFound {
		t.Fatalf("ghost role status=%d want 404", r.StatusCode)
	}
	r.Body.Close()
}
