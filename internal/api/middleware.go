package api

import (
	"context"
	"crypto/subtle"
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

// csrfSafeMethods is the set of HTTP methods that are exempt from CSRF checks
// (they are defined as safe/idempotent in RFC 7231 and do not change state).
var csrfSafeMethods = map[string]bool{
	http.MethodGet:     true,
	http.MethodHead:    true,
	http.MethodOptions: true,
}

// RequireCSRF is a middleware that enforces the double-submit cookie pattern
// for all non-safe HTTP methods (POST, PUT, PATCH, DELETE).
//
// For state-changing requests it requires:
//   - The burrow_csrf cookie to be present and non-empty.
//   - The X-CSRF-Token request header to equal the cookie value
//     (compared with crypto/subtle.ConstantTimeCompare to prevent timing attacks).
//
// On mismatch or missing values → 403. Safe methods (GET/HEAD/OPTIONS) pass
// through unconditionally so SSE and read-only endpoints are never blocked.
//
// This middleware MUST be applied after RequireSession so an unauthenticated
// request returns 401 before it reaches CSRF validation.
func RequireCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if csrfSafeMethods[r.Method] {
			next.ServeHTTP(w, r)
			return
		}
		cookie, err := r.Cookie(csrfCookieName)
		if err != nil || cookie.Value == "" {
			writeErr(w, http.StatusForbidden, "csrf token invalid")
			return
		}
		header := r.Header.Get("X-CSRF-Token")
		if subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(header)) != 1 {
			writeErr(w, http.StatusForbidden, "csrf token invalid")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireAdmin is a middleware that must run AFTER RequireSession (so that
// unauthenticated requests get 401 before this 403 check runs). It loads the
// authed user and rejects with 403 if their role is not "admin".
func (d Deps) RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uid := userID(r.Context())
		u, err := d.Users.GetUserByID(r.Context(), uid)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "lookup failed")
			return
		}
		if u.Role != "admin" {
			writeErr(w, http.StatusForbidden, "admin required")
			return
		}
		next.ServeHTTP(w, r)
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
