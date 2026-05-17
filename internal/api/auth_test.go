package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ankoehn/burrow/internal/db"
)

type authUsers struct {
	fakeUsers
	verify  func(email, pw string) (bool, error)
	created string
	deleted string
}

func (a *authUsers) VerifyUserPassword(_ context.Context, e, p string) (bool, error) {
	return a.verify(e, p)
}
func (a *authUsers) GetUserByEmail(_ context.Context, e string) (db.User, error) {
	return db.User{ID: "u1", Email: e, Role: "admin"}, nil
}
func (a *authUsers) CreateSession(_ context.Context, uid, _, _ string) (string, error) {
	a.created = uid
	return "sid-1", nil
}
func (a *authUsers) DeleteSession(_ context.Context, id string) error { a.deleted = id; return nil }
func (a *authUsers) ValidateSession(_ context.Context, id string) (string, error) {
	if id == "sid-1" {
		return "u1", nil
	}
	return "", nil
}

func newTestServer(d Deps) *httptest.Server { return httptest.NewServer(NewRouter(d)) }

func TestLoginSuccessSetsCookie(t *testing.T) {
	au := &authUsers{verify: func(_, _ string) (bool, error) { return true, nil }}
	ts := newTestServer(Deps{Users: au, Log: discardLog()})
	defer ts.Close()
	resp, err := http.Post(ts.URL+"/api/v1/auth/login", "application/json",
		strings.NewReader(`{"email":"a@x","password":"pw"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var got bool
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookieName && c.Value == "sid-1" && c.HttpOnly {
			got = true
		}
	}
	if !got || au.created != "u1" {
		t.Fatalf("cookie/session not set: cookies=%v created=%q", resp.Cookies(), au.created)
	}
}

func TestLoginWrongPassword(t *testing.T) {
	au := &authUsers{verify: func(_, _ string) (bool, error) { return false, nil }}
	ts := newTestServer(Deps{Users: au, Log: discardLog()})
	defer ts.Close()
	resp, err := http.Post(ts.URL+"/api/v1/auth/login", "application/json",
		strings.NewReader(`{"email":"a@x","password":"bad"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", resp.StatusCode)
	}
	if len(resp.Cookies()) != 0 {
		t.Fatal("no cookie on failed login")
	}
}

func TestMeAndLogout(t *testing.T) {
	au := &authUsers{verify: func(_, _ string) (bool, error) { return true, nil }}
	ts := newTestServer(Deps{Users: au, Log: discardLog()})
	defer ts.Close()
	jar, _ := cookiejarNew()
	cl := &http.Client{Jar: jar}
	_, _ = cl.Post(ts.URL+"/api/v1/auth/login", "application/json",
		strings.NewReader(`{"email":"a@x","password":"pw"}`))
	r, err := cl.Get(ts.URL + "/api/v1/me")
	if err != nil || r.StatusCode != http.StatusOK {
		t.Fatalf("me: %v status=%v", err, r.StatusCode)
	}
	r.Body.Close()
	lo, _ := cl.Post(ts.URL+"/api/v1/auth/logout", "application/json", nil)
	lo.Body.Close()
	if lo.StatusCode != http.StatusNoContent || au.deleted != "sid-1" {
		t.Fatalf("logout status=%d deleted=%q", lo.StatusCode, au.deleted)
	}
}
