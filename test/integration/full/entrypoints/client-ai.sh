#!/usr/bin/env bash
# test-only — never deploy this shape.
set -euo pipefail
TOKEN_PATH="/run/burrow/token-ai"
for i in $(seq 1 120); do
  [ -s "$TOKEN_PATH" ] && break
  echo "[client-ai] waiting for $TOKEN_PATH ($i/120)"
  sleep 1
done
[ -s "$TOKEN_PATH" ] || { echo "[client-ai] token never appeared" >&2; exit 1; }
TOKEN="$(cat "$TOKEN_PATH")"

# mockoai listens at mockoai:8081 inside the e2e network — burrow client tunnels to it.
# BURROW_UPSTREAM defaults to mockoai:8081 via Dockerfile ENV.
exec burrow connect \
  --server "$BURROW_RELAY" \
  --token "$TOKEN" \
  --local "$BURROW_UPSTREAM" \
  --remote "$BURROW_REMOTE_PORT" \
  --name "$BURROW_TUNNEL_NAME" \
  --type http \
  --insecure
