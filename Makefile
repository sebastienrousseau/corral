# SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
# SPDX-License-Identifier: GPL-3.0-only

BINARY_NAME=corralctl

# The Unix install contract. PREFIX defaults to /usr/local per the FHS and
# the convention every packager expects; DESTDIR stages the tree elsewhere
# without changing the paths compiled into it, which is how deb, rpm, AUR
# and Homebrew all build.
#
# Installing to /usr/local needs root. For a home-directory install:
#   make install PREFIX=$$HOME/.local
PREFIX ?= /usr/local
DESTDIR ?=
BINDIR = $(DESTDIR)$(PREFIX)/bin
MANDIR = $(DESTDIR)$(PREFIX)/share/man/man1
DOCDIR = $(DESTDIR)$(PREFIX)/share/doc/corral
BASHCOMPDIR = $(DESTDIR)$(PREFIX)/share/bash-completion/completions
ZSHCOMPDIR = $(DESTDIR)$(PREFIX)/share/zsh/site-functions
FISHCOMPDIR = $(DESTDIR)$(PREFIX)/share/fish/vendor_completions.d

# Where generated artefacts land. Never committed: manpages and completions
# are derived from the cobra command tree, so a checked-in copy could only
# ever be out of date with `--help`.
# Not dist/: goreleaser owns that name and cleans it after its before-hooks
# run, which would delete these before packaging.
DIST ?= build

# Build-time version. Resolved from `git describe` so local builds carry a
# meaningful version (matching whatever tag/commit you built from) rather than
# the "dev" fallback baked into the source. Overridden by goreleaser at
# release time.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
VERSION_PKG = github.com/sebastienrousseau/corral
LDFLAGS = -s -w \
	-X $(VERSION_PKG)/cmd.Version=$(VERSION) \
	-X $(VERSION_PKG)/internal/tui.Version=$(VERSION)

.PHONY: all build docs install uninstall install-smoke test test-race vet lint \
        clean format sbom-check example-check doc-check spdx-check pkg-check \
        docs-lint help

all: format vet spdx-check sbom-check pkg-check example-check test test-race build

## build: compile the binary with version metadata
build:
	go build -trimpath -ldflags '$(LDFLAGS)' -o $(BINARY_NAME) ./cmd/corralctl

## docs: generate manpages and shell completions from the command tree
docs:
	go run scripts/gen_docs.go $(DIST)

## install: install binary, manpages and completions under PREFIX
install: build docs
	install -d $(BINDIR)
	install -m 0755 $(BINARY_NAME) $(BINDIR)/$(BINARY_NAME)
	install -d $(MANDIR)
	install -m 0644 $(DIST)/man/*.1 $(MANDIR)/
	install -d $(BASHCOMPDIR) $(ZSHCOMPDIR) $(FISHCOMPDIR)
	install -m 0644 $(DIST)/completions/corralctl.bash $(BASHCOMPDIR)/$(BINARY_NAME)
	install -m 0644 $(DIST)/completions/corralctl.zsh  $(ZSHCOMPDIR)/_$(BINARY_NAME)
	install -m 0644 $(DIST)/completions/corralctl.fish $(FISHCOMPDIR)/$(BINARY_NAME).fish
	install -d $(DOCDIR)
	install -m 0644 README.md CHANGELOG.md LICENSE SECURITY.md $(DOCDIR)/

## uninstall: remove everything install put in place
uninstall:
	rm -f $(BINDIR)/$(BINARY_NAME)
	rm -f $(MANDIR)/corralctl.1 $(MANDIR)/corralctl-*.1
	rm -f $(BASHCOMPDIR)/$(BINARY_NAME)
	rm -f $(ZSHCOMPDIR)/_$(BINARY_NAME)
	rm -f $(FISHCOMPDIR)/$(BINARY_NAME).fish
	rm -rf $(DOCDIR)

## install-smoke: stage an install into a temp tree and assert its shape
install-smoke:
	@rm -rf /tmp/corral-stage
	@$(MAKE) --no-print-directory install DESTDIR=/tmp/corral-stage PREFIX=/usr
	@set -e; \
	for f in usr/bin/$(BINARY_NAME) \
	         usr/share/man/man1/corralctl.1 \
	         usr/share/man/man1/corralctl-mcp.1 \
	         usr/share/bash-completion/completions/$(BINARY_NAME) \
	         usr/share/zsh/site-functions/_$(BINARY_NAME) \
	         usr/share/fish/vendor_completions.d/$(BINARY_NAME).fish \
	         usr/share/doc/corral/README.md; do \
	  test -f "/tmp/corral-stage/$$f" || { echo "MISSING: $$f" >&2; exit 1; }; \
	done; \
	test -x /tmp/corral-stage/usr/bin/$(BINARY_NAME) || { echo "binary not executable" >&2; exit 1; }
	@echo "install-smoke: staged tree is correct"
	@rm -rf /tmp/corral-stage

## test: run the test suite
test:
	go test ./...

## test-race: run the suite under the race detector with shuffled order
test-race:
	go test -race -shuffle=on ./...

## vet: run go vet
vet:
	go vet ./...

## lint: run golangci-lint
lint:
	golangci-lint run ./...

## sbom-check: verify SBOM.md and server.json against go.mod and CHANGELOG
sbom-check:
	go run scripts/manifest_check.go

## example-check: compile every program under examples/
example-check:
	go run scripts/example_check.go

## doc-check: enforce 100% documentation coverage on exported declarations
doc-check:
	go run scripts/doc_coverage.go

## pkg-check: verify pkg/ documents every distribution format built
pkg-check:
	go run scripts/pkg_check.go

## spdx-check: verify every source file carries an SPDX header
spdx-check:
	go run scripts/spdx_sweep.go -check

## docs-lint: markdownlint + codespell over the prose
docs-lint:
	@command -v markdownlint >/dev/null && markdownlint '**/*.md' --ignore node_modules --ignore docs-site || echo "markdownlint not installed; skipping"
	@command -v codespell >/dev/null && codespell --skip='./.git,./docs-site,./public,go.sum' || echo "codespell not installed; skipping"

## format: gofmt the tree
format:
	go fmt ./...

## clean: remove build output
clean:
	go clean
	rm -f $(BINARY_NAME)
	rm -rf $(DIST)

## help: list the targets
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## /  /' | sort
