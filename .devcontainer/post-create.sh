#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
# SPDX-License-Identifier: GPL-3.0-only
#
# Boot the container to a working `make`. Everything below is a gate the
# CI runs, so a fresh Codespace can reproduce CI without further setup.
set -euo pipefail

echo "==> Go toolchain"
go version

echo "==> Warming the module cache"
go mod download

echo "==> Tools for the local gates"
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
go install golang.org/x/vuln/cmd/govulncheck@latest

echo "==> Prose tooling (best effort; the CI job is authoritative)"
sudo apt-get update -qq
sudo apt-get install -y -qq groff python3-pip
pip3 install --quiet --break-system-packages codespell pre-commit || true
npm install -g markdownlint-cli2 >/dev/null 2>&1 || true

echo "==> Verifying the build"
make build

cat <<'BANNER'

Ready. Useful targets:

  make            format, vet, checks, tests, build
  make test-race  race detector with shuffled order
  make docs       generate manpages + completions
  make help       every target

See DEVELOPMENT.md for the local equivalent of every CI gate.
BANNER
