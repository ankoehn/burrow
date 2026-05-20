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
// Per-IP accuracy is guaranteed by TrustedProxyMiddleware (C2), which runs
// before this limiter and only honors X-Forwarded-For when the TCP peer is
// within a trusted CIDR — preventing spoofed headers from bypassing the limit.
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
	// TrustedProxyMiddleware replaces the unconditional middleware.RealIP.
	// It only honors X-Forwarded-For / X-Real-IP when the immediate TCP peer
	// is within a trusted CIDR (Deps.TrustedProxies). When TrustedProxies is
	// empty (the safe default), forwarded headers are ignored entirely and the
	// raw TCP peer is used — preventing XFF spoofing from bypassing the per-IP
	// login rate-limiter or poisoning session.ip records.
	// MUST run before the per-IP rate-limiter and Login.
	r.Use(TrustedProxyMiddleware(d.TrustedProxies))
	// HSTSMiddleware adds Strict-Transport-Security only when the server is
	// serving native TLS (Deps.HTTPSEnabled). It intentionally does NOT read
	// X-Forwarded-Proto or any other spoofable header — the flag is set by
	// cmd/server from config, not from incoming request headers.
	r.Use(HSTSMiddleware(d.HTTPSEnabled))
	r.Use(middleware.RequestID)
	r.Use(d.requestLogger)
	r.Use(middleware.Recoverer)

	// Liveness/readiness probes: unauthenticated, no CSRF, no /api/v1 prefix
	// (k8s-style). Registered before the SPA catch-all so they are not
	// shadowed by client-side routing.
	r.Get("/healthz", d.Healthz)
	r.Get("/readyz", d.Readyz)

	loginPerIP, loginGlobal := d.loginRateLimiters()

	r.Route("/api/v1", func(r chi.Router) {
		r.With(loginPerIP, loginGlobal).Post("/auth/login", d.Login)

		// JSON routes: session-protected + CSRF-protected + JSONHandlerTimeout.
		// RequireCSRF is placed after RequireSession so unauthenticated requests
		// get 401 before CSRF validation runs. Safe methods (GET/HEAD/OPTIONS)
		// pass through CSRF unconditionally — future state-changing routes added
		// to this group are automatically protected.
		r.Group(func(r chi.Router) {
			r.Use(d.RequireSession)
			r.Use(RequireCSRF)
			r.Use(middleware.Timeout(JSONHandlerTimeout))
			r.Post("/auth/logout", d.Logout)
			r.Post("/auth/change-password", d.ChangePassword)
			r.Get("/me", d.Me)
			r.Get("/tokens", d.ListTokens)
			r.Post("/tokens", d.CreateToken)
			r.Delete("/tokens/{id}", d.RevokeToken)
			r.Get("/tunnels", d.ListTunnels)
			r.Get("/sessions", d.ListSessions)
			r.Delete("/sessions/{id}", d.RevokeSession)
			r.Post("/sessions/revoke-all", d.RevokeAllSessions)
			r.Put("/tunnels/{id}/access-mode", d.SetAccessMode)
			// v0.3.0: service-scoped routes (owner-gated via store authz).
			r.Get("/services", d.ListServices)
			r.Get("/services/{serviceID}", d.GetService)
			r.Put("/services/{serviceID}/access-mode", d.SetServiceAccessMode)
			r.Get("/services/{serviceID}/api-keys", d.ListAPIKeys)
			r.Post("/services/{serviceID}/api-keys", d.CreateAPIKey)
			r.Delete("/services/{serviceID}/api-keys/{id}", d.DeleteAPIKey)
			r.Get("/services/{serviceID}/access-policy", d.GetAccessPolicy)
			r.Put("/services/{serviceID}/access-policy", d.SetAccessPolicy)
			// v0.4.0 Task 4: exact-match prompt cache JSON API.
			// GET endpoints are session-authed only (any user may read
			// settings/stats); the PUT and global DELETE are gated by
			// requireAdminOrAIConfigureAny (admin OR ai:configure:any);
			// the per-service DELETE handler does its own ownership
			// check (owner OR ai:configure:own/any).
			r.Get("/cache/settings", d.GetCacheSettings)
			r.Get("/cache/stats", d.GetCacheStats)
			r.With(d.requireAdminOrAIConfigureAny).Put("/cache/settings", d.PutCacheSettings)
			r.With(d.requireAdminOrAIConfigureAny).Delete("/cache/entries", d.DeleteCacheEntries)
			r.Delete("/services/{serviceID}/cache/entries", d.DeleteServiceCacheEntries)
			// v0.4.0 Task 5: redaction rules + settings + preview JSON API.
			// GET endpoints are session-authed (any user may read); mutations
			// (POST/PUT/DELETE rules, PUT settings, POST preview) are gated
			// by requireAdminOrAIConfigureAny (admin OR ai:configure:any),
			// same pattern as the cache mutation routes.
			r.Get("/redaction/rules", d.GetRedactionRules)
			r.With(d.requireAdminOrAIConfigureAny).Post("/redaction/rules", d.PostRedactionRule)
			r.With(d.requireAdminOrAIConfigureAny).Put("/redaction/rules/{id}", d.PutRedactionRule)
			r.With(d.requireAdminOrAIConfigureAny).Delete("/redaction/rules/{id}", d.DeleteRedactionRule)
			r.Get("/redaction/settings", d.GetRedactionSettings)
			r.With(d.requireAdminOrAIConfigureAny).Put("/redaction/settings", d.PutRedactionSettings)
			r.With(d.requireAdminOrAIConfigureAny).Post("/redaction/preview", d.PostRedactionPreview)
			// v0.4.0 Task 6: prompt-injection guardrails JSON API.
			// GET endpoints are session-authed (any user may read settings
			// and the bundled-pattern list); PUT settings is gated by
			// requireAdminOrAIConfigureAny (admin OR ai:configure:any),
			// same pattern as the cache/redaction mutation routes.
			r.Get("/guardrails/settings", d.GetGuardrailSettings)
			r.With(d.requireAdminOrAIConfigureAny).Put("/guardrails/settings", d.PutGuardrailSettings)
			r.Get("/guardrails/patterns", d.GetGuardrailPatterns)
			// v0.4.0 Task 7: model alias registry (spec Part C.1).
			// GET is session-authed (any user may read); POST/PUT/DELETE are
			// admin OR ai:configure:any — same gating as cache/redaction/
			// guardrails mutations.
			r.Get("/models/aliases", d.GetModelAliases)
			r.With(d.requireAdminOrAIConfigureAny).Post("/models/aliases", d.PostModelAlias)
			r.With(d.requireAdminOrAIConfigureAny).Put("/models/aliases/{alias}", d.PutModelAlias)
			r.With(d.requireAdminOrAIConfigureAny).Delete("/models/aliases/{alias}", d.DeleteModelAlias)
			// Admin-only user management: RequireAdmin runs after RequireSession
			// (already applied by the outer Group), so unauthenticated requests
			// get 401 before RequireAdmin's 403 check runs.
			r.Group(func(r chi.Router) {
				r.Use(d.RequireAdmin)
				r.Get("/users", d.AdminListUsers)
				r.Post("/users", d.AdminCreateUser)
				r.Patch("/users/{id}", d.AdminUpdateUser)
				r.Delete("/users/{id}", d.AdminDeleteUser)
				r.Get("/roles", d.ListRoles)
				r.Get("/roles/{name}", d.GetRole)
				r.Get("/settings", d.GetSettings)
				r.Put("/settings", d.SaveSettings)
				r.Post("/settings/test-email", d.SendTestEmail)
				r.Get("/clients", d.ListClients)
				r.Get("/clients/{sessionID}", d.GetClient)
			})
		})

		// SSE: session-protected, NO timeout (long-lived stream).
		// CSRF is not applied here: GET is a safe method and SSE clients cannot
		// send custom headers after the connection is upgraded.
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
