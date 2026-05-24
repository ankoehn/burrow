# Burrow e2e full harness — manual runbook

Walk-through checklist that proves every v0.1–v0.5 UI surface works against the live 4-container stack from Plan 1. Each section:
1. Sets up state (URL, login, prerequisite).
2. Describes the click-path.
3. Asserts expected UI.
4. Captures findings (✅ pass / ⚠ gotcha / ❌ defect).

Plan 3's Playwright suite codifies each section as an automated spec.

## Conventions

- **Browser:** any Chromium-based (Chrome, Edge, Brave). Firefox/Safari out of scope.
- **Admin dashboard:** `http://localhost:8080/`.
- **HTTPS proxy / visitor-facing surfaces:** `https://<subdomain>.test.local:8443/` (host-routed via the wildcard cert mounted into the relay container; HTTP tunnels get a random subdomain at registration time — read it from `docker logs burrow-e2e-full-relay-1 | grep "http tunnel registered"`).
- **TCP tunnels:** `http://localhost:<port>/...` where `<port>` is the `--remote` value (9002 for tcp-echo, 9003/9004 for multi-svc-a/b).
- **Login:** `admin@e2e.local` / `e2e-pass` (seeded by `relay-full.sh`).
- **Reset between sections:** `curl -X POST http://localhost:8080/api/v1/internal/test-reset` (build-tagged endpoint from Plan 1 T18). Truncates audit, tokens, sessions, services + per-service rows, webhooks, connection-logs, rate limits, budgets, etc. — preserves seeded admin + schema migrations. ⚠ After /test-reset, cached tokens on `/run/burrow/token-{ai,tcp,multi}` become invalid until the relay re-mints them; the cleanest follow-up is `docker compose restart` to re-seed.

## Pre-flight

Bring up the stack and verify the proven smoke shape before starting:

```bash
docker compose -f test/integration/full/compose.full.yml up -d --wait

# Discover the AI tunnel's auto-assigned subdomain (changes per boot):
AI=$(docker logs burrow-e2e-full-relay-1 2>&1 \
  | grep "http tunnel registered" | tail -1 \
  | grep -oE 'subdomain=[a-z0-9]+' | cut -d= -f2)
echo "AI subdomain: $AI"

# Proven 6-surface smoke (must all pass before starting):
curl -fsS http://localhost:8080/healthz                       # [1/6] dashboard
curl --ssl-no-revoke -k --resolve "$AI.test.local:8443:127.0.0.1" \
     -fsS -X POST -H "content-type: application/json" \
     -d '{"model":"x","stream":true,"messages":[{"role":"user","content":"hi"}]}' \
     "https://$AI.test.local:8443/v1/chat/completions"        # [2/6] AI HTTP tunnel SSE
curl -fsS http://localhost:9002/healthz                       # [3/6] TCP echo
curl -fsS http://localhost:9003/healthz                       # [4/6] multi svc-a
curl -fsS http://localhost:9004/healthz                       # [5/6] multi svc-b
curl -fsS -X POST http://localhost:8080/api/v1/internal/test-reset  # [6/6] 204
```

⚠ The original plan's pre-flight references `curl http://localhost:9001/healthz`. That is incorrect: the AI tunnel is `--type http` and is host-routed on :8443, not port-bound on :9001. Use the `$AI.test.local:8443` variant above.

Optional — add to `/etc/hosts` (or `C:\Windows\System32\drivers\etc\hosts` on Windows; requires admin) for browser-driven testing of host-routed surfaces:
```
127.0.0.1 relay.test.local client-ai.test.local client-tcp.test.local client-multi.test.local test.local
```

Optional — trust the test CA at the OS level to eliminate browser warnings:
- Linux: `sudo cp test/integration/full/certs/ca.crt /usr/local/share/ca-certificates/burrow-test-ca.crt && sudo update-ca-certificates`
- macOS: `sudo security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain test/integration/full/certs/ca.crt`
- Windows: import `certs\ca.crt` into "Trusted Root Certification Authorities" via certmgr.msc

## Table of contents

1. [Bootstrap (login, password change, sidebar nav)](#1-bootstrap)
2. [Tunnels (3 clients visible, bytes flow)](#2-tunnels)
3. [Services (burrow.yaml multi-service)](#3-services)
4. [Tokens (UI mint + use)](#4-tokens)
5. [Users + Roles (CRUD)](#5-users--roles)
6. [Access modes (open, api_key, burrow_login, mTLS)](#6-access-modes)
7. [AI Gateway basic (chat-completions, metering, rate limit, cost)](#7-ai-gateway-basic)
8. [AI Gateway depth (semantic cache, guardrail, redaction)](#8-ai-gateway-depth)
9. [Custom domains (per-service CNAME + cert pair)](#9-custom-domains)
10. [Connection logs (drive TCP traffic → entry + NDJSON export)](#10-connection-logs)
11. [Audit + Webhooks + OpenAPI viewer + Retention](#11-audit--webhooks--openapi--retention)
12. [Postgres swap (compose.full.postgres.yml profile)](#12-postgres-swap)
13. [Reconnect (restart relay container)](#13-reconnect)

## Sign-off

After all 13 sections pass, append:

> Run-through completed on YYYY-MM-DD by &lt;NAME&gt; against commit &lt;SHA&gt;. All sections ✅ except &lt;list&gt;. Findings filed at &lt;issues&gt;.

---

## 1. Bootstrap

**Goal:** Confirm login + sidebar nav + password change flow.

### Steps
1. Open `http://localhost:8080/` → expect redirect to `/login`.
2. Sign in: `admin@e2e.local` / `e2e-pass`.
3. Land on `/tunnels`. Sidebar shows: Tunnels, Services, Tokens, Clients, Users, Roles, AI endpoints, Cache, Guardrails, Cost, Audit log, Webhooks, Settings.
4. Click avatar → Account → Change password → new password `e2e-pass-2` → save → toast "Password updated".
5. Sign out → sign in with new password → success.
6. Reset for subsequent sections: `curl -X POST http://localhost:8080/api/v1/internal/test-reset` (password reverts to seeded). ⚠ After reset, `docker compose restart` is the cleanest way to also re-seed the on-disk token files so tunnels reconnect cleanly.

### Expected ✅
- Dashboard renders without console errors (DevTools Console clean).
- All sidebar links route correctly.

### Common gotchas ⚠
- Session cookie survives test-reset (admin user is re-seeded with the same ID). If logged out, re-login.
- The exact sidebar item list is set in the SPA; the textual labels above are illustrative. Mismatches should be captured in Findings, not treated as a defect of the runbook.

### Findings
- [ ] ✅ / ⚠ / ❌ — fill in during run-through.

---

## 2. Tunnels

**Goal:** All 3 client containers + their 4 tunnels (1 from client-ai, 1 from client-tcp, 2 from client-multi) appear on `/tunnels` with status=connected; bytes counters move under traffic.

### Steps
1. Navigate `/tunnels`. Confirm 4 rows:
   - `ai` (http; random subdomain, e.g. `qgnh4v.test.local` — no fixed port)
   - `tcp-echo` (tcp; remote :9002)
   - `svc-a` (tcp; remote :9003)
   - `svc-b` (tcp; remote :9004)
2. All 4 rows show `connected` badge.
3. Initial `In` and `Out` values are small (≤ a few KB from the connect handshake).
4. Drive traffic from a shell:
   ```bash
   for i in 1 2 3 4 5; do curl -fsS http://localhost:9002/healthz; done
   AI=$(docker logs burrow-e2e-full-relay-1 | grep "http tunnel registered" | tail -1 | grep -oE 'subdomain=[a-z0-9]+' | cut -d= -f2)
   curl --ssl-no-revoke -k --resolve "$AI.test.local:8443:127.0.0.1" \
        -fsS -X POST -H "content-type: application/json" \
        -d '{"model":"x","stream":true,"messages":[{"role":"user","content":"hi"}]}' \
        "https://$AI.test.local:8443/v1/chat/completions" > /dev/null
   ```
5. Within 5s the `tcp-echo` row's `In`/`Out` cells should increase (SSE event `tunnels` fires); the `ai` row should also tick up.

### Expected ✅
- 4 rows visible.
- All connected.
- Bytes counters move within 5s of driving traffic.

### Gotchas ⚠
- If `connected` badge doesn't appear, check `docker compose logs client-ai|client-tcp|client-multi` — the client may be in backoff retry loop (typically after a /test-reset wiped tokens).
- HTTP tunnels (`ai`) do NOT have a fixed `--remote` port — they're host-routed on :8443. The "Remote" column may render `—` or the assigned subdomain.

### Findings
- [ ]

---

## 3. Services (burrow.yaml multi-service)

**Goal:** Confirm v0.3 burrow.yaml multi-service surface — `client-multi` exposes 2 services through one client process.

### Steps
1. Navigate `/services`. Two rows under the `client-multi` client: `svc-a` (remote :9003) and `svc-b` (remote :9004).
2. Click `svc-a` → service detail page renders (per-service tabs: Overview, Access, AI config if applicable, Connection logs).
3. Drive traffic through both:
   ```bash
   curl -fsS http://localhost:9003/healthz
   curl -fsS http://localhost:9004/healthz
   ```
4. Both services' bytes counters move independently.

### Expected ✅
- Both services visible under one client.
- Per-service bytes counters move.

### Gotchas ⚠
- The basic upstream binary (`test/integration/upstream/main.go`) only exposes `/healthz` and `/echo`. Other paths return 404 from upstream; that is upstream behavior, not a tunnel defect.

### Findings
- [ ]

---
