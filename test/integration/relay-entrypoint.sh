#!/usr/bin/env bash
# test/integration/relay-entrypoint.sh
# Boots burrowd with dev certs, polls its own /healthz, mints a token,
# writes it to the shared /run/burrow/token volume, then waits on burrowd.

set -euo pipefail

: "${BURROW_ADMIN_EMAIL:=admin@e2e.local}"
: "${BURROW_ADMIN_PASSWORD:=e2e-pass}"
export BURROW_ADMIN_EMAIL BURROW_ADMIN_PASSWORD

TOKEN_PATH="/run/burrow/token"
mkdir -p "$(dirname "$TOKEN_PATH")"

echo "[relay-entrypoint] starting burrowd serve --dev-certs (admin=$BURROW_ADMIN_EMAIL)"
burrowd serve --dev-certs &
SERVE_PID=$!

# Poll the dashboard /healthz over plain HTTP — --dev-certs only secures the :7000 control plane.
echo "[relay-entrypoint] polling http://127.0.0.1:8080/healthz"
for i in $(seq 1 60); do
  if curl -fsS -o /dev/null "http://127.0.0.1:8080/healthz"; then
    echo "[relay-entrypoint] burrowd /healthz is up (after ${i}s)"
    break
  fi
  if ! kill -0 "$SERVE_PID" 2>/dev/null; then
    echo "[relay-entrypoint] burrowd exited before /healthz came up" >&2
    exit 1
  fi
  sleep 1
done

# Mint a client token for the e2e admin user. Writes the bur_* string to stdout.
echo "[relay-entrypoint] minting token via 'burrowd token'"
TOKEN="$(burrowd token --email "$BURROW_ADMIN_EMAIL" --name e2e-default)"
if [ -z "$TOKEN" ]; then
  echo "[relay-entrypoint] burrowd token produced empty output" >&2
  exit 1
fi
echo "$TOKEN" > "$TOKEN_PATH"
echo "[relay-entrypoint] token written to $TOKEN_PATH ($(wc -c < "$TOKEN_PATH") bytes)"

# Hand control back to burrowd.
wait "$SERVE_PID"
