# Contributing to Burrow

Thanks for your interest. Burrow is Apache 2.0 and pre-alpha — expect churn.

## Prerequisites

- Go **1.25+** (raised from 1.22 in Phase 4a — required by the pure-Go `modernc.org/sqlite` driver)
- `git`
- (optional) `make`; on Windows use [`task`](https://taskfile.dev) instead
- (optional) `golangci-lint`, `air`, `goreleaser` for lint / hot-reload / release

Clone and build:

```bash
git clone https://github.com/ankoehn/burrow
cd burrow
go mod tidy
make build          # or: task build
```

## Project layout rules

- `cmd/` holds only `main.go` entry points. All logic lives in `internal/`.
- `internal/` is private to this module; use it generously.
- `pkg/` is reserved for code we would expose as a public API. Empty for now.
- Tests live next to the code they test (`foo.go` + `foo_test.go`).

## Before you push

```bash
make test           # go test -race -cover ./...
make lint           # golangci-lint run
```

CI runs the same on every pull request and on `main`. Keep it green.

## Roadmap & scope

Read [docs/ROADMAP.md](docs/ROADMAP.md) and [docs/MVP_PHASES.md](docs/MVP_PHASES.md)
before proposing features. Phase discipline is intentional — out-of-scope ideas go to
a backlog, not into the current phase.
