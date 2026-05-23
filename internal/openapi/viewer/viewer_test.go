package viewer_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ankoehn/burrow/internal/openapi/viewer"
	"github.com/go-chi/chi/v5"
)

// newTestServer registers the viewer routes on a fresh chi mux and returns a
// running httptest.Server mounted at /api/v1/openapi/viewer (matching the
// production registration).
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := viewer.New(log)
	r := chi.NewRouter()
	r.Route("/api/v1/openapi/viewer", h.Routes)
	return httptest.NewServer(r)
}

// TestViewerServesHTMLAndStaticAllowlist covers the four assertions from the
// task specification:
//
//  1. GET /api/v1/openapi/viewer → 200 text/html
//  2. GET /api/v1/openapi/viewer/static/viewer.css → 200 text/css
//  3. GET /api/v1/openapi/viewer/static/viewer.js → 200 application/javascript
//  4. GET /api/v1/openapi/viewer/static/evil.exe → 404
func TestViewerServesHTMLAndStaticAllowlist(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	tests := []struct {
		name         string
		path         string
		wantStatus   int
		wantCTPrefix string
	}{
		{
			name:         "HTML root",
			path:         "/api/v1/openapi/viewer/",
			wantStatus:   http.StatusOK,
			wantCTPrefix: "text/html",
		},
		{
			name:         "CSS static file",
			path:         "/api/v1/openapi/viewer/static/viewer.css",
			wantStatus:   http.StatusOK,
			wantCTPrefix: "text/css",
		},
		{
			name:         "JS static file",
			path:         "/api/v1/openapi/viewer/static/viewer.js",
			wantStatus:   http.StatusOK,
			wantCTPrefix: "application/javascript",
		},
		{
			name:       "non-allowlisted file",
			path:       "/api/v1/openapi/viewer/static/evil.exe",
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := http.Get(srv.URL + tc.path)
			if err != nil {
				t.Fatalf("GET %s: %v", tc.path, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tc.wantStatus {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("status=%d want=%d body=%s", resp.StatusCode, tc.wantStatus, body)
			}
			if tc.wantCTPrefix != "" {
				ct := resp.Header.Get("Content-Type")
				if !strings.HasPrefix(ct, tc.wantCTPrefix) {
					t.Errorf("Content-Type=%q want prefix %q", ct, tc.wantCTPrefix)
				}
			}
		})
	}
}

// TestViewerHTMLContainsExpectedElements spot-checks that the HTML response
// contains the core structural elements described in the spec (title, sidebar,
// detail pane, script reference, CSS reference).
func TestViewerHTMLContainsExpectedElements(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/openapi/viewer/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	checks := []struct{ name, want string }{
		{"DOCTYPE", "<!DOCTYPE html>"},
		{"CSS link", "viewer.css"},
		{"JS script", "viewer.js"},
		{"sidebar element", "sidebar"},
		{"detail element", "detail"},
		{"theme toggle", "theme-toggle"},
	}
	for _, c := range checks {
		if !strings.Contains(html, c.want) {
			t.Errorf("[%s] HTML missing %q", c.name, c.want)
		}
	}
}

// TestViewerCSSBodyNotEmpty ensures the embedded CSS has non-trivial content.
func TestViewerCSSBodyNotEmpty(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/openapi/viewer/static/viewer.css")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if len(body) < 100 {
		t.Errorf("CSS suspiciously short: %d bytes", len(body))
	}
	if !strings.Contains(string(body), "--bg") {
		t.Error("CSS missing --bg custom property")
	}
}

// TestViewerJSFetchCountIsOne is the static assertion from Step 4 of the spec:
// the viewer JS must contain exactly one fetch() call (the single same-origin
// GET /api/v1/openapi.yaml). XMLHttpRequest, WebSocket, and EventSource must
// appear zero times.
func TestViewerJSFetchCountIsOne(t *testing.T) {
	js := string(viewer.JS())

	if count := strings.Count(js, "fetch("); count != 1 {
		t.Errorf("expected exactly one fetch() call; got %d", count)
	}
	if count := strings.Count(js, "XMLHttpRequest"); count != 0 {
		t.Errorf("expected zero XMLHttpRequest references; got %d", count)
	}
	if count := strings.Count(js, "WebSocket"); count != 0 {
		t.Errorf("expected zero WebSocket references; got %d", count)
	}
	if count := strings.Count(js, "EventSource"); count != 0 {
		t.Errorf("expected zero EventSource references; got %d", count)
	}
}

// TestViewerStaticUnknownFileTypes checks several additional non-allowlisted
// names to ensure the allowlist is truly closed.
func TestViewerStaticUnknownFileTypes(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	paths := []string{
		"/api/v1/openapi/viewer/static/../../etc/passwd",
		"/api/v1/openapi/viewer/static/viewer.html",
		"/api/v1/openapi/viewer/static/",
		"/api/v1/openapi/viewer/static/foo.txt",
	}
	for _, p := range paths {
		resp, err := http.Get(srv.URL + p)
		if err != nil {
			t.Fatalf("GET %s: %v", p, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			t.Errorf("GET %s: want non-200 status, got %d", p, resp.StatusCode)
		}
	}
}
