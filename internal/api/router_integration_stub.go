//go:build !integration

// Default-build stub for the integration test-only route registrar. The
// real implementation lives in router_integration.go (//go:build
// integration). Production binaries get THIS file, so registerIntegrationRoutes
// is a no-op — no symbol for `/api/v1/internal/test-reset` lands in the
// release binary (verified by `go tool nm` in the harness verification step).
package api

import (
	"github.com/go-chi/chi/v5"
)

func registerIntegrationRoutes(_ chi.Router, _ Deps) {}
