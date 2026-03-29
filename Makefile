SHELL := /bin/bash
SCRIPTS := clone-gh-repos.sh .githooks/pre-commit

.PHONY: all init lint check

all: check

init:
	git config core.hooksPath .githooks
	@echo "Git hooks enabled."

lint:
	shellcheck $(SCRIPTS)

check: lint
	@for s in $(SCRIPTS); do bash -n "$$s" || exit 1; done
	@echo "All checks passed."
