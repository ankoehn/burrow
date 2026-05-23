# Burrow e2e full harness (`test/integration/full/`)

Comprehensive 4-container Docker Compose harness for proving Burrow's
real-deployment shape works end-to-end across v0.1–v0.5 surfaces. Sibling
of the fast PR-gating mini-suite at `test/integration/`.

## Topology

```
                  ┌────────────────────────┐
                  │  relay (burrowd)       │
                  │  :7000 control TLS     │  ← all 3 clients dial here
                  │  :8080 dashboard HTTP  │  ← browser + Playwright
                  │  :8443 HTTPS ingress   │  ← visitor traffic via *.test.local
                  │  :9002-9004 tcp tunnels│  ← curl through TCP tunnels
                  │  :7800 MCP             │
                  └────────────────────────┘
                            ▲    ▲    ▲
              ┌─────────────┘    │    └────────────────┐
              │                  │                     │
   ┌──────────────────┐  ┌──────────────────┐  ┌────────────────────┐
   │ client-ai        │  │ client-tcp       │  │ client-multi       │
   │ burrow connect   │  │ burrow connect   │  │ burrow connect     │
   │ --type http      │  │ --type tcp       │  │ --config           │
   │ → mockoai:8081   │  │ → upstream:8082  │  │ → 2 svcs (8083/4)  │
   │ host-routed via  │  │ port-bound :9002 │  │ (burrow.yaml)      │
   │ <sub>.test.local │  │                  │  │ port-bound 9003/4  │
   └──────────────────┘  └──────────────────┘  └────────────────────┘
              │
       ┌──────────────┐
       │ mockoai      │
       │ :8081        │
       │ chat / embed │
       │ / messages   │
       └──────────────┘
```

**Routing note:** HTTP tunnels (`--type http`) are *host-routed* on :8443
(the wildcard TLS proxy), not port-bound. The relay assigns each HTTP
tunnel a random subdomain like `frqd3c.test.local`; hit it via
`https://<subdomain>.test.local:8443/...`. TCP tunnels (`--type tcp`)
bind the `--remote` port directly on the relay container.

## Quick start

```bash
# Build + bring up
docker compose -f test/integration/full/compose.full.yml up -d --build --wait

# Read the AI tunnel's assigned subdomain from the relay log:
SUB=$(docker logs burrow-e2e-full-relay-1 2>&1 \
  | grep "http tunnel registered" | tail -1 \
  | grep -oE 'subdomain=[a-z0-9]+' | cut -d= -f2)

# Smoke (6 surfaces):
curl -fsS http://localhost:8080/healthz                       # dashboard
curl --ssl-no-revoke -k --resolve "$SUB.test.local:8443:127.0.0.1" \
     -X POST -H "content-type: application/json" \
     -d '{"model":"x","stream":true,"messages":[{"role":"user","content":"hi"}]}' \
     "https://$SUB.test.local:8443/v1/chat/completions"        # AI SSE
curl -fsS http://localhost:9002/healthz                       # TCP echo
curl -fsS http://localhost:9003/healthz                       # multi svc-a
curl -fsS http://localhost:9004/healthz                       # multi svc-b
curl -fsS -X POST http://localhost:8080/api/v1/internal/test-reset  # 204

# Manual browser test:
#   1. Add to /etc/hosts (or %SystemRoot%\System32\drivers\etc\hosts on Windows):
#        127.0.0.1 relay.test.local
#   2. Open https://relay.test.local:8443/ — accept the test CA in your browser,
#      OR trust certs/ca.crt at the OS level.
#   3. Sign in: admin@e2e.local / e2e-pass
#   4. Follow test/integration/full/RUNBOOK.md (Plan 2 deliverable) to verify
#      each v0.1-v0.5 surface.

# Tear down
docker compose -f test/integration/full/compose.full.yml down --volumes
```

## Postgres profile

```bash
docker compose \
  -f test/integration/full/compose.full.yml \
  -f test/integration/full/compose.full.postgres.yml \
  up -d --build --wait
```

Adds a `postgres:16-alpine` testcontainer; rebuilds the relay image with
`-tags=integration,postgres` via `GO_BUILD_TAGS`.

## Files

- `compose.full.yml` — 4 services (relay + 3 clients) + mockoai + network_aliases
- `compose.full.postgres.yml` — Postgres override (v0.5 alpha)
- `certs/` — test CA + `*.test.local` wildcard (10y, test-fixture only); regen via `bash gen.sh`
- `mockoai/` — Burrow-authored stdlib OpenAI mock (~200 LOC, nested go.mod, stdlib only)
- `Dockerfile.{relay,client-ai,client-tcp,client-multi}` — multi-stage builds
- `entrypoints/*.sh` — per-container bootstrap (token wait, upstream start, burrow connect)
- `burrow-multi.yaml` — client-multi's 2-service config (v0.3 surface)
- `RUNBOOK.md` (Plan 2) — manual browser checklist
- `package.json`, `playwright.config.ts`, `spec/` (Plan 3) — automated suite

## /test-reset endpoint

`POST /api/v1/internal/test-reset` (no auth) → 204. Truncates audit,
client tokens, automation tokens, sessions, services + per-service rows
(api-keys, access-policy, custom-domains, ai-config, upstream creds,
ip-geo), webhooks + deliveries, connection-logs + rollups, rate limits,
model aliases, budgets + alerts, cost pricing, webauthn credentials, and
non-seeded users — preserving the schema_migrations table and the
seeded admin row.

**Compiled in ONLY under `-tags=integration`.** The default release
binary contains a no-op stub (`registerIntegrationRoutes` in
`internal/api/router_integration_stub.go`). Verified absent from the
default build by `go tool nm | grep testReset` (zero matches).

After /test-reset, cached tokens on `/run/burrow/token-{ai,tcp,multi}`
become invalid until the relay re-mints them. The Playwright pattern
is: `/test-reset` → restart compose → re-seed → assert.

## Constraints

- Test-only. Never deploy this shape.
- Append-only commits on `main`; race-free `git commit -- <paths>` pattern.
- No new Go module dependencies (mockoai uses a nested go.mod, stdlib only).
- Build-tagged `/test-reset` endpoint behind `//go:build integration`;
  never in the release binary.
- ONE production-code touch: a single line in `internal/api/router.go`
  invokes `registerIntegrationRoutes(r, d)` inside the `/api/v1` closure.
  Stub vs real implementation is selected by the build tag.

## What's tested (Plan 2 + 3 cover this)

v0.1–v0.5 surfaces: tunnels, services, tokens, users, roles, access modes
(all 4), AI gateway (basic + semantic cache + metering + cost), custom
domains, connection logs, audit, webhooks, OpenAPI viewer, retention,
Postgres swap, reconnect.

## See also

- `test/integration/README.md` — basic 2-docker harness (PR-gating)
- `docs/BACKLOG_INTEGRATION.md` — design rationale + locked decisions D1-D10
