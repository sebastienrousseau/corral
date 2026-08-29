BINARY_NAME=corralctl
PREFIX ?= $(HOME)/.local

# Build-time version. Resolved from `git describe` so local builds carry a
# meaningful version (matching whatever tag/commit you built from) rather than
# the "dev" fallback baked into the source. Overridden by goreleaser at
# release time.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
VERSION_PKG = github.com/sebastienrousseau/corral
LDFLAGS = -s -w \
	-X $(VERSION_PKG)/cmd.Version=$(VERSION) \
	-X $(VERSION_PKG)/internal/tui.Version=$(VERSION)

.PHONY: all build install test test-race vet lint clean format

all: format vet sbom-check example-check test test-race build

build:
	go build -ldflags '$(LDFLAGS)' -o $(BINARY_NAME) ./cmd/corralctl

install: build
	install -d $(DESTDIR)$(PREFIX)/bin
	install -m 0755 $(BINARY_NAME) $(DESTDIR)$(PREFIX)/bin/$(BINARY_NAME)

test:
	go test ./...

test-race:
	go test -race ./...

vet:
	go vet ./...

lint:
	golangci-lint run ./...

# Verify SBOM.md matches go.mod's direct requirements, and that
# server.json's version matches the newest CHANGELOG.md release.
sbom-check:
	go run scripts/manifest_check.go

# Compile every program under examples/. They carry `//go:build ignore`,
# so nothing else in the build ever touches them.
example-check:
	go run scripts/example_check.go

format:
	go fmt ./...

clean:
	go clean
	rm -f $(BINARY_NAME)
