package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/httprate"
)

// JSONHandlerTimeout is the maximum duration the chi middleware.Timeout allows
// a JSON API handler to run. cmd/server uses this constant to set the HTTP
// server shutdown grace period to strictly exceed this value, ensuring every
// in-flight handler completes (or is chi-cancelled) before database.Close()
// runs.
const JSONHandlerTimeout = 30 * time.Second

// loginRateLimiters builds the two httprate middlewares applied only to
// POST /auth/login: one per-IP limiter and one global endpoint limiter.
// Both values are resolved from Deps overrides (for test injection) with
// fallback to the package-level constants.
//
// Per-IP accuracy depends on the trusted-proxy gating wired in C2 (RealIP
// currently trusts XFF unconditionally; C2 will gate it behind a
// trusted-proxy config so spoofed IPs cannot bypass per-IP limits).
func (d Deps) loginRateLimiters() (perIP, global func(http.Handler) http.Handler) {
	limitPerIP := d.LoginRateLimitPerIPOverride
	if limitPerIP <= 0 {
		limitPerIP = LoginRateLimitPerIP
	}
	limitGlobal := d.LoginRateLimitGlobalOverride
	if limitGlobal <= 0 {
		limitGlobal = LoginRateLimitGlobal
	}
	rateLimitedHandler := httprate.WithLimitHandler(func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, http.StatusTooManyRequests, "too many login attempts")
	})
	perIP = httprate.Limit(limitPerIP, time.Minute,
		httprate.WithKeyFuncs(httprate.KeyByIP),
		rateLimitedHandler,
	)
	global = httprate.Limit(limitGlobal, time.Minute,
		httprate.WithKeyFuncs(httprate.KeyByEndpoint),
		rateLimitedHandler,
	)
	return perIP, global
}

// NewRouter builds the /api/v1 HTTP handler.
func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RealIP)
	r.Use(middleware.RequestID)
	r.Use(d.requestLogger)
	r.Use(middleware.Recoverer)

	loginPerIP, loginGlobal := d.loginRateLimiters()

	r.Route("/api/v1", func(r chi.Router) {
		r.With(loginPerIP, loginGlobal).Post("/auth/login", d.Login)

		// JSON routes: session-protected + JSONHandlerTimeout.
		r.Group(func(r chi.Router) {
			r.Use(d.RequireSession)
			r.Use(middleware.Timeout(JSONHandlerTimeout))
			r.Post("/auth/logout", d.Logout)
			r.Get("/me", d.Me)
			r.Get("/tokens", d.ListTokens)
			r.Post("/tokens", d.CreateToken)
			r.Delete("/tokens/{id}", d.RevokeToken)
			r.Get("/tunnels", d.ListTunnels)
		})

		// SSE: session-protected, NO timeout (long-lived stream).
		r.Group(func(r chi.Router) {
			r.Use(d.RequireSession)
			r.Get("/events", d.EventsStream)
		})
	})

	if d.SPA != nil {
		// Only a root catch-all: "/api/v1" is a mounted subrouter so chi
		// matches it first; unknown/unauth /api/v1/* stays in the API group's
		// own JSON 404/401 and never falls through here. (r.NotFound is NOT
		// used: chi propagates the root NotFound into the /api/v1 subrouter,
		// which would wrongly serve the SPA for /api/v1/nope.)
		r.Handle("/*", d.SPA)
	}

	return r
}
