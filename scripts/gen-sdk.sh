#!/usr/bin/env bash
# Generates TypeScript-fetch and Go SDK clients from docs/openapi.yaml using
# openapi-generator-cli. The operator must install openapi-generator-cli
# separately (Java runtime + the generator JAR); Burrow does NOT bundle it
# and Go tests never invoke this script.
#
# Per v0.4.0 spec Q9, CI automation of SDK generation is deferred to v0.5.
# Until then, run this from the repo root when bumping the API surface:
#
#     bash scripts/gen-sdk.sh
#
# Output layout:
#     sdk/ts/     TypeScript fetch client
#     sdk/go/     Go client
#
# Install openapi-generator-cli:
#     https://openapi-generator.tech/docs/installation
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SPEC="${ROOT}/docs/openapi.yaml"
OUT_TS="${ROOT}/sdk/ts"
OUT_GO="${ROOT}/sdk/go"

if [[ ! -f "${SPEC}" ]]; then
    echo "error: ${SPEC} not found" >&2
    exit 1
fi

if ! command -v openapi-generator-cli >/dev/null 2>&1; then
    cat >&2 <<'EOF'
openapi-generator-cli not found on PATH.

Install it first:
    https://openapi-generator.tech/docs/installation

For example (npm):
    npm install -g @openapitools/openapi-generator-cli

This script intentionally has no Go dependency on the generator — SDK
generation is an operator workflow, not a build step. CI integration is
deferred to v0.5 (spec Q9).
EOF
    exit 1
fi

mkdir -p "${OUT_TS}" "${OUT_GO}"

echo "==> Generating TypeScript-fetch SDK → ${OUT_TS}"
openapi-generator-cli generate \
    -i "${SPEC}" \
    -g typescript-fetch \
    -o "${OUT_TS}" \
    --additional-properties=supportsES6=true,withInterfaces=true

echo "==> Generating Go SDK → ${OUT_GO}"
openapi-generator-cli generate \
    -i "${SPEC}" \
    -g go \
    -o "${OUT_GO}" \
    --additional-properties=packageName=burrowclient

echo "==> Done."
echo "    TypeScript: ${OUT_TS}"
echo "    Go:         ${OUT_GO}"
