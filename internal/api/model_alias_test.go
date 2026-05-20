package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/ankoehn/burrow/internal/db"
)

// fakeModelAliasStore is an in-memory ModelAliasStore for the handler tests.
type fakeModelAliasStore struct {
	mu        sync.Mutex
	rows      map[string]db.ModelAlias
	createErr error
}

func newFakeModelAliasStore() *fakeModelAliasStore {
	return &fakeModelAliasStore{rows: map[string]db.ModelAlias{}}
}

func (f *fakeModelAliasStore) ListModelAliases(_ context.Context) ([]db.ModelAlias, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]db.ModelAlias, 0, len(f.rows))
	for _, m := range f.rows {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Alias < out[j].Alias })
	return out, nil
}

func (f *fakeModelAliasStore) GetModelAlias(_ context.Context, alias string) (db.ModelAlias, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.rows[alias]
	if !ok {
		return db.ModelAlias{}, db.ErrNotFound
	}
	return m, nil
}

func (f *fakeModelAliasStore) CreateModelAlias(_ context.Context, m db.ModelAlias) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return f.createErr
	}
	if _, exists := f.rows[m.Alias]; exists {
		return db.ErrAliasExists
	}
	m.CreatedAt = time.Now().UTC()
	f.rows[m.Alias] = m
	return nil
}

func (f *fakeModelAliasStore) UpdateModelAlias(_ context.Context, alias, concrete, svc string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.rows[alias]
	if !ok {
		return db.ErrNotFound
	}
	m.ConcreteModel = concrete
	m.ServiceID = svc
	f.rows[alias] = m
	return nil
}

func (f *fakeModelAliasStore) DeleteModelAlias(_ context.Context, alias string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.rows[alias]; !ok {
		return db.ErrNotFound
	}
	delete(f.rows, alias)
	return nil
}

// TestModelAliasGet_Empty asserts GET returns an empty (non-null) JSON
// array when no aliases exist.
func TestModelAliasGet_Empty(t *testing.T) {
	d := Deps{
		Log:          discardLog(),
		Users:        &fakeUserStore{role: "user"},
		ModelAliases: newFakeModelAliasStore(),
	}
	srv := httptest.NewServer(NewRouter(d))
	defer srv.Close()
	c := authedClient(t, srv)
	r := c.get(t, "/api/v1/models/aliases")
	if r.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", r.StatusCode, readBody(t, r))
	}
	var out []modelAliasResp
	if err := json.NewDecoder(r.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if out == nil || len(out) != 0 {
		t.Fatalf("want empty non-nil array; got %v", out)
	}
}

// TestModelAliasPost_OK asserts the happy path: a valid POST round-trips
// into the GET list, and the response includes created_at.
func TestModelAliasPost_OK(t *testing.T) {
	d := Deps{
		Log:          discardLog(),
		Users:        &fakeUserStore{role: "admin"},
		ModelAliases: newFakeModelAliasStore(),
	}
	srv := httptest.NewServer(NewRouter(d))
	defer srv.Close()
	c := authedClient(t, srv)

	body := map[string]string{
		"alias": "fast", "concrete_model": "gpt-4o-mini", "service_id": "svc-1",
	}
	r := c.post(t, "/api/v1/models/aliases", body)
	if r.StatusCode != http.StatusCreated {
		t.Fatalf("POST status=%d body=%s", r.StatusCode, readBody(t, r))
	}
	var got modelAliasResp
	if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if got.Alias != "fast" || got.ConcreteModel != "gpt-4o-mini" || got.ServiceID != "svc-1" {
		t.Fatalf("got %+v", got)
	}
	if got.CreatedAt.IsZero() {
		t.Errorf("created_at zero")
	}

	// GET should list it.
	r = c.get(t, "/api/v1/models/aliases")
	if r.StatusCode != http.StatusOK {
		t.Fatalf("list status=%d", r.StatusCode)
	}
	var list []modelAliasResp
	_ = json.NewDecoder(r.Body).Decode(&list)
	r.Body.Close()
	if len(list) != 1 || list[0].Alias != "fast" {
		t.Fatalf("list = %+v", list)
	}
}

// TestModelAliasPost_BadInputs covers the 400 paths.
func TestModelAliasPost_BadInputs(t *testing.T) {
	d := Deps{
		Log:          discardLog(),
		Users:        &fakeUserStore{role: "admin"},
		ModelAliases: newFakeModelAliasStore(),
	}
	srv := httptest.NewServer(NewRouter(d))
	defer srv.Close()
	c := authedClient(t, srv)

	cases := []struct {
		name string
		body map[string]string
	}{
		{"empty alias", map[string]string{}},
		{"bad chars", map[string]string{"alias": "bad name", "concrete_model": "m", "service_id": "s"}},
		{"no concrete", map[string]string{"alias": "ok", "concrete_model": "", "service_id": "s"}},
		{"no service", map[string]string{"alias": "ok", "concrete_model": "m", "service_id": ""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := c.post(t, "/api/v1/models/aliases", tc.body)
			if r.StatusCode != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", r.StatusCode, readBody(t, r))
			}
		})
	}
}

// TestModelAliasPost_Conflict — duplicate alias returns 409.
func TestModelAliasPost_Conflict(t *testing.T) {
	store := newFakeModelAliasStore()
	_ = store.CreateModelAlias(context.Background(), db.ModelAlias{
		Alias: "dup", ConcreteModel: "m", ServiceID: "s",
	})
	d := Deps{
		Log:          discardLog(),
		Users:        &fakeUserStore{role: "admin"},
		ModelAliases: store,
	}
	srv := httptest.NewServer(NewRouter(d))
	defer srv.Close()
	c := authedClient(t, srv)
	r := c.post(t, "/api/v1/models/aliases", map[string]string{
		"alias": "dup", "concrete_model": "x", "service_id": "y",
	})
	if r.StatusCode != http.StatusConflict {
		t.Fatalf("status=%d body=%s", r.StatusCode, readBody(t, r))
	}
}

// TestModelAliasPut_OK — successful update returns 204 and changes the row.
func TestModelAliasPut_OK(t *testing.T) {
	store := newFakeModelAliasStore()
	_ = store.CreateModelAlias(context.Background(), db.ModelAlias{
		Alias: "a", ConcreteModel: "m", ServiceID: "s1",
	})
	d := Deps{
		Log:          discardLog(),
		Users:        &fakeUserStore{role: "admin"},
		ModelAliases: store,
	}
	srv := httptest.NewServer(NewRouter(d))
	defer srv.Close()
	c := authedClient(t, srv)
	r := c.put(t, "/api/v1/models/aliases/a", map[string]string{
		"concrete_model": "m2", "service_id": "s2",
	})
	if r.StatusCode != http.StatusNoContent {
		t.Fatalf("PUT status=%d body=%s", r.StatusCode, readBody(t, r))
	}
	row, _ := store.GetModelAlias(context.Background(), "a")
	if row.ConcreteModel != "m2" || row.ServiceID != "s2" {
		t.Fatalf("update did not persist: %+v", row)
	}
}

// TestModelAliasPut_NotFound — PUT on missing alias returns 404.
func TestModelAliasPut_NotFound(t *testing.T) {
	d := Deps{
		Log:          discardLog(),
		Users:        &fakeUserStore{role: "admin"},
		ModelAliases: newFakeModelAliasStore(),
	}
	srv := httptest.NewServer(NewRouter(d))
	defer srv.Close()
	c := authedClient(t, srv)
	r := c.put(t, "/api/v1/models/aliases/nope", map[string]string{
		"concrete_model": "m", "service_id": "s",
	})
	if r.StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", r.StatusCode, readBody(t, r))
	}
}

// TestModelAliasDelete_OK — DELETE removes the row and returns 204.
func TestModelAliasDelete_OK(t *testing.T) {
	store := newFakeModelAliasStore()
	_ = store.CreateModelAlias(context.Background(), db.ModelAlias{
		Alias: "byebye", ConcreteModel: "m", ServiceID: "s",
	})
	d := Deps{
		Log:          discardLog(),
		Users:        &fakeUserStore{role: "admin"},
		ModelAliases: store,
	}
	srv := httptest.NewServer(NewRouter(d))
	defer srv.Close()
	c := authedClient(t, srv)
	r := c.delete(t, "/api/v1/models/aliases/byebye")
	if r.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE status=%d body=%s", r.StatusCode, readBody(t, r))
	}
	if _, err := store.GetModelAlias(context.Background(), "byebye"); err == nil {
		t.Fatalf("row should be gone")
	}
}

// TestModelAliasDelete_NotFound — DELETE on missing alias returns 404.
func TestModelAliasDelete_NotFound(t *testing.T) {
	d := Deps{
		Log:          discardLog(),
		Users:        &fakeUserStore{role: "admin"},
		ModelAliases: newFakeModelAliasStore(),
	}
	srv := httptest.NewServer(NewRouter(d))
	defer srv.Close()
	c := authedClient(t, srv)
	r := c.delete(t, "/api/v1/models/aliases/nope")
	if r.StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", r.StatusCode, readBody(t, r))
	}
}

// TestModelAlias_NonAdminForbidden — a "user" role gets 403 on mutations
// and 200 on GET. Matches the cache/redaction handler test patterns.
func TestModelAlias_NonAdminForbidden(t *testing.T) {
	d := Deps{
		Log:          discardLog(),
		Users:        &fakeUserStore{role: "user"},
		ModelAliases: newFakeModelAliasStore(),
	}
	srv := httptest.NewServer(NewRouter(d))
	defer srv.Close()
	c := authedClient(t, srv)

	// GET allowed.
	if r := c.get(t, "/api/v1/models/aliases"); r.StatusCode != http.StatusOK {
		t.Fatalf("GET as user status=%d", r.StatusCode)
	}
	// POST forbidden.
	r := c.post(t, "/api/v1/models/aliases", map[string]string{
		"alias": "x", "concrete_model": "m", "service_id": "s",
	})
	if r.StatusCode != http.StatusForbidden {
		t.Fatalf("POST as user status=%d body=%s", r.StatusCode, readBody(t, r))
	}
	// PUT forbidden.
	r = c.put(t, "/api/v1/models/aliases/x", map[string]string{
		"concrete_model": "m", "service_id": "s",
	})
	if r.StatusCode != http.StatusForbidden {
		t.Fatalf("PUT as user status=%d body=%s", r.StatusCode, readBody(t, r))
	}
	// DELETE forbidden.
	r = c.delete(t, "/api/v1/models/aliases/x")
	if r.StatusCode != http.StatusForbidden {
		t.Fatalf("DELETE as user status=%d body=%s", r.StatusCode, readBody(t, r))
	}
}
