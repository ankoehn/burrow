package api

import "net/http"

// hstsHeader is the Strict-Transport-Security header value used when the
// server is serving native HTTPS. Two-year max-age; includeSubDomains.
const hstsHeader = "max-age=63072000; includeSubDomains"

// HSTSMiddleware returns a middleware that adds Strict-Transport-Security to
// every response, but ONLY when enabled is true (i.e. the server is serving
// TLS natively via BURROW_HTTP_TLS_CERT/KEY). When enabled is false the header
// is not emitted.
//
// The enabled flag is set by cmd/server based on the config fields, NOT derived
// from request headers such as X-Forwarded-Proto, which are spoofable unless a
// trusted proxy is in the path. This ensures HSTS is never emitted on a plain
// HTTP deployment regardless of what headers an attacker sends.
func HSTSMiddleware(enabled bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if enabled {
				w.Header().Set("Strict-Transport-Security", hstsHeader)
			}
			next.ServeHTTP(w, r)
		})
	}
}
