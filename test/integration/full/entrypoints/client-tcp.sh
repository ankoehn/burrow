#!/usr/bin/env bash
# test-only — never deploy this shape.
set -euo pipefail
TOKEN_PATH="/run/burrow/token-tcp"
for i in $(seq 1 120); do
  [ -s "$TOKEN_PATH" ] && break
  sleep 1
done
[ -s "$TOKEN_PATH" ] || { echo "[client-tcp] token never appeared" >&2; exit 1; }
TOKEN="$(cat "$TOKEN_PATH")"

# Start the stdlib upstream in background on :8082.
upstream -addr :8082 &
UP_PID=$!
for i in $(seq 1 20); do
  curl -fsS -o /dev/null "http://127.0.0.1:8082/healthz" && break
  kill -0 "$UP_PID" 2>/dev/null || { echo "[client-tcp] upstream died" >&2; exit 1; }
  sleep 1
done

exec burrow connect \
  --server "$BURROW_RELAY" \
  --token "$TOKEN" \
  --local "$UPSTREAM_ADDR" \
  --remote "$BURROW_REMOTE_PORT" \
  --name "$BURROW_TUNNEL_NAME" \
  --type tcp \
  --insecure
