# Security Policy

## Supported versions

Burrow is **pre-alpha**. No releases are supported for production use yet. Security
fixes are made only against `main` until the first tagged release.

## Reporting a vulnerability

Use **GitHub Private Vulnerability Reporting**: go to the repository's Security tab
and click "Report a vulnerability". This opens a private advisory visible only to
maintainers. Do **not** file a public issue for security problems.

Please allow a reasonable disclosure window before any public discussion.

## Posture

The control channel uses TLS; client tokens are 256-bit random, stored hashed;
passwords use argon2id (from MVP Phase 2+). No third-party telemetry is ever collected.
