package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ankoehn/burrow/internal/authz"
	"github.com/ankoehn/burrow/internal/db"
	"github.com/ankoehn/burrow/internal/store"
)

// fakeAutomationStore is an in-memory AutomationStore for handler tests.
// It also satisfies BearerStore so a single fake services both the JSON
// surface and the bearer middleware in one test server.
type fakeAutomationStore struct {
	mu      sync.Mutex
	rows    map[string]store.AutomationTokenView // by id
	hashes  map[string]string                    // sha256-hex → id
	counter int
	// roleOf maps userID → current role, used by the subset check in
	// MintAutomationToken to validate :any callers cannot escalate.
	roleOf map[string]string
}

func newFakeAutomationStore() *fakeAutomationStore {
	return &fakeAutomationStore{
		rows:   map[string]store.AutomationTokenView{},
		hashes: map[string]string{},
		roleOf: map[string]string{},
	}
}

func (f *fakeAutomationStore) MintAutomationToken(
	_ context.Context,
	callerID, callerRole, name string,
	perms []string,
	expiresAt *time.Time,
) (store.AutomationTokenView, string, error) {
	if strings.TrimSpace(name) == "" {
		return store.AutomationTokenView{}, "", store.ErrTokenNameRequired
	}
	// Mirror real store: every requested perm must be in caller's role.
	for _, p := range perms {
		if !authz.Can(callerRole, authz.Permission(p)) {
			return store.AutomationTokenView{}, "", store.ErrPermissionNotInRole
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.counter++
	id := fmt.Sprintf("tok%d", f.counter)
	plaintext := fmt.Sprintf("bua_plain%d", f.counter)
	view := store.AutomationTokenView{
		ID:          id,
		Name:        name,
		Prefix:      "bua_pla",
		UserID:      callerID,
		RoleAtMint:  callerRole,
		Permissions: perms,
		ExpiresAt:   expiresAt,
		CreatedAt:   time.Now().UTC(),
	}
	f.rows[id] = view
	sum := sha256.Sum256([]byte(plaintext))
	f.hashes[hex.EncodeToString(sum[:])] = id
	return view, plaintext, nil
}

func (f *fakeAutomationStore) ListAutomationTokensForCaller(
	_ context.Context,
	callerID, callerRole string,
) ([]store.AutomationTokenView, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	canAny := callerRole == "admin" || authz.Can(callerRole, authz.PermAutomationTokensManageAny)
	canOwn := canAny || authz.Can(callerRole, authz.PermAutomationTokensManageOwn)
	if !canOwn {
		return nil, store.ErrForbidden
	}
	out := make([]store.AutomationTokenView, 0, len(f.rows))
	for _, r := range f.rows {
		if canAny || r.UserID == callerID {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeAutomationStore) RevokeAutomationToken(
	_ context.Context,
	callerID, callerRole, id string,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	canAny := callerRole == "admin" || authz.Can(callerRole, authz.PermAutomationTokensManageAny)
	canOwn := canAny || authz.Can(callerRole, authz.PermAutomationTokensManageOwn)
	if !canOwn {
		return store.ErrForbidden
	}
	row, ok := f.rows[id]
	if !ok {
		return db.ErrNotFound
	}
	if !canAny && row.UserID != callerID {
		return db.ErrNotFound
	}
	delete(f.rows, id)
	for h, rid := range f.hashes {
		if rid == id {
			delete(f.hashes, h)
		}
	}
	return nil
}

// BearerStore satisfaction:

func (f *fakeAutomationStore) LookupBearer(_ context.Context, hash string) (AutomationTokenInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id, ok := f.hashes[hash]
	if !ok {
		return AutomationTokenInfo{}, db.ErrNotFound
	}
	row := f.rows[id]
	return AutomationTokenInfo{
		ID:          row.ID,
		UserID:      row.UserID,
		Permissions: row.Permissions,
		ExpiresAt:   row.ExpiresAt,
	}, nil
}

func (f *fakeAutomationStore) TouchBearer(_ context.Context, _ string) error { return nil }

// --- test server bootstrap -------------------------------------------------

func newAutomationTestServer(t *testing.T, role string) (
	*httptest.Server,
	*authClient,
	*fakeAutomationStore,
	*fakeUserStore,
) {
	t.Helper()
	as := newFakeAutomationStore()
	users := &fakeUserStore{role: role}
	d := Deps{
		Log:        discardLog(),
		Users:      users,
		Automation: as,
		Bearer:     as,
	}
	srv := httptest.NewServer(NewRouter(d))
	t.Cleanup(srv.Close)
	c := authedClient(t, srv)
	return srv, c, as, users
}

// --- happy-path: POST returns plaintext, GET redacts it -------------------

func TestAutomationPostReturnsPlaintextGetRedacts(t *testing.T) {
	_, c, _, _ := newAutomationTestServer(t, "admin")

	body := map[string]any{
		"name":        "ci",
		"permissions": []string{"tunnels:read:any"},
	}
	r := c.post(t, "/api/v1/automation/tokens", body)
	if r.StatusCode != http.StatusCreated {
		t.Fatalf("POST status=%d body=%s", r.StatusCode, readBody(t, r))
	}
	var created createTokenWrap
	if err := json.NewDecoder(r.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if !strings.HasPrefix(created.Plaintext, "bua_") {
		t.Fatalf("plaintext prefix: %q", created.Plaintext)
	}
	if created.Token.ID == "" {
		t.Fatalf("missing token.id: %+v", created.Token)
	}
	if len(created.Token.Permissions) != 1 || created.Token.Permissions[0] != "tunnels:read:any" {
		t.Fatalf("perms round-trip: %+v", created.Token.Permissions)
	}

	// GET must not leak plaintext.
	r2 := c.get(t, "/api/v1/automation/tokens")
	if r2.StatusCode != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", r2.StatusCode, readBody(t, r2))
	}
	body2 := readBody(t, r2)
	if strings.Contains(body2, created.Plaintext) {
		t.Fatalf("GET response leaked plaintext: %s", body2)
	}
	if strings.Contains(body2, `"plaintext"`) {
		t.Fatalf("GET response must omit plaintext key: %s", body2)
	}
	var list []automationTokenResp
	if err := json.Unmarshal([]byte(body2), &list); err != nil {
		t.Fatalf("decode list: %v body=%s", err, body2)
	}
	if len(list) != 1 || list[0].ID != created.Token.ID {
		t.Fatalf("list mismatch: %+v", list)
	}
}

// --- BEARER-AUTHED CALL skips CSRF, cookie-authed still requires it -------

func TestBearerAuthSkipsCSRF(t *testing.T) {
	srv, c, _, _ := newAutomationTestServer(t, "admin")
	// 1. Mint via cookie path so we have a plaintext.
	r := c.post(t, "/api/v1/automation/tokens", map[string]any{
		"name":        "ci",
		"permissions": []string{"tunnels:read:any", "automation:tokens:manage:any"},
	})
	if r.StatusCode != http.StatusCreated {
		t.Fatalf("mint status=%d body=%s", r.StatusCode, readBody(t, r))
	}
	var created createTokenWrap
	json.NewDecoder(r.Body).Decode(&created)
	r.Body.Close()

	// 2. Bearer GET /tunnels with NO cookies + NO X-CSRF-Token → 200.
	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/tunnels", nil)
	req.Header.Set("Authorization", "Bearer "+created.Plaintext)
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bearer GET /tunnels (no csrf, no cookie): want 200 got %d", resp.StatusCode)
	}

	// 3. Bearer DELETE (mutating) with NO X-CSRF-Token → 204 (skips CSRF).
	// We delete a token we just minted to prove a state-changing call also
	// succeeds without the double-submit header.
	r2 := c.post(t, "/api/v1/automation/tokens", map[string]any{
		"name":        "ephemeral",
		"permissions": []string{"tunnels:read:any"},
	})
	var ephemeral createTokenWrap
	json.NewDecoder(r2.Body).Decode(&ephemeral)
	r2.Body.Close()

	delReq, _ := http.NewRequest("DELETE",
		srv.URL+"/api/v1/automation/tokens/"+ephemeral.Token.ID, nil)
	delReq.Header.Set("Authorization", "Bearer "+created.Plaintext)
	delResp, err := (&http.Client{}).Do(delReq)
	if err != nil {
		t.Fatal(err)
	}
	delResp.Body.Close()
	if delResp.StatusCode != http.StatusNoContent {
		t.Fatalf("bearer DELETE without X-CSRF-Token: want 204 got %d", delResp.StatusCode)
	}

	// 4. Cookie-authed mutating call with the session cookie but withOUT the
	// X-CSRF-Token header → 403. This proves the CSRF bypass is gated on the
	// presence of a bearer token, not just "GET vs mutating".
	jar := c.hc.Jar // session cookie still good
	rawDelReq, _ := http.NewRequest("DELETE",
		srv.URL+"/api/v1/automation/tokens/nonexistent", nil)
	rawClient := &http.Client{Jar: jar}
	rawDelResp, err := rawClient.Do(rawDelReq)
	if err != nil {
		t.Fatal(err)
	}
	rawDelResp.Body.Close()
	if rawDelResp.StatusCode != http.StatusForbidden {
		t.Fatalf("cookie-authed DELETE without X-CSRF-Token: want 403 got %d", rawDelResp.StatusCode)
	}
}

// --- limited-perm token cannot hit admin-gated endpoint -------------------

func TestBearerWithOwnPermsCannotReachAdminEndpoint(t *testing.T) {
	srv, c, _, _ := newAutomationTestServer(t, "user")
	// Mint a token whose only permission is tunnels:read:own.
	r := c.post(t, "/api/v1/automation/tokens", map[string]any{
		"name":        "limited",
		"permissions": []string{"tunnels:read:own"},
	})
	if r.StatusCode != http.StatusCreated {
		t.Fatalf("mint status=%d body=%s", r.StatusCode, readBody(t, r))
	}
	var created createTokenWrap
	json.NewDecoder(r.Body).Decode(&created)
	r.Body.Close()

	// Hit admin-only GET /api/v1/users with bearer.
	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/users", nil)
	req.Header.Set("Authorization", "Bearer "+created.Plaintext)
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("bearer-with-own-only on /users: want 403 got %d body=%s",
			resp.StatusCode, readBody(t, resp))
	}
}

// --- demoting minter narrows token reach immediately ----------------------

func TestBearerRoleDemotionRevokesAnyImmediately(t *testing.T) {
	srv, c, _, users := newAutomationTestServer(t, "admin")
	// Mint an :any token while caller is admin.
	r := c.post(t, "/api/v1/automation/tokens", map[string]any{
		"name":        "ci-any",
		"permissions": []string{"tunnels:read:any"},
	})
	if r.StatusCode != http.StatusCreated {
		t.Fatalf("mint status=%d body=%s", r.StatusCode, readBody(t, r))
	}
	var created createTokenWrap
	json.NewDecoder(r.Body).Decode(&created)
	r.Body.Close()

	// Demote the user to "user" — the new role no longer grants :any.
	users.role = "user"

	// The bearer should now fail to reach GET /api/v1/users (admin-only).
	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/users", nil)
	req.Header.Set("Authorization", "Bearer "+created.Plaintext)
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("post-demotion admin endpoint: want 403 got %d", resp.StatusCode)
	}
}

// --- POST escalation attempt rejected --------------------------------------

func TestPostAutomationTokenEscalationRejected(t *testing.T) {
	_, c, _, _ := newAutomationTestServer(t, "user")
	r := c.post(t, "/api/v1/automation/tokens", map[string]any{
		"name":        "ci",
		"permissions": []string{"users:manage"}, // not granted by "user"
	})
	if r.StatusCode != http.StatusForbidden {
		t.Fatalf("escalation: want 403 got %d body=%s", r.StatusCode, readBody(t, r))
	}
	r.Body.Close()
}

// --- DELETE revokes; bearer token becomes invalid ----------------------------

func TestDeleteAutomationTokenRevokesBearerAuth(t *testing.T) {
	srv, c, _, _ := newAutomationTestServer(t, "admin")
	r := c.post(t, "/api/v1/automation/tokens", map[string]any{
		"name":        "ci",
		"permissions": []string{"tunnels:read:any"},
	})
	if r.StatusCode != http.StatusCreated {
		t.Fatalf("mint: %d %s", r.StatusCode, readBody(t, r))
	}
	var created createTokenWrap
	json.NewDecoder(r.Body).Decode(&created)
	r.Body.Close()

	// Revoke.
	rev := c.delete(t, "/api/v1/automation/tokens/"+created.Token.ID)
	if rev.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: want 204 got %d body=%s", rev.StatusCode, readBody(t, rev))
	}
	rev.Body.Close()

	// Bearer call now 401.
	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/tunnels", nil)
	req.Header.Set("Authorization", "Bearer "+created.Plaintext)
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("post-revoke: want 401 got %d", resp.StatusCode)
	}
}

// --- expiry path through the full middleware chain ------------------------

func TestBearerExpiredRejected(t *testing.T) {
	srv, c, as, _ := newAutomationTestServer(t, "admin")
	// Mint via the API, then back-date expires_at on the underlying fake row.
	r := c.post(t, "/api/v1/automation/tokens", map[string]any{
		"name":        "exp",
		"permissions": []string{"tunnels:read:any"},
	})
	if r.StatusCode != http.StatusCreated {
		t.Fatalf("mint: %d", r.StatusCode)
	}
	var created createTokenWrap
	json.NewDecoder(r.Body).Decode(&created)
	r.Body.Close()

	// Mutate the fake row to be expired.
	past := time.Now().UTC().Add(-time.Hour)
	as.mu.Lock()
	row := as.rows[created.Token.ID]
	row.ExpiresAt = &past
	as.rows[created.Token.ID] = row
	as.mu.Unlock()

	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/tunnels", nil)
	req.Header.Set("Authorization", "Bearer "+created.Plaintext)
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expired: want 401 got %d body=%s", resp.StatusCode, readBody(t, resp))
	}
}

// --- Deps.Automation nil makes the surface 500 cleanly -------------------

func TestAutomationStoreUnavailable500(t *testing.T) {
	users := &fakeUserStore{role: "admin"}
	d := Deps{
		Log:   discardLog(),
		Users: users,
		// Automation + Bearer intentionally nil.
	}
	srv := httptest.NewServer(NewRouter(d))
	defer srv.Close()
	c := authedClient(t, srv)

	r := c.get(t, "/api/v1/automation/tokens")
	if r.StatusCode != http.StatusInternalServerError {
		t.Fatalf("nil store: want 500 got %d body=%s", r.StatusCode, readBody(t, r))
	}
	r.Body.Close()
}

// Compile-time guard: ensure the automation fake actually satisfies the
// AutomationStore interface (catches signature drift loudly).
var _ AutomationStore = (*fakeAutomationStore)(nil)
var _ BearerStore = (*fakeAutomationStore)(nil)
