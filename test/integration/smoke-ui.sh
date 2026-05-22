#!/usr/bin/env bash
# test-only — never deploy this shape.
# test/integration/smoke-ui.sh
# Brings up the 2-docker e2e stack, runs the Playwright UI mini-suite,
# tears the stack down via `trap` on EXIT. Mirrors smoke.sh's shape.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$REPO_ROOT"

COMPOSE_FILE="test/integration/compose.e2e.yml"
INTEGRATION_DIR="test/integration"

teardown() {
  echo "[smoke-ui] tearing down stack"
  docker compose -f "$REPO_ROOT/$COMPOSE_FILE" down --volumes || true
}
trap teardown EXIT

echo "[smoke-ui] building + starting stack (--wait blocks until healthy)"
docker compose -f "$COMPOSE_FILE" up -d --build --wait

# Sanity: the relay healthcheck is enough, but double-check the tunnel
# is reachable so a Playwright failure points at the UI, not the data plane.
echo "[smoke-ui] sanity curl through tunnel"
curl -fsS http://localhost:9000/healthz >/dev/null

echo "[smoke-ui] running Playwright suite"
cd "$INTEGRATION_DIR"
# npm ci is idempotent and fast when node_modules is already in sync.
npm ci
npx playwright install --with-deps chromium
npx playwright test

echo "[smoke-ui] all specs passed"
