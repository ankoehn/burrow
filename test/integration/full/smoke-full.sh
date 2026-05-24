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
  # The script cd's into $DIR for the Playwright run; restore CWD so the
  # relative $COMPOSE path resolves against the repo root.
  cd "$REPO_ROOT"
  docker compose -f "$COMPOSE" down --volumes || true
}
trap teardown EXIT

echo "[smoke-full] building + starting stack (--wait)"
docker compose -f "$COMPOSE" up -d --build --wait

# Plan-fidelity notes:
#   - The plan-as-written included `curl :9001/healthz` here. That binding
#     doesn't exist — HTTP tunnels are host-routed on :8443 only.
#   - compose --wait only waits for healthchecks on services that define them
#     (relay + mockoai); client containers register their tunnels
#     asynchronously after the relay reports healthy, so we poll each TCP
#     tunnel port until it responds rather than firing curls immediately.
#   - /api/v1/internal/test-reset is NOT in this smoke gate: the endpoint
#     wipes client_tokens, leaving cached on-disk tokens (seeded by
#     relay-full.sh) stale, which then prevents tunnel re-registration.
#     The endpoint is covered by Go tests under -tags=integration
#     (internal/api/router_integration_test.go) and by Playwright spec 07
#     end-to-end via the access-mode + api-key UI flow.
echo "[smoke-full] sanity: dashboard"
curl -fsS http://localhost:8080/healthz >/dev/null

echo "[smoke-full] sanity: wait for 3 TCP tunnels to register"
for port in 9002 9003 9004; do
  for attempt in $(seq 1 60); do
    if curl -fsS -m 2 -o /dev/null "http://localhost:${port}/healthz"; then
      echo "  :${port} ready (after ${attempt}s)"
      break
    fi
    if [ "$attempt" = "60" ]; then
      echo "  :${port} did NOT register within 60s — aborting"
      exit 1
    fi
    sleep 1
  done
done

echo "[smoke-full] running Playwright (mock profile, 20 specs)"
cd "$DIR"
npm ci --no-fund --no-audit
npx playwright install --with-deps chromium
npx playwright test --project=mock

echo "[smoke-full] all specs passed"
