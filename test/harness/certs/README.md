# Test CA & wildcard cert — TEST FIXTURES ONLY

These cert files are committed to the repo for the e2e full harness. They are:

- **Self-signed, never CA-trusted in production.**
- **Valid for `*.test.local` and `test.local` only** — these hostnames never resolve outside the harness.
- **Regenerable** via `bash gen.sh` (requires `openssl`).
- **Rotated per release** if a check-in flags them as stale (`openssl x509 -in wildcard.test.local.crt -noout -enddate`).

NEVER:
- Copy these into production configs.
- Re-use the CA outside this harness.
- Trust them in a real browser session.

Used by `test/integration/full/compose.full.yml` (mounted into the relay container) and trusted by the Playwright config in Plan 3 via CA mount.
