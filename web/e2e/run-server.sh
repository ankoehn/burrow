#!/usr/bin/env sh
# run-server.sh — builds and starts burrowd for the Playwright e2e suite.
# Called by playwright.config.ts webServer.command on the CI ubuntu runner.
# Env vars injected by the config:
#   BURROW_ADMIN_EMAIL, BURROW_ADMIN_PASSWORD,
#   BURROW_HTTP_LISTEN, BURROW_DATABASE_PATH, E2E_TMPDIR
set -e

TMPDIR="${E2E_TMPDIR:-/tmp/burrow-e2e-$$}"
mkdir -p "$TMPDIR"

BINARY="$TMPDIR/burrowd"

# Build from the repo root (two levels up from web/e2e/).
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
echo "[run-server] building burrowd from $REPO_ROOT ..."
go build -o "$BINARY" "$REPO_ROOT/cmd/server"

echo "[run-server] starting burrowd on ${BURROW_HTTP_LISTEN:-:8723} ..."
exec "$BINARY" serve --dev-certs
