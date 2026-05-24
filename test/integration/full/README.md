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

## Playwright suite (Plan 3)

20 automated specs covering every section of RUNBOOK.md. Reuses the live stack;
specs use unique (timestamped) resource names and don't call `/test-reset`
between runs (it wipes `client_tokens` and breaks the cached on-disk tokens
that `relay-full.sh` seeded; cleanest cure is `docker compose restart`).

### Run

```bash
task e2e:full
# or directly:
bash test/integration/full/smoke-full.sh
```

### Fast iteration

```bash
docker compose -f test/integration/full/compose.full.yml up -d --build --wait
cd test/integration/full
npx playwright test                                  # all 20 specs (mock project)
npx playwright test 10-ai-gateway                    # one spec
npx playwright test --headed --workers=1             # visible browser
npx playwright show-report                           # HTML report
```

### Postgres project

```bash
docker compose -f test/integration/full/compose.full.yml -f test/integration/full/compose.full.postgres.yml up -d --build --wait
cd test/integration/full
npx playwright test --project=postgres               # only specs whose name matches /postgres/
```

### CI

GitHub Actions job `e2e-compose-full` (in `.github/workflows/ci.yml`) runs the
full pipeline on every PR. On failure uploads `playwright-report/` and
`test-results/` as the `e2e-compose-full-report` artifact for post-mortem.

### Spec inventory

| # | Spec | Surface | Status |
|---|---|---|---|
| 01 | bootstrap | login + 4 tunnels visible | ✅ |
| 02 | tunnels | bytes counters move via SSE | ✅ |
| 03 | services-burrow-yaml | v0.3 multi-service (verified via /tunnels) | ✅ |
| 04 | tokens-mint | UI write path | ✅ |
| 05 | users-roles | create + delete (built-in role) | ✅ |
| 06 | access-mode-open | unauth GET → 200 | ✅ |
| 07 | access-mode-api-key | 200/401 (via /services/<id>) | ✅ |
| 08 | access-mode-burrow-login | SSO redirect signal | ✅ |
| 09 | access-mode-mtls | UI surface check | ✅ |
| 10 | ai-gateway-basic | chat-completions SSE via :8443 | ✅ |
| 11 | ai-gateway-semantic-cache | reachability (skipped w/o `-tags=semantic_cache`) | ⏭ |
| 12 | ai-gateway-metering-cost | request metered + cost page renders | ✅ |
| 13 | custom-domains | UI surface check | ✅ |
| 14 | connection-logs | TCP sessions visible | ✅ |
| 15 | audit-chain | token.mint + verify | ✅ |
| 16 | webhooks | page + Add dialog | ✅ |
| 17 | openapi-viewer | viewer renders, no CDN scripts | ✅ |
| 18 | retention | knobs page renders | ✅ |
| 19 | postgres-swap | Postgres parity (postgres project) | ✅ |
| 20 | relay-restart | 4-client reconnect | ✅ |

19/20 active + 1 build-tag-gated skip. Total wall-clock ~50-70s.

### Plan-fidelity deviations (called out in spec headers)

- HTTP tunnels are host-routed on :8443, not port-bound on :9001 — fixtures
  use `aiHost()` + Host header.
- Access-mode and api-key flows go via `/services/<id>` (the per-service
  detail), NOT `/tunnels` Configure (which passes tunnel.id where the
  panel expects service.id — real v0.5.2 defect).
- `/api/v1/internal/test-reset` truncates `client_tokens`, leaving cached
  on-disk tokens stale. Specs in this suite are written to be idempotent
  (timestamp suffix) and do NOT reset between runs.
- `api_key_header` persists per-service; spec 07 explicitly resets the
  field to "Authorization" before saving.
- The Tabs component renders `role="tab"`/`role="tabpanel"`; the Dialog
  component renders `role="dialog"` with `aria-labelledby="dialog-title"`,
  but the SAME ID is used for nested dialogs — Playwright's
  `getByRole("dialog", {name: ...})` only matches one. Specs that open
  nested dialogs use `[role="dialog"]` + a `has: heading` filter +
  `.last()` to disambiguate.
