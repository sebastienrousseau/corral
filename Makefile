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

all: format vet test test-race build

build:
	go build -ldflags '$(LDFLAGS)' -o $(BINARY_NAME) main.go

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

format:
	go fmt ./...

clean:
	go clean
	rm -f $(BINARY_NAME)
