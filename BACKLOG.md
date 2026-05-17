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
