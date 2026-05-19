package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ankoehn/burrow/internal/db"
)

func TestAdminUserListSurfacesStatusAndLastLogin(t *testing.T) {
	ll := time.Now().UTC()
	us := &fakeUserStore{
		role: "admin",
		page: []db.User{
			{ID: "u1", Email: "a@b.c", Role: "admin", Status: "active", LastLogin: &ll, CreatedAt: ll},
			{ID: "u2", Email: "z@b.c", Role: "user", Status: "suspended", CreatedAt: ll},
		},
		total: 2,
	}
	d := Deps{Log: discardLog(), Users: us}
	srv := httptest.NewServer(NewRouter(d))
	defer srv.Close()
	c := authedClient(t, srv)

	r := c.get(t, "/api/v1/users?q=&limit=10&offset=0")
	if r.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", r.StatusCode)
	}
	var resp struct {
		Users []userAdminResp `json:"users"`
		Total int             `json:"total"`
	}
	json.NewDecoder(r.Body).Decode(&resp)
	r.Body.Close()
	if resp.Total != 2 || len(resp.Users) != 2 {
		t.Fatalf("paged resp: %+v", resp)
	}
	if resp.Users[0].Status != "active" || resp.Users[0].LastLogin == nil {
		t.Fatalf("status/last_login not surfaced: %+v", resp.Users[0])
	}
	if resp.Users[1].Status != "suspended" || resp.Users[1].LastLogin != nil {
		t.Fatalf("u2: %+v", resp.Users[1])
	}
}

func TestAdminUpdateUser(t *testing.T) {
	us := &fakeUserStore{role: "admin", selfID: "u-self"}
	d := Deps{Log: discardLog(), Users: us}
	srv := httptest.NewServer(NewRouter(d))
	defer srv.Close()
	c := authedClient(t, srv)

	r := c.patch(t, "/api/v1/users/u2", map[string]string{"role": "admin", "status": "suspended"})
	if r.StatusCode != http.StatusNoContent {
		t.Fatalf("patch status=%d body=%s", r.StatusCode, readBody(t, r))
	}
	r.Body.Close()
	if us.lastRole != "admin" || us.lastStatus != "suspended" {
		t.Fatalf("update not applied: role=%q status=%q", us.lastRole, us.lastStatus)
	}

	// invalid status -> 400
	r = c.patch(t, "/api/v1/users/u2", map[string]string{"status": "bogus"})
	if r.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid status -> %d want 400", r.StatusCode)
	}
	r.Body.Close()

	// self status-change guard: cannot suspend yourself
	r = c.patch(t, "/api/v1/users/u-self", map[string]string{"status": "suspended"})
	if r.StatusCode != http.StatusBadRequest {
		t.Fatalf("self-suspend -> %d want 400", r.StatusCode)
	}
	r.Body.Close()
}

func TestLoginStampsLastLoginAndBlocksSuspended(t *testing.T) {
	us := &fakeUserStore{role: "user", loginEmail: "a@b.c", loginPass: "password1"}
	d := Deps{Log: discardLog(), Users: us}
	srv := httptest.NewServer(NewRouter(d))
	defer srv.Close()

	// happy login stamps last_login
	r, _ := http.Post(srv.URL+"/api/v1/auth/login", "application/json",
		mustJSON(map[string]string{"email": "a@b.c", "password": "password1"}))
	if r.StatusCode != http.StatusOK {
		t.Fatalf("login status=%d", r.StatusCode)
	}
	r.Body.Close()
	if !us.lastLoginTouched {
		t.Fatal("login must call TouchUserLastLogin")
	}

	// suspended user blocked at login with 403
	us.suspended = true
	r, _ = http.Post(srv.URL+"/api/v1/auth/login", "application/json",
		mustJSON(map[string]string{"email": "a@b.c", "password": "password1"}))
	if r.StatusCode != http.StatusForbidden {
		t.Fatalf("suspended login status=%d want 403", r.StatusCode)
	}
	r.Body.Close()
}
