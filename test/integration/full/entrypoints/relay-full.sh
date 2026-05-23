#!/usr/bin/env bash
# test-only — never deploy this shape.
# test/integration/full/entrypoints/relay-full.sh
# Boots burrowd with the test wildcard cert (mounted at /certs), seeds an
# admin user, mints 3 client tokens (one per client container), writes
# them to the shared /run/burrow/ volume.
set -euo pipefail
: "${BURROW_ADMIN_EMAIL:=admin@e2e.local}"
: "${BURROW_ADMIN_PASSWORD:=e2e-pass}"
export BURROW_ADMIN_EMAIL BURROW_ADMIN_PASSWORD

mkdir -p /run/burrow

echo "[relay-full] starting burrowd serve --dev-certs (admin=$BURROW_ADMIN_EMAIL)"
# --dev-certs handles :7000 control-plane TLS; the wildcard cert mounted at
# /certs secures :8443 visitor traffic (BURROW_HTTPS_PROXY_TLS_*).
burrowd serve --dev-certs &
SERVE_PID=$!

# Poll dashboard /healthz (plain HTTP under --dev-certs).
for i in $(seq 1 60); do
  if curl -fsS -o /dev/null http://127.0.0.1:8080/healthz; then
    echo "[relay-full] dashboard up after ${i}s"
    break
  fi
  kill -0 "$SERVE_PID" 2>/dev/null || { echo "[relay-full] burrowd died early"; exit 1; }
  sleep 1
done

# Mint 3 tokens, one per client container.
for name in ai tcp multi; do
  TOKEN="$(burrowd token --email "$BURROW_ADMIN_EMAIL" --name "e2e-${name}")"
  if [ -z "$TOKEN" ]; then
    echo "[relay-full] empty token for ${name}" >&2
    exit 1
  fi
  echo "$TOKEN" > "/run/burrow/token-${name}"
  echo "[relay-full] token-${name} written ($(wc -c < "/run/burrow/token-${name}") bytes)"
done

wait "$SERVE_PID"
