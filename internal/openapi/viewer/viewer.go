// Package viewer serves a hand-rolled, zero-dependency OpenAPI browser UI.
// It embeds viewer.html, viewer.css, and viewer.js at build time via embed.FS
// and exposes two chi routes:
//
//	GET /api/v1/openapi/viewer            → viewer.html (text/html)
//	GET /api/v1/openapi/viewer/static/{file} → viewer.css or viewer.js only
//
// The static file allowlist is closed: any path other than viewer.css or
// viewer.js returns 404. The handler itself is gated by the caller; the
// permission check lives in the router (metrics:read or admin).
package viewer

import (
	"embed"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// assets holds the three embedded files declared below.
//
//go:embed viewer.html viewer.css viewer.js
var assets embed.FS

// htmlContent is the pre-read viewer.html byte slice.
//
//go:embed viewer.html
var htmlContent []byte

// JS returns the embedded viewer.js source. Used by tests to assert the
// fetch() call count without hitting the filesystem at runtime.
func JS() []byte {
	b, _ := assets.ReadFile("viewer.js")
	return b
}

// staticAllowlist is the closed set of files servable from /static/{file}.
// Any request for a path not in this map returns 404.
var staticAllowlist = map[string]struct {
	contentType string
	read        func() ([]byte, error)
}{
	"viewer.css": {
		contentType: "text/css; charset=utf-8",
		read:        func() ([]byte, error) { return assets.ReadFile("viewer.css") },
	},
	"viewer.js": {
		contentType: "application/javascript; charset=utf-8",
		read:        func() ([]byte, error) { return assets.ReadFile("viewer.js") },
	},
}

// Handler serves the embedded OpenAPI viewer HTML and its static assets.
type Handler struct {
	log *slog.Logger
}

// New returns a Handler ready to be registered on a chi subrouter.
func New(log *slog.Logger) *Handler {
	return &Handler{log: log}
}

// Routes registers the two viewer endpoints on r (expected to be the
// /api/v1/openapi/viewer subrouter):
//
//	GET /     → viewer.html
//	GET /static/{file} → viewer.css or viewer.js; anything else → 404
func (h *Handler) Routes(r chi.Router) {
	r.Get("/", h.serveHTML)
	r.Get("/static/{file}", h.serveStatic)
}

func (h *Handler) serveHTML(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(htmlContent)
}

func (h *Handler) serveStatic(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "file")
	entry, ok := staticAllowlist[name]
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	data, err := entry.read()
	if err != nil {
		h.log.Error("viewer: embed read failed", "file", name, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", entry.contentType)
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
