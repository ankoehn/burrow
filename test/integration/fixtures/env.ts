// test-only — never deploy this shape.
//
// Shared constants for the compose UI mini-suite.
// MUST match test/integration/compose.e2e.yml. If you change one, change both.

export const ADMIN_EMAIL = "admin@e2e.local";
export const ADMIN_PASSWORD = "e2e-pass";

// Dashboard speaks plain HTTP under --dev-certs (only :7000 control plane is TLS).
export const DASHBOARD_URL = "http://localhost:8080";

// The TCP tunnel exposed by the client container's `upstream` service.
// client-entrypoint.sh defaults BURROW_TUNNEL_NAME=upstream, BURROW_REMOTE_PORT=9000.
export const TUNNEL_URL = "http://localhost:9000";

// Compose file path, relative to the repo root (Taskfile + smoke-ui.sh use this).
export const COMPOSE_FILE = "test/integration/compose.e2e.yml";

// The default tunnel name seeded by client-entrypoint.sh.
export const SEEDED_TUNNEL_NAME = "upstream";
