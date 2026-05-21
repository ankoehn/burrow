#!/usr/bin/env bash
# test/integration/client-entrypoint.sh
# Waits for the relay to write /run/burrow/token, starts the upstream service
# in the background, then runs `burrow connect` (foreground).

set -euo pipefail

TOKEN_PATH="/run/burrow/token"

echo "[client-entrypoint] waiting for $TOKEN_PATH (relay bootstraps this)"
for i in $(seq 1 120); do
  if [ -s "$TOKEN_PATH" ]; then
    echo "[client-entrypoint] token present (after ${i}s)"
    break
  fi
  sleep 1
done
if [ ! -s "$TOKEN_PATH" ]; then
  echo "[client-entrypoint] giving up — $TOKEN_PATH not present after 120s" >&2
  exit 1
fi
TOKEN="$(cat "$TOKEN_PATH")"

echo "[client-entrypoint] starting upstream on $UPSTREAM_ADDR (background)"
/usr/local/bin/upstream --addr "$UPSTREAM_ADDR" &
UPSTREAM_PID=$!

# Brief readiness wait for the upstream.
for i in $(seq 1 20); do
  if curl -fsS -o /dev/null "http://$UPSTREAM_ADDR/healthz"; then
    echo "[client-entrypoint] upstream /healthz is up (after ${i}*0.5s)"
    break
  fi
  if ! kill -0 "$UPSTREAM_PID" 2>/dev/null; then
    echo "[client-entrypoint] upstream exited before /healthz came up" >&2
    exit 1
  fi
  sleep 0.5
done

echo "[client-entrypoint] running burrow connect → $BURROW_RELAY (insecure, dev-certs)"
exec /usr/local/bin/burrow connect \
  --server "$BURROW_RELAY" \
  --token  "$TOKEN" \
  --local  "$UPSTREAM_ADDR" \
  --remote "$BURROW_REMOTE_PORT" \
  --name   "$BURROW_TUNNEL_NAME" \
  --type   tcp \
  --insecure
