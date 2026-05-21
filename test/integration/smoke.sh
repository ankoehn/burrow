#!/usr/bin/env bash
# test/integration/smoke.sh
# Brings the e2e Compose stack up, asserts the manual smoke checks from
# README.md, and tears it down on success OR failure.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
COMPOSE_FILE="test/integration/compose.e2e.yml"
cd "$REPO_ROOT"

pass=0
fail=0
fails=()

assert() {
  local name="$1"; shift
  if "$@" >/dev/null 2>&1; then
    echo "PASS  $name"
    pass=$((pass+1))
  else
    echo "FAIL  $name (cmd: $*)"
    fail=$((fail+1))
    fails+=("$name")
  fi
}

teardown() {
  echo "--- tearing down ---"
  docker compose -f "$COMPOSE_FILE" down --volumes || true
}
trap teardown EXIT

echo "--- bringing stack up (build + detached) ---"
docker compose -f "$COMPOSE_FILE" up --build -d

echo "--- waiting for client to register the tunnel ---"
# Up to 60 s. We poll via the public visitor port: if /healthz on :9000
# returns 200, the full chain (client → relay → public listener → upstream)
# is wired.
ok=""
for i in $(seq 1 60); do
  if curl -fsS -o /dev/null "http://localhost:9000/healthz"; then
    ok="y"
    echo "tunnel is live (after ${i}s)"
    break
  fi
  sleep 1
done
if [ -z "$ok" ]; then
  echo "tunnel never came up after 60s — dumping logs:"
  docker compose -f "$COMPOSE_FILE" logs --tail=80
  exit 1
fi

echo "--- assertions ---"

# 1) /healthz round-trip via the tunnel.
body="$(curl -fsS http://localhost:9000/healthz)"
assert "GET /healthz returns {\"status\":\"ok\"}" \
  bash -c "[ '$body' = '{\"status\":\"ok\"}' ]"

# 2) /echo POST round-trip with custom header + body.
echo_resp="$(curl -fsS -X POST -H 'X-T: y' -d 'hi' http://localhost:9000/echo)"
assert "POST /echo echoes the body 'hi'" \
  bash -c "echo '$echo_resp' | grep -q '\"body\":\"hi\"'"
assert "POST /echo echoes the X-T header" \
  bash -c "echo '$echo_resp' | grep -q '\"X-T\":\\[\"y\"\\]'"
assert "POST /echo reports method=POST" \
  bash -c "echo '$echo_resp' | grep -q '\"method\":\"POST\"'"

# 3) Dashboard /healthz over plain HTTP is 200.
# (Under --dev-certs, --dev-certs only secures :7000; dashboard :8080 stays HTTP.)
assert "Dashboard http://localhost:8080/healthz returns 200" \
  curl -fsS -o /dev/null "http://localhost:8080/healthz"

echo "--- result ---"
echo "passed: $pass   failed: $fail"
if [ "$fail" -gt 0 ]; then
  printf '  - %s\n' "${fails[@]}"
  exit 1
fi
echo "ALL GREEN"
