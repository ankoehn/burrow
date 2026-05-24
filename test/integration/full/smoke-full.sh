#!/usr/bin/env bash
# test-only — never deploy this shape.
# Brings up the full e2e stack, runs all 20 Playwright specs (mock profile),
# tears down via trap. Mirrors test/integration/smoke-ui.sh.

set -euo pipefail
REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
cd "$REPO_ROOT"
COMPOSE="test/integration/full/compose.full.yml"
DIR="test/integration/full"

teardown() {
  echo "[smoke-full] tearing down"
  docker compose -f "$COMPOSE" down --volumes || true
}
trap teardown EXIT

echo "[smoke-full] building + starting stack (--wait)"
docker compose -f "$COMPOSE" up -d --build --wait

# Plan-fidelity note: the plan-as-written included `curl :9001/healthz` here.
# That binding doesn't exist — HTTP tunnels are host-routed on :8443 only.
# Smoke checks the surfaces that actually exist.
echo "[smoke-full] sanity: dashboard + TCP tunnels + /test-reset"
curl -fsS http://localhost:8080/healthz >/dev/null                             # dashboard
curl -fsS http://localhost:9002/healthz >/dev/null                             # tcp-echo
curl -fsS http://localhost:9003/healthz >/dev/null                             # multi svc-a
curl -fsS http://localhost:9004/healthz >/dev/null                             # multi svc-b
test "$(curl -fsS -o /dev/null -w "%{http_code}" -X POST http://localhost:8080/api/v1/internal/test-reset)" = "204"

echo "[smoke-full] running Playwright (mock profile, 20 specs)"
cd "$DIR"
npm ci --no-fund --no-audit
npx playwright install --with-deps chromium
npx playwright test --project=mock

echo "[smoke-full] all specs passed"
