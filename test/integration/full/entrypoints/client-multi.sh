#!/usr/bin/env bash
# test-only — never deploy this shape.
set -euo pipefail
TOKEN_PATH="/run/burrow/token-multi"
for i in $(seq 1 120); do
  [ -s "$TOKEN_PATH" ] && break
  sleep 1
done
[ -s "$TOKEN_PATH" ] || { echo "[client-multi] token never appeared" >&2; exit 1; }

# Start 2 upstream instances on :8083 and :8084 (matches burrow-multi.yaml services).
upstream -addr :8083 &
upstream -addr :8084 &
sleep 1

# burrow.yaml's token_file: points at /run/burrow/token-multi; the --config
# loader reads the token from disk. The --insecure flag is a CLI flag (not
# a yaml field) for this in-cluster test-CA setup.
exec burrow connect --config /etc/burrow/burrow.yaml --insecure
