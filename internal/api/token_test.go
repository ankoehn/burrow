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
	issued     string
	issuedUID  string
	tokens     []db.ClientToken
	listedUID  string
	revoked    string
	revokedUID string
	revErr     error
}

func (u *tokUsers) IssueClientToken(_ context.Context, uid, name string) (string, error) {
	u.issued = name
	u.issuedUID = uid
	return "bur_PLAINTEXT", nil
}
func (u *tokUsers) ListClientTokens(_ context.Context, uid string) ([]db.ClientToken, error) {
	u.listedUID = uid
	return u.tokens, nil
}
func (u *tokUsers) RevokeClientToken(_ context.Context, id, uid string) error {
	u.revokedUID = uid
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
	if u.issuedUID != "u1" {
		t.Fatalf("IssueClientToken must receive authenticated userID, got %q", u.issuedUID)
	}

	lr, _ := cl.Get(ts.URL + "/api/v1/tokens")
	lb := readBody(t, lr)
	if lr.StatusCode != http.StatusOK || !strings.Contains(lb, `"id":"t1"`) || !strings.Contains(lb, `"name":"laptop"`) {
		t.Fatalf("list status=%d body=%s", lr.StatusCode, lb)
	}
	if strings.Contains(lb, "SECRETHASH") || strings.Contains(lb, "token_hash") {
		t.Fatalf("list MUST NOT leak token hash: %s", lb)
	}
	if u.listedUID != "u1" {
		t.Fatalf("ListClientTokens must receive authenticated userID, got %q", u.listedUID)
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
	if u.revokedUID != "u1" {
		t.Fatalf("RevokeClientToken must receive authenticated userID, got %q", u.revokedUID)
	}

	u.revErr = db.ErrNotFound
	req2, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/tokens/missing", nil)
	dr2, _ := cl.Do(req2)
	dr2.Body.Close()
	if dr2.StatusCode != http.StatusNotFound {
		t.Fatalf("revoke unknown want 404, got %d", dr2.StatusCode)
	}
}

func TestCreateTokenEmptyName(t *testing.T) {
	u := &tokUsers{}
	u.verify = func(_, _ string) (bool, error) { return true, nil }
	ts := newTestServer(Deps{Users: u, Log: discardLog()})
	defer ts.Close()
	cl := loggedInClient(t, &httptestServer{ts})
	r, err := cl.Post(ts.URL+"/api/v1/tokens", "application/json", strings.NewReader(`{"name":""}`))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty name want 400, got %d", r.StatusCode)
	}
	if u.issued != "" {
		t.Fatalf("must not issue a token for empty name (issued=%q)", u.issued)
	}
}

func TestListTokensEmptyIsJSONArray(t *testing.T) {
	u := &tokUsers{tokens: nil}
	u.verify = func(_, _ string) (bool, error) { return true, nil }
	ts := newTestServer(Deps{Users: u, Log: discardLog()})
	defer ts.Close()
	cl := loggedInClient(t, &httptestServer{ts})
	r, _ := cl.Get(ts.URL + "/api/v1/tokens")
	b := strings.TrimSpace(readBody(t, r))
	if r.StatusCode != http.StatusOK || b != "[]" {
		t.Fatalf("empty token list must be JSON [] (status=%d body=%q)", r.StatusCode, b)
	}
}
