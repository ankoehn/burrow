.PHONY: build test lint dev clean release-snapshot certs

BINDIR := ./bin
VERSION := $(shell git describe --tags --always --dirty)
LDFLAGS := -X github.com/ankoehn/burrow/internal/version.Version=$(VERSION)

build:
	@mkdir -p $(BINDIR)
	go build -ldflags="$(LDFLAGS)" -o $(BINDIR)/burrowd ./cmd/server
	go build -ldflags="$(LDFLAGS)" -o $(BINDIR)/burrow ./cmd/client

test:
	go test -race -cover ./...

lint:
	golangci-lint run

dev:
	# Run the server in dev mode with hot reload. Requires `air`.
	air -c .air.toml

certs:
	./scripts/dev-certs.sh

clean:
	rm -rf $(BINDIR) dist/

release-snapshot:
	goreleaser release --snapshot --clean
