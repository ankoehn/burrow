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

- **`go ./...` traverses `web/node_modules` when present.** The `flatted` npm
  package ships a Go reference file; once `npm ci` has populated
  `web/node_modules` (e.g. the GoReleaser web build hook, or local dev),
  `go test ./...` discovers `github.com/ankoehn/burrow/web/node_modules/flatted/...`
  as `[no test files]` — benign today (compiles; node_modules is gitignored so the
  committed/CI tree is unaffected). Latent risk: a future npm dep shipping
  non-compiling Go would break local `go test ./...`. Revisit with a Go workspace
  (`go.work`) or build-tag scheme if it ever bites. _Source: Phase 4c Task 1 independent review._

## Dashboard (Phase 4c)

- **Real browser-automation e2e.** The dashboard is verified by Vitest unit tests + Go embed/router unit tests + assembled HTTP smokes (login→token→connect→/tunnels, SPA fallback, /api/v1-no-leak); a real headless-browser click-through (Playwright) is post-MVP. _Source: Phase 4c spec §6/§8._
- **Dashboard visual polish + dark/light toggle.** MVP shell is intentionally minimal (Phase-5 polish); add an explicit theme toggle, refined layout/empty-states. _Source: Phase 4c spec §6/G11._
- **Change-password / account endpoint + page.** Account is read-only this MVP (parent Phase-4 spec §7 + done-criteria omit password change); cross-refs the existing 4b backlog bullet — implement together in v0.2. _Source: Phase 4c spec §6._
- **Static-asset requests are logged at info via the API request logger.** SPA assets flow through the chi `requestLogger` (shared router); a handful of info lines per page load — acceptable for MVP, consider downgrading `/*`/`/assets/` to debug or skipping if log noise matters at scale. _Source: Phase 4c Task 7 independent code review._
