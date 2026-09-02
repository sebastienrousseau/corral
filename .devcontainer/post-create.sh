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

# Every version below is pinned, never @latest.
#
# An unpinned installer means whatever the tag resolves to at container-build
# time then executes with the developer's credentials, and OpenSSF Scorecard
# flags it under Pinned-Dependencies. This repository pins its actions by SHA
# and its base image by digest; a devcontainer that shells out to @latest
# would be the one unpinned link in that chain.
#
# govulncheck matches the version ci.yml pins, so a local run and a CI run
# read the same database with the same scanner.
GOLANGCI_LINT_VERSION=v2.13.2
GOVULNCHECK_VERSION=v1.7.0
CODESPELL_VERSION=2.4.3
PRE_COMMIT_VERSION=4.6.2
MARKDOWNLINT_CLI2_VERSION=0.23.2

echo "==> Tools for the local gates"
go install "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCI_LINT_VERSION}"
go install "golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION}"

echo "==> Prose tooling (best effort; the CI job is authoritative)"
sudo apt-get update -qq
sudo apt-get install -y -qq groff python3-pip
pip3 install --quiet --break-system-packages \
  "codespell==${CODESPELL_VERSION}" "pre-commit==${PRE_COMMIT_VERSION}" || true
npm install -g "markdownlint-cli2@${MARKDOWNLINT_CLI2_VERSION}" >/dev/null 2>&1 || true

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
