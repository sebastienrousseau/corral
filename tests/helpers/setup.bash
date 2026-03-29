#!/usr/bin/env bash
# Common test setup: source functions, create temp dirs, build mocks.

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT="$REPO_ROOT/corral.sh"

# Source only the function definitions
source_functions() {
	__CORRAL_SOURCED=1 source "$SCRIPT"
}

# Create an isolated temp directory for each test
create_test_env() {
	TEST_DIR="$(mktemp -d)"
	MOCK_BIN="$TEST_DIR/bin"
	mkdir -p "$MOCK_BIN"
}

# Remove the temp directory after each test
teardown_test_env() {
	if [[ -n "${TEST_DIR:-}" && -d "$TEST_DIR" ]]; then
		rm -rf "$TEST_DIR"
	fi
}

# Build a minimal isolated PATH containing only essential system utilities.
# Excludes gh and git unless explicitly mocked in MOCK_BIN.
build_isolated_path() {
	local iso_bin="$TEST_DIR/iso_bin"
	mkdir -p "$iso_bin"
	local utils=(bash env basename dirname cat printf mktemp rm mkdir rmdir mv sort)
	for u in "${utils[@]}"; do
		local p
		p="$(command -v "$u" 2>/dev/null)" && ln -sf "$p" "$iso_bin/$u"
	done
	echo "$MOCK_BIN:$iso_bin"
}

# Create a mock `gh` that outputs a given TSV repo list
mock_gh() {
	local tsv_content="$1"
	cat >"$MOCK_BIN/gh" <<MOCK
#!/usr/bin/env bash
if [[ "\$1" == "repo" && "\$2" == "list" ]]; then
	cat <<'TSV'
${tsv_content}
TSV
	exit 0
fi
exit 1
MOCK
	chmod +x "$MOCK_BIN/gh"
}

# Create a mock `gh` that fails
mock_gh_fail() {
	cat >"$MOCK_BIN/gh" <<'MOCK'
#!/usr/bin/env bash
echo "gh: error" >&2
exit 1
MOCK
	chmod +x "$MOCK_BIN/gh"
}

# Create a mock `git` that simulates clone and pull
mock_git() {
	cat >"$MOCK_BIN/git" <<MOCK
#!/usr/bin/env bash
if [[ "\$1" == "clone" ]]; then
	echo "\$2" >> "$TEST_DIR/git_clone_urls"
	mkdir -p "\$3"
	exit 0
fi
if [[ "\$1" == "-C" && "\$3" == "pull" ]]; then
	echo "\$2" >> "$TEST_DIR/git_pull_targets"
	exit 0
fi
exit 1
MOCK
	chmod +x "$MOCK_BIN/git"
}

# Create a mock `git` that fails to clone
mock_git_fail() {
	cat >"$MOCK_BIN/git" <<'MOCK'
#!/usr/bin/env bash
if [[ "$1" == "clone" ]]; then
	echo "fatal: repository not found" >&2
	exit 128
fi
exit 1
MOCK
	chmod +x "$MOCK_BIN/git"
}
