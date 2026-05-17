package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// NewRouter builds the /api/v1 HTTP handler.
func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RealIP)
	r.Use(middleware.RequestID)
	r.Use(d.requestLogger)
	r.Use(middleware.Recoverer)

	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/auth/login", d.Login)

		// JSON routes: session-protected + 30s timeout.
		r.Group(func(r chi.Router) {
			r.Use(d.RequireSession)
			r.Use(middleware.Timeout(30 * time.Second))
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
	return r
}
