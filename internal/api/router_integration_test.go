//go:build integration

// test-only — never deploy this shape.
package api

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "modernc.org/sqlite"
)

// TestIntegrationTestResetEndpoint verifies POST /api/v1/internal/test-reset
// is registered, unauthenticated (so test runners can call it without first
// minting a token), and returns 204 No Content when the database is
// reachable. The handler is build-tagged (//go:build integration); the
// default-build stub registers no route, so this test only runs under
// `go test -tags=integration`.
func TestIntegrationTestResetEndpoint(t *testing.T) {
	rawDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = rawDB.Close() })

	d := Deps{Log: discardLog(), DB: rawDB}
	srv := httptest.NewServer(NewRouter(d))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/v1/internal/test-reset", "application/json", nil)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("test-reset status=%d want 204", resp.StatusCode)
	}
}

// TestIntegrationTestResetRequiresPOST verifies the endpoint rejects GETs.
func TestIntegrationTestResetRequiresPOST(t *testing.T) {
	rawDB, _ := sql.Open("sqlite", ":memory:")
	t.Cleanup(func() { _ = rawDB.Close() })

	d := Deps{Log: discardLog(), DB: rawDB}
	srv := httptest.NewServer(NewRouter(d))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/internal/test-reset")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET test-reset status=%d want 405", resp.StatusCode)
	}
}
