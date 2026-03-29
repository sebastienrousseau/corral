SHELL := /bin/bash
SCRIPTS := corral.sh .githooks/pre-commit .githooks/pre-push

.PHONY: all init lint check test

all: check

init:
	git config core.hooksPath .githooks
	@echo "Git hooks enabled."

lint:
	shellcheck $(SCRIPTS)

check: lint
	@for s in $(SCRIPTS); do bash -n "$$s" || exit 1; done
	@echo "All checks passed."

test: check
	@command -v bats >/dev/null 2>&1 || { echo "ERROR: bats not found. Install: brew install bats-core (macOS) or sudo apt install bats (Linux)"; exit 1; }
	bats tests/
