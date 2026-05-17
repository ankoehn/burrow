package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ankoehn/burrow/internal/db"
	"github.com/ankoehn/burrow/internal/store"
)

type fakeUsers struct {
	validate func(id string) (string, error)
}

func (f fakeUsers) VerifyUserPassword(_ context.Context, _, _ string) (bool, error) {
	return false, nil
}
func (f fakeUsers) GetUserByEmail(_ context.Context, _ string) (db.User, error) {
	return db.User{}, db.ErrNotFound
}
func (f fakeUsers) GetUserByID(_ context.Context, id string) (db.User, error) {
	return db.User{ID: id, Email: "a@x", Role: "admin"}, nil
}
func (f fakeUsers) IssueClientToken(_ context.Context, _, _ string) (string, error) {
	return "bur_x", nil
}
func (f fakeUsers) ListClientTokens(_ context.Context, _ string) ([]db.ClientToken, error) {
	return nil, nil
}
func (f fakeUsers) RevokeClientToken(_ context.Context, _, _ string) error { return nil }
func (f fakeUsers) CreateSession(_ context.Context, _, _, _ string) (string, error) {
	return "sid", nil
}
func (f fakeUsers) ValidateSession(_ context.Context, id string) (string, error) {
	return f.validate(id)
}
func (f fakeUsers) DeleteSession(_ context.Context, _ string) error { return nil }

func testDeps(v func(string) (string, error)) Deps {
	return Deps{Users: fakeUsers{validate: v}, Log: slog.Default()}
}

func TestRequireSessionNoCookie(t *testing.T) {
	d := testDeps(func(string) (string, error) { return "", store.ErrUnauthorized })
	h := d.RequireSession(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/x", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rr.Code)
	}
}

func TestRequireSessionValid(t *testing.T) {
	d := testDeps(func(id string) (string, error) {
		if id == "good" {
			return "u1", nil
		}
		return "", store.ErrUnauthorized
	})
	var seen string
	h := d.RequireSession(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = userID(r.Context())
	}))
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/x", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "good"})
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || seen != "u1" {
		t.Fatalf("want 200+u1, got %d / %q", rr.Code, seen)
	}
}

func TestRequireSessionInvalidCookie(t *testing.T) {
	d := testDeps(func(string) (string, error) { return "", store.ErrUnauthorized })
	h := d.RequireSession(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/x", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "bad"})
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rr.Code)
	}
}

func TestRequireSessionStoreError(t *testing.T) {
	d := testDeps(func(string) (string, error) { return "", errors.New("db down") })
	h := d.RequireSession(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/x", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "x"})
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", rr.Code)
	}
}

func TestRequireSessionEmptyCookieValue(t *testing.T) {
	d := testDeps(func(string) (string, error) { return "", store.ErrUnauthorized })
	h := d.RequireSession(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/x", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: ""})
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 for present-but-empty cookie, got %d", rr.Code)
	}
}
