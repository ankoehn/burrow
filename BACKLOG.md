# Backlog

Out-of-scope-but-noted improvements deferred per MVP phase discipline
(see `docs/MVP_PHASES.md` — "If you find yourself reaching for one of these
mid-MVP, write it down in `BACKLOG.md` and keep going.").

## Database / persistence

- **Offset-proof datetime comparison in `internal/db`.** `DeleteExpiredSessions`
  currently relies on lexical string comparison of Go `time.Time.String()`
  values, which is correct only because `internal/store` consistently writes
  `time.Now().UTC()` (Phase 4a, commit `90699d2`). Two latent fragilities
  remain, neither reachable on a fresh single-TZ MVP install:
  1. A future query that compares a Go-time bound parameter against a
     `CURRENT_TIMESTAMP`/`DEFAULT` column (different lexical formats:
     `2026-05-17 17:00:15` vs `2026-05-17 17:00:15.5 +0000 UTC`) would silently
     mis-sort.
  2. Rows written by pre-`90699d2` (non-UTC) code are not purged after a
     server moves to UTC — a DB-upgrade scenario only.
  **Recommended fix:** compare in SQL via `expires_at <= datetime('now')` /
  `strftime`, or store expiry as a unix-epoch `INTEGER`. Removes the entire
  Go-`String()`-vs-`CURRENT_TIMESTAMP`-vs-offset fragility class.
  _Source: Phase 4a Task 5 independent code review._

## Testing

- **Restore TLS cert/key required-validation coverage.** Phase 4a Task 7 replaced
  `TestServerValidationRequiresToken` (which implicitly exercised the validator
  firing) with `TestServerNoTokenRequired`. The `validate:"required"` tags on
  `ServerConfig.TLSCert`/`TLSKey` are unchanged, but no test now explicitly
  asserts `LoadServer` errors when they are empty. Add a `TestServerTLSRequired`
  (e.g. `LoadServer(map[string]any{"tls_cert":"","tls_key":""})` → error).
  _Source: Phase 4a Task 7 independent code review._

## HTTP API security (Phase 4b)

- **Session row IP hardening.** `api.Login` stores `r.Request.RemoteAddr` (host:port,
  and post-`middleware.RealIP` it is the X-Forwarded-For value, spoofable on a
  direct-internet deployment) into `sessions.ip`. For MVP single-admin this is
  informational only. Hardening: strip the port via `net.SplitHostPort`, and only
  trust forwarded headers behind a configured trusted proxy. Document the proxy-trust
  assumption. _Source: Phase 4b Task 4 independent code review._

- **Login rate-limiting.** `POST /api/v1/auth/login` is unauthenticated and has no rate limit; brute-force/credential-stuffing is unthrottled. MVP single-admin lowers impact but add per-IP/global throttling (e.g. `httprate`) before multi-user. _Source: Phase 4b spec §6/F10._

- **CSRF tokens for state-changing endpoints.** Session is a `SameSite=Lax` cookie; Lax + the same-origin embedded SPA (4c) blocks basic cross-site POST CSRF for the MVP, but add explicit CSRF tokens (or double-submit) before exposing the API cross-origin. _Source: Phase 4b spec §6/F10._

- **First-class HTTPS / `Secure` cookie / HSTS for the API.** MVP serves plain HTTP on `:8080` behind an operator TLS-terminating proxy (`http_secure_cookies` default false). Add native TLS for the HTTP server, set `Secure` cookies + HSTS when terminating TLS itself. _Source: Phase 4b spec §6/F10._

- **Change-password / account endpoint + page.** `docs/MVP_PHASES.md` lists an Account page, but the parent Phase-4 spec §7 4b handler list and the Phase-4 done-criteria omit password change; MVP `/me` is read-only. Defer to v0.2. _Source: Phase 4b spec §6._

## HTTP API lifecycle (Phase 4b)

- **API graceful-shutdown should fully drain JSON handlers before DB close.**
  `burrowd serve` calls `apiSrv.Shutdown(5s)` then `srv.Wait()` then (deferred)
  `database.Close()`. The JSON route group has a 30s chi `middleware.Timeout`, so a
  handler force-unblocked by chi at up to 30s can outlive the 5s `Shutdown` window
  and (very rarely, under SQLite stall) be mid-`GetSession` when `database.Close()`
  runs — handled (returns `sql: database is closed` → 500), not memory-unsafe, but
  untidy. Hardening: align the `Shutdown` deadline with the handler timeout, or
  close the DB only after the API drain goroutine confirms. _Source: Phase 4b Task 9 independent code review._

## Build tooling

- **`go ./...` traversed `web/node_modules` when present — RESOLVED 2026-05-18.**
  The `flatted` npm package ships a Go reference file; once `npm ci` populated
  `web/node_modules`, `go test ./...` / `go vet ./...` discovered
  `github.com/ankoehn/burrow/web/node_modules/flatted/...`. Fix: the test/vet
  entrypoints (`Makefile`, `Taskfile.yml`, CI) now enumerate the real module
  roots `./cmd/... ./internal/... ./web` instead of `./...`. `./web` is
  non-recursive, so the single `web` embed package stays covered while the npm
  subtree is structurally unreachable — no external tools, identical on
  Linux/macOS/Windows. Rejected alternatives: a nested `web/go.mod` (orphans
  `web/embed.go`+tests from the main module that `cmd` imports); a `go.work`
  workspace (does **not** prune a module's own subtree walk); a `//go:build
  ignore` tag (un-committable — `web/node_modules` is gitignored and
  npm-regenerated). Tradeoff: a future top-level Go dir must be added to these
  targets. _Source: Phase 4c Task 1 independent review; resolved Phase 5._

## Dashboard (Phase 4c)

- **Real browser-automation e2e.** The dashboard is verified by Vitest unit tests + Go embed/router unit tests + assembled HTTP smokes (login→token→connect→/tunnels, SPA fallback, /api/v1-no-leak); a real headless-browser click-through (Playwright) is post-MVP. _Source: Phase 4c spec §6/§8._
- **Dashboard visual polish + dark/light toggle.** MVP shell is intentionally minimal (Phase-5 polish); add an explicit theme toggle, refined layout/empty-states. _Source: Phase 4c spec §6/G11._
- **Change-password / account endpoint + page.** Account is read-only this MVP (parent Phase-4 spec §7 + done-criteria omit password change); cross-refs the existing 4b backlog bullet — implement together in v0.2. _Source: Phase 4c spec §6._
- **Static-asset requests are logged at info via the API request logger.** SPA assets flow through the chi `requestLogger` (shared router); a handful of info lines per page load — acceptable for MVP, consider downgrading `/*`/`/assets/` to debug or skipping if log noise matters at scale. _Source: Phase 4c Task 7 independent code review._

## Deployment / packaging (Phase 5)

- **Support the `*_FILE` secret convention.** The MVP binary reads
  `BURROW_ADMIN_PASSWORD` (and tokens) only from the literal env var. The
  original `docker-compose.yml` used `BURROW_ADMIN_PASSWORD_FILE` + Docker
  `secrets:`, which the binary never implemented (admin silently not seeded).
  Fixed by switching the example to `BURROW_ADMIN_PASSWORD`. Add a generic
  `<VAR>_FILE` indirection (if `<VAR>_FILE` is set, read that file into
  `<VAR>`) so Docker/Swarm/K8s file secrets work without exposing values in
  the environment — v0.2. _Source: Phase 5 P5._
- **Smoother first-run container UX.** `burrowd serve` requires TLS certs; the
  compose example passes `--dev-certs` and sets `working_dir: /data` so
  self-signed certs land on the writable volume, and `./data` must be
  pre-chowned to the non-root uid (65532). A first-run entrypoint that
  bootstraps the cert/data directories (plus a documented production path for
  real certs) would remove these caveats — v0.2. _Source: Phase 5 P5._

## Release pipeline hardening (Phase 5, v0.2)

Non-blocking items surfaced by the Phase 5 per-task code reviews. None block
the v0.1.0 tag; track for v0.2.

- **Narrow the release tag glob.** `release.yml` triggers on `tags: ["v*"]`,
  which also matches non-semver tags (`vfoo`, `v-test`). Tighten to
  `["v[0-9]*"]` to eliminate accidental full release+GHCR+sign runs.
  _Source: Phase 5 Task 3 review._
- **Add a `concurrency:` group** (`group: release-${{ github.ref }}`,
  `cancel-in-progress: true`) to `release.yml` and `release-dryrun.yml` so a
  rapid re-tag does not race two publishing runs. _Source: Phase 5 Task 3/4 review._
- **Add `timeout-minutes`** (~30) to the release and dry-run jobs to cap a
  hung QEMU multi-arch build (default GH timeout is 6h).
  _Source: Phase 5 Task 4 review._
- **SHA-pin GitHub Actions.** Actions are major-tag pinned (`@v4` …); pin to
  immutable commit SHAs for supply-chain hardening. _Source: Phase 5 Task 3 review._
- **Tag protection / required CI on the tagged commit.** A tag on a broken
  commit would still trigger a publish. Add a GitHub tag-protection rule or a
  required-status gate before release. _Source: Phase 5 Task 3 review._
- **Migrate `.goreleaser.yml` `dockers:`/`docker_manifests:` to the new
  `dockers_v2:` schema** before a goreleaser release that removes the
  deprecated keys. Non-blocking for v0.1.0: the `~> v2` pin keeps goreleaser
  on v2.x (which still supports `dockers:` with only a deprecation warning);
  removal would land in goreleaser v3, which `~> v2` never selects. Do the
  migration when bumping to goreleaser v3 / `goreleaser-action` v7+.
  _Source: Phase 5 Task 2 review._
- **`linux/arm` archive naming.** Archives use `{{ .Arch }}` → `arm` for the
  armv7 build (`burrow_linux_arm_<ver>.tar.gz`); unambiguous (only v7 is
  built) but a downloader can't tell v6/v7 from the name. Consider emitting
  `armv7` in `archives.name_template`. _Source: Phase 5 Task 2 review._
