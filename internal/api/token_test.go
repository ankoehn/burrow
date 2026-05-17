package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ankoehn/burrow/internal/db"
)

type tokUsers struct {
	authUsers
	issued  string
	tokens  []db.ClientToken
	revoked string
	revErr  error
}

func (u *tokUsers) IssueClientToken(_ context.Context, _, name string) (string, error) {
	u.issued = name
	return "bur_PLAINTEXT", nil
}
func (u *tokUsers) ListClientTokens(_ context.Context, _ string) ([]db.ClientToken, error) {
	return u.tokens, nil
}
func (u *tokUsers) RevokeClientToken(_ context.Context, id, _ string) error {
	if u.revErr != nil {
		return u.revErr
	}
	u.revoked = id
	return nil
}

func loggedInClient(t *testing.T, ts *httptestServer) *http.Client {
	t.Helper()
	jar, _ := cookiejarNew()
	cl := &http.Client{Jar: jar}
	r, _ := cl.Post(ts.URL+"/api/v1/auth/login", "application/json",
		strings.NewReader(`{"email":"a@x","password":"pw"}`))
	r.Body.Close()
	return cl
}

func TestCreateAndListTokens(t *testing.T) {
	now := time.Now()
	u := &tokUsers{tokens: []db.ClientToken{{ID: "t1", UserID: "u1", Name: "laptop", TokenHash: "SECRETHASH", CreatedAt: now}}}
	u.verify = func(_, _ string) (bool, error) { return true, nil }
	ts := newTestServer(Deps{Users: u, Log: discardLog()})
	defer ts.Close()
	cl := loggedInClient(t, &httptestServer{ts})

	cr, err := cl.Post(ts.URL+"/api/v1/tokens", "application/json", strings.NewReader(`{"name":"laptop"}`))
	if err != nil || cr.StatusCode != http.StatusCreated {
		t.Fatalf("create: %v status=%v", err, cr.StatusCode)
	}
	body := readBody(t, cr)
	if !strings.Contains(body, `"token":"bur_PLAINTEXT"`) || !strings.Contains(body, `"name":"laptop"`) {
		t.Fatalf("create body missing plaintext/name: %s", body)
	}
	if u.issued != "laptop" {
		t.Fatalf("issued name = %q", u.issued)
	}

	lr, _ := cl.Get(ts.URL + "/api/v1/tokens")
	lb := readBody(t, lr)
	if lr.StatusCode != http.StatusOK || !strings.Contains(lb, `"id":"t1"`) || !strings.Contains(lb, `"name":"laptop"`) {
		t.Fatalf("list status=%d body=%s", lr.StatusCode, lb)
	}
	if strings.Contains(lb, "SECRETHASH") || strings.Contains(lb, "token_hash") {
		t.Fatalf("list MUST NOT leak token hash: %s", lb)
	}
}

func TestRevokeToken(t *testing.T) {
	u := &tokUsers{}
	u.verify = func(_, _ string) (bool, error) { return true, nil }
	ts := newTestServer(Deps{Users: u, Log: discardLog()})
	defer ts.Close()
	cl := loggedInClient(t, &httptestServer{ts})

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/tokens/t1", nil)
	dr, _ := cl.Do(req)
	dr.Body.Close()
	if dr.StatusCode != http.StatusNoContent || u.revoked != "t1" {
		t.Fatalf("revoke status=%d revoked=%q", dr.StatusCode, u.revoked)
	}

	u.revErr = db.ErrNotFound
	req2, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/tokens/missing", nil)
	dr2, _ := cl.Do(req2)
	dr2.Body.Close()
	if dr2.StatusCode != http.StatusNotFound {
		t.Fatalf("revoke unknown want 404, got %d", dr2.StatusCode)
	}
}
