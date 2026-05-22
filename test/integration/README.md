# Basic e2e Docker Compose harness

Two-container stack proving a real `burrow connect → relay → public TCP tunnel
→ upstream` round-trip on a separate process boundary. Foundation for the
future Playwright harness specified in `docs/BACKLOG_INTEGRATION.md`.

**Test-only.** `burrowd serve --dev-certs` + client `--insecure` — never deploy this shape.
Under `--dev-certs`, the `:7000` control plane uses self-signed TLS but the `:8080`
dashboard stays plain HTTP. Set `BURROW_HTTP_TLS_CERT` / `BURROW_HTTP_TLS_KEY` for
native dashboard HTTPS in production.

## What it builds

| Service  | Image                    | Purpose                                                |
|----------|--------------------------|--------------------------------------------------------|
| `relay`  | `burrow-e2e-relay:dev`   | `burrowd serve --dev-certs` + token mint               |
| `client` | `burrow-e2e-client:dev`  | tiny HTTP upstream on `127.0.0.1:8081` + `burrow connect` |

Shared volume `token-share` carries the minted client token from relay to
client. The visitor port `9000` is published on the host.

## Prereqs

- Docker Engine ≥ 24
- Docker Compose v2 (`docker compose` — with a space; not `docker-compose`)
- Ports `8080` and `9000` free on the host

## Bring it up

```
docker compose -f test/integration/compose.e2e.yml up --build
```

Expected log sequence (relay first, then client):

1. `[relay-entrypoint] starting burrowd serve --dev-certs (admin=admin@e2e.local)`
2. `[relay-entrypoint] polling http://127.0.0.1:8080/healthz`
3. `[relay-entrypoint] burrowd /healthz is up (after Ns)`
4. `[relay-entrypoint] minting token via 'burrowd token'`
5. `[relay-entrypoint] token written to /run/burrow/token (N bytes)`
6. `[client-entrypoint] token present (after Ns)`
7. `[client-entrypoint] upstream /healthz is up (after Ms)`
8. `[client-entrypoint] running burrow connect → relay:7000 (insecure, dev-certs)`
9. `level=INFO msg=connected session_id=...` and `level=INFO msg="tunnel registered" tunnel_id=... remote_port=9000` from the burrow client.
10. `level=INFO msg="client authenticated" session_id=... remote_addr=...` and `level=INFO msg="tunnel registered" tunnel_id=... remote_port=9000` from the relay.

If any of those don't appear within ~20 s, see Troubleshooting.

## Manual smoke (from another terminal)

```
curl -fsS http://localhost:9000/healthz
# → {"status":"ok"}

curl -fsS -X POST -H 'X-T: y' -d 'hi' http://localhost:9000/echo
# → {"body":"hi","headers":{...,"X-T":["y"]},"method":"POST","path":"/echo"}
```

Dashboard (optional, manual eyeballing):

- `http://localhost:8080/` — plain HTTP under `--dev-certs`.
- Sign in: `admin@e2e.local` / `e2e-pass`.
- **Clients** / **Tunnels**: one connected client, one tunnel
  `upstream` → remote port `9000`.

## Automated smoke

```
./test/integration/smoke.sh
```

Brings the stack up, asserts the same `curl` checks above, and reports
green/red. Tears the stack down on success **and** on failure. See
`smoke.sh` for the exact assertions.

## Tear down

```
docker compose -f test/integration/compose.e2e.yml down --volumes
```

`--volumes` removes the token-share + relay-data named volumes so the next
run starts clean.

## Troubleshooting

- **Port already in use (`8080` or `9000`)**: stop whatever else is bound
  there, or edit the `ports:` lines in `compose.e2e.yml`.
- **`burrowd /healthz` never comes up**: re-run with `docker compose ... up
  --build --no-attach client` to inspect only the relay's logs. Confirm the
  poll uses `http://` (not `https://`) — `--dev-certs` does not enable TLS
  on `:8080`.
- **Relay healthy but client never starts**: check `docker compose ps`. If
  the relay shows `(unhealthy)` despite logs looking fine, verify the
  healthcheck inside `compose.e2e.yml` probes `http://` (commit `eb8a375`
  fixed the original `https://` mismatch).
- **Client says `token not present`**: the relay's `burrowd token` call
  failed. Exec into the relay (`docker compose exec relay sh`) and run it
  by hand: `burrowd token --email admin@e2e.local --name debug`.
- **`curl` to `:9000` hangs**: the client side likely failed to register
  the tunnel. Check the client logs for the `tunnel registered ... remote_port=9000` line.

## Constraints (don't change without coordination)

- **TCP tunnel only.** HTTP-mode + `auth_domain` + wildcard TLS = the bigger
  `BACKLOG_INTEGRATION.md` harness (post-v0.5.0).
- **No new Go module deps.** Upstream is stdlib only.
- **No host-published ports on the client container.** It connects outbound.
- **Token via shared named volume.** Not via env, not via HTTP API.
- **Dashboard is plain HTTP under `--dev-certs`.** Setting
  `BURROW_HTTP_TLS_CERT` / `BURROW_HTTP_TLS_KEY` switches it to HTTPS but
  the harness intentionally doesn't — keep it minimal.

## Playwright UI mini-suite (this directory)

In addition to the curl-only `smoke.sh` data-plane gate, this directory
also hosts a small Playwright suite that exercises the dashboard UI
against the live 2-docker stack.

### What it catches that `smoke.sh` doesn't

- Dashboard renders against real cross-container TLS handshake.
- The seeded tunnel from `client-entrypoint.sh` appears in `/tunnels`
  with status `connected`.
- Real bytes through the tunnel make the `bytes_in`/`bytes_out` counters
  on `/tunnels` increment via SSE (covers the SSE event pipeline
  end-to-end).
- The `/tokens` UI mint flow succeeds and reveals a `bur_*` plaintext.
- A UI-initiated token mint reaches the audit chain (`token.mint`).
- The client reconnects after a `docker compose restart relay`, and the
  dashboard reflects the recovery via SSE.

These are surfaces that the in-process Playwright suite in `web/e2e/`
cannot exercise — it boots `burrowd` in the same process as the test
runner, so there is no real network and no real container boundary.

### How to run

Full suite (brings stack up, runs 5 specs, tears down):

```bash
task e2e:ui
# or directly:
bash test/integration/smoke-ui.sh
```

Fast local iteration (leave stack up, run specs repeatedly):

```bash
task e2e:up                         # leave stack up in a foreground window
# in another shell:
cd test/integration
npx playwright test                 # all 5 specs
npx playwright test 03-token-mint   # single spec
npx playwright test --headed --workers=1   # with a visible browser
npx playwright show-report          # open the HTML report
```

Tear down:

```bash
task e2e:down
```

### CI

A GitHub Actions job `e2e-compose-ui` (in `.github/workflows/ci.yml`)
runs the full pipeline on every PR. On failure, it uploads the
`playwright-report/` and `test-results/` directories as the
`e2e-compose-ui-playwright-report` artifact for post-mortem.

### Troubleshooting

- **`Container burrow-e2e-relay-1 Unhealthy` during `--wait`** — the
  dashboard `/healthz` polled in the relay healthcheck is **plain
  HTTP**, not HTTPS. See `relay-entrypoint.sh` and `compose.e2e.yml`
  for the canonical shape. The fix is committed under `eb8a375`.
- **A spec times out waiting for `connected`** — the seeded client may
  have failed to dial the relay. Check `docker compose logs client`.
- **`bytes_in` counter doesn't move in spec 02** — verify the SSE
  stream is open: `curl http://localhost:8080/api/v1/events` should
  emit `event: tunnels` lines as traffic flows. Note: the spec
  deliberately sends a `connection: close` header per request so
  `bridge.Pipe`'s deferred byte counter flushes between requests.
- **Spec 04 fails with "0 rows with Action=token.mint"** — the audit
  emit site may have moved. Search:
  `git grep -n "ActionTokenMint" internal/`. The constant lives in
  `internal/audit/actions.go`.

### Pinned constraints

This entire directory follows the strict scope discipline of the
parent harness: ONLY `test/integration/**` and additive entries in
`Taskfile.yml` and `.github/workflows/ci.yml`. No production code is
touched. No new Go module dependencies. Three new npm devDependencies
in `test/integration/package.json`: `@playwright/test` (version-aligned
with `web/`), plus `typescript` and `@types/node` required by the
tsconfig's `types` field and the `tsc --noEmit` gate.
