package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"
)

// docsOpenAPIPath is the canonical hand-written OpenAPI v3 file relative to
// the package directory. The route-coverage test loads it directly from the
// source tree (not the embedded copy) so a stale embed never masks a missing
// path entry.
const docsOpenAPIPath = "../../docs/openapi.yaml"

// routeEntry is a single (method, path) the chi mux exposes.
type routeEntry struct {
	Method string
	Path   string
}

// enumerateRoutes walks the chi mux and returns every (method, path) the
// router answers. Health probes (/healthz, /readyz) are excluded by
// convention — they exist for k8s/load-balancer use, not for SDK consumers.
// The OpenAPI doc serve endpoints are also excluded: they describe the spec
// itself, not the JSON API, and pinning them inside the doc would create a
// self-referential surface that adds no SDK value.
func enumerateRoutes(t *testing.T, r chi.Router) []routeEntry {
	t.Helper()
	var out []routeEntry
	err := chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		switch route {
		case "/healthz", "/readyz":
			return nil
		case "/api/v1/openapi.yaml", "/api/v1/openapi.json":
			return nil
		}
		out = append(out, routeEntry{Method: method, Path: route})
		return nil
	})
	if err != nil {
		t.Fatalf("chi.Walk: %v", err)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Method < out[j].Method
	})
	return out
}

// loadOpenAPIPaths parses docs/openapi.yaml and returns the set of
// (method, path) entries declared in its `paths:` mapping. Each path key
// is expected to be a literal OpenAPI path (e.g. /api/v1/services/{id});
// chi path params use the same `{id}` form, so no template conversion is
// needed.
func loadOpenAPIPaths(t *testing.T, path string) map[routeEntry]bool {
	t.Helper()
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("abs(%s): %v", path, err)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read %s: %v", abs, err)
	}
	var doc struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("yaml unmarshal: %v", err)
	}
	out := make(map[routeEntry]bool, len(doc.Paths)*4)
	for p, methods := range doc.Paths {
		for m := range methods {
			// Spec methods are typically lowercase; chi reports uppercase.
			out[routeEntry{Method: strings.ToUpper(m), Path: p}] = true
		}
	}
	return out
}

// TestOpenAPI_RouteCoverage walks the chi mux and asserts every
// (method, path) it answers is documented in docs/openapi.yaml. Adding a
// new route without a corresponding YAML entry breaks the build — this is
// the lock that keeps the hand-written doc honest.
func TestOpenAPI_RouteCoverage(t *testing.T) {
	r := NewRouter(Deps{Log: discardLog()})
	mux, ok := r.(chi.Router)
	if !ok {
		t.Fatalf("NewRouter did not return chi.Router; got %T", r)
	}
	routes := enumerateRoutes(t, mux)
	docPaths := loadOpenAPIPaths(t, docsOpenAPIPath)

	var missing []string
	for _, e := range routes {
		if !docPaths[e] {
			missing = append(missing, fmt.Sprintf("%s %s", e.Method, e.Path))
		}
	}
	if len(missing) > 0 {
		t.Fatalf("%d route(s) not documented in %s:\n  %s",
			len(missing), docsOpenAPIPath, strings.Join(missing, "\n  "))
	}
}

// TestOpenAPI_EmbedFresh asserts the embedded copy at
// internal/api/openapi.yaml matches the canonical docs/openapi.yaml byte-
// for-byte. The two files exist because go:embed cannot reach outside the
// package directory; this test is the contract that keeps them in sync.
// If you edit docs/openapi.yaml, run:
//
//	cp docs/openapi.yaml internal/api/openapi.yaml
func TestOpenAPI_EmbedFresh(t *testing.T) {
	canonical, err := os.ReadFile(docsOpenAPIPath)
	if err != nil {
		t.Fatalf("read canonical: %v", err)
	}
	mirror, err := os.ReadFile("openapi.yaml")
	if err != nil {
		t.Fatalf("read embed mirror: %v", err)
	}
	if string(canonical) != string(mirror) {
		t.Fatalf("internal/api/openapi.yaml is stale; copy docs/openapi.yaml into internal/api/openapi.yaml")
	}
}

// TestOpenAPI_ServeYAML asserts GET /api/v1/openapi.yaml returns 200 with
// the embedded YAML bytes and `application/yaml` content-type. The route is
// public (no auth) so SDK tooling can curl it.
func TestOpenAPI_ServeYAML(t *testing.T) {
	srv := httptest.NewServer(NewRouter(Deps{Log: discardLog()}))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/v1/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/yaml") {
		t.Errorf("Content-Type=%q want application/yaml*", ct)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "openapi:") {
		t.Errorf("body missing openapi: header; got %q...", body[:min(120, len(body))])
	}
	if !strings.Contains(body, "paths:") {
		t.Errorf("body missing paths: section")
	}
}

// TestOpenAPI_ServeJSON asserts GET /api/v1/openapi.json returns 200 with
// the YAML converted to JSON at request time (no new dep — yaml.v3 →
// encoding/json). The content-type is application/json and the body parses
// as a generic JSON object with the same top-level keys as the YAML.
func TestOpenAPI_ServeJSON(t *testing.T) {
	srv := httptest.NewServer(NewRouter(Deps{Log: discardLog()}))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/v1/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type=%q want application/json*", ct)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(readBody(t, resp)), &parsed); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if _, ok := parsed["openapi"]; !ok {
		t.Errorf("JSON body missing openapi key: keys=%v", keysOf(t, parsed))
	}
	if _, ok := parsed["paths"]; !ok {
		t.Errorf("JSON body missing paths key: keys=%v", keysOf(t, parsed))
	}
}
