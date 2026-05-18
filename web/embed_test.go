package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerServesIndexAndSPAFallback(t *testing.T) {
	h, err := Handler()
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/", nil))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("/ want 200 text/html, got %d %q", rr.Code, rr.Header().Get("Content-Type"))
	}
	body, _ := io.ReadAll(rr.Body)
	if !strings.Contains(string(body), `id="root"`) {
		t.Fatalf("/ did not serve the SPA index.html: %.120s", body)
	}
	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, httptest.NewRequest("GET", "/tokens/deep/route", nil))
	b2, _ := io.ReadAll(rr2.Body)
	if rr2.Code != http.StatusOK || !strings.Contains(string(b2), `id="root"`) {
		t.Fatalf("SPA fallback failed: %d %.120s", rr2.Code, b2)
	}
}

func TestHandlerServesHashedAsset(t *testing.T) {
	h, err := Handler()
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/", nil))
	idx, _ := io.ReadAll(rr.Body)
	i := strings.Index(string(idx), "/assets/")
	if i < 0 {
		t.Skip("no /assets/ reference in index.html (unexpected for a Vite build)")
	}
	s := string(idx)[i:]
	end := strings.IndexAny(s, `"'`)
	asset := s[:end]
	ar := httptest.NewRecorder()
	h.ServeHTTP(ar, httptest.NewRequest("GET", asset, nil))
	if ar.Code != http.StatusOK {
		t.Fatalf("asset %s want 200, got %d", asset, ar.Code)
	}
	ct := ar.Header().Get("Content-Type")
	if !strings.Contains(ct, "javascript") && !strings.Contains(ct, "css") {
		t.Fatalf("asset %s unexpected content-type %q", asset, ct)
	}
}
