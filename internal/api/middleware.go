package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ankoehn/burrow/internal/store"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// RequireSession authenticates via the burrow_session cookie and injects the user id.
func (d Deps) RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookieName)
		if err != nil || c.Value == "" {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		uid, err := d.Users.ValidateSession(r.Context(), c.Value)
		if err != nil {
			if errors.Is(err, store.ErrUnauthorized) {
				writeErr(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			writeErr(w, http.StatusInternalServerError, "session check failed")
			return
		}
		ctx := context.WithValue(r.Context(), userIDKey, uid)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// isStaticAssetPath reports whether the request path is a static/SPA-asset
// path that should be logged at Debug rather than Info. These are the paths
// served directly by the embedded SPA handler (web/embed.go): the root,
// hashed JS/CSS bundles under /assets/, the favicon, and index.html itself.
func isStaticAssetPath(p string) bool {
	return p == "/" ||
		p == "/index.html" ||
		p == "/favicon.svg" ||
		strings.HasPrefix(p, "/assets/")
}

// requestLogger logs method, path, status and duration via slog.
// Requests for static/SPA-asset paths are logged at Debug to avoid INFO noise
// on every page load; /api/v1/* and other routes remain at Info (or Error on 5xx).
func (d Deps) requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(sw, r)
		lvl := slog.LevelInfo
		if sw.status >= 500 {
			lvl = slog.LevelError
		} else if isStaticAssetPath(r.URL.Path) {
			lvl = slog.LevelDebug
		}
		d.Log.Log(r.Context(), lvl, "http", "method", r.Method, "path", r.URL.Path,
			"status", sw.status, "dur", time.Since(start).String())
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(c int) { s.status = c; s.ResponseWriter.WriteHeader(c) }

// Flush implements http.Flusher so SSE keeps working through the wrapper.
func (s *statusWriter) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
