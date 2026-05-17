#!/usr/bin/env sh
# Generate local development TLS certs. DEV ONLY.
set -e
exec go run ./cmd/gencerts "$@"
