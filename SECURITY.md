# Security Policy

## Supported versions

Burrow is **pre-alpha**. No releases are supported for production use yet. Security
fixes are made only against `main` until the first tagged release.

## Reporting a vulnerability

<!-- TODO: set the disclosure contact before making this repository public.
     Options: enable GitHub Private Vulnerability Reporting (Security > Advisories),
     and/or list a monitored security email here. -->

**A disclosure contact has not yet been configured.** Until it is, please do **not**
file public issues for security problems. If you have found a vulnerability, hold it
privately until this section lists a private channel.

Once configured, please report privately and allow a reasonable disclosure window
before any public discussion.

## Posture

The control channel uses TLS; client tokens are 256-bit random, stored hashed;
passwords use argon2id (from MVP Phase 2+). No third-party telemetry is ever collected.
