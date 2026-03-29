#!/usr/bin/env bats
# Integration tests: clone, skip, migrate, fail, and cleanup flows.

setup() {
	load helpers/setup
	create_test_env
	BASE="$TEST_DIR/repos"
}

teardown() {
	teardown_test_env
}

# --- Fresh clone ---

@test "fresh clone: creates target directory structure" {
	mock_gh "$(printf 'my-repo\tRust\tPUBLIC')"
	mock_git

	run env PATH="$MOCK_BIN:$PATH" bash "$SCRIPT" testowner "$BASE"
	[[ "$status" -eq 0 ]]
	[[ -d "$BASE/Public/rust/my-repo" ]]
}

@test "fresh clone: summary reports 1 cloned" {
	mock_gh "$(printf 'my-repo\tRust\tPUBLIC')"
	mock_git

	run env PATH="$MOCK_BIN:$PATH" bash "$SCRIPT" testowner "$BASE"
	[[ "$output" == *"Cloned 1 repos"* ]]
}

@test "fresh clone: private repo lands in Private tree" {
	mock_gh "$(printf 'secret\tPython\tPRIVATE')"
	mock_git

	run env PATH="$MOCK_BIN:$PATH" bash "$SCRIPT" testowner "$BASE"
	[[ -d "$BASE/Private/python/secret" ]]
}

# --- Multiple repos ---

@test "multiple repos: each sorted by visibility and language" {
	mock_gh "$(printf 'app\tTypeScript\tPUBLIC\nlib\tRust\tPRIVATE')"
	mock_git

	run env PATH="$MOCK_BIN:$PATH" bash "$SCRIPT" testowner "$BASE"
	[[ "$status" -eq 0 ]]
	[[ -d "$BASE/Public/typescript/app" ]]
	[[ -d "$BASE/Private/rust/lib" ]]
	[[ "$output" == *"Cloned 2 repos"* ]]
}

# --- Already cloned (skip) ---

@test "already cloned: skips existing target directory" {
	mock_gh "$(printf 'my-repo\tRust\tPUBLIC')"
	mock_git

	mkdir -p "$BASE/Public/rust/my-repo"

	run env PATH="$MOCK_BIN:$PATH" bash "$SCRIPT" testowner "$BASE"
	[[ "$status" -eq 0 ]]
	[[ "$output" == *"kept 1 repos"* ]]
	[[ "$output" != *"Cloning"* ]]
}

# --- Legacy migration ---

@test "legacy migration: moves flat layout into new structure" {
	mock_gh "$(printf 'my-repo\tRust\tPUBLIC')"
	mock_git

	# Create legacy directory
	mkdir -p "$BASE/rust/my-repo"
	touch "$BASE/rust/my-repo/marker"

	run env PATH="$MOCK_BIN:$PATH" bash "$SCRIPT" testowner "$BASE"
	[[ "$status" -eq 0 ]]

	# Moved to new location
	[[ -f "$BASE/Public/rust/my-repo/marker" ]]
	# Old location removed
	[[ ! -d "$BASE/rust/my-repo" ]]
	[[ "$output" == *"moved 1"* ]]
}

# --- Clone failure ---

@test "clone failure: counts failure and continues" {
	mock_gh "$(printf 'bad-repo\tGo\tPUBLIC\ngood-repo\tGo\tPUBLIC')"

	# Mock git that fails on bad-repo, succeeds on good-repo
	cat >"$MOCK_BIN/git" <<'MOCK'
#!/usr/bin/env bash
if [[ "$1" == "clone" ]]; then
	if [[ "$2" == *"bad-repo"* ]]; then
		echo "fatal: not found" >&2
		exit 128
	fi
	mkdir -p "$3"
	exit 0
fi
MOCK
	chmod +x "$MOCK_BIN/git"

	run env PATH="$MOCK_BIN:$PATH" bash "$SCRIPT" testowner "$BASE"
	[[ "$status" -eq 0 ]]
	[[ "$output" == *"1 failures"* ]]
	[[ "$output" == *"Cloned 1 repos"* ]]
	[[ -d "$BASE/Public/go/good-repo" ]]
}

# --- gh failure ---

@test "gh failure: exits 1 with error message" {
	mock_gh_fail
	mock_git

	run env PATH="$MOCK_BIN:$PATH" bash "$SCRIPT" testowner "$BASE"
	[[ "$status" -eq 1 ]]
	[[ "$output" == *"gh repo list failed"* ]]
}

# --- Summary format ---

@test "summary without failures: omits failure count" {
	mock_gh "$(printf 'repo\tRust\tPUBLIC')"
	mock_git

	run env PATH="$MOCK_BIN:$PATH" bash "$SCRIPT" testowner "$BASE"
	[[ "$output" != *"failures"* ]]
}

@test "summary with failures: includes failure count" {
	mock_gh "$(printf 'repo\tRust\tPUBLIC')"
	mock_git_fail

	run env PATH="$MOCK_BIN:$PATH" bash "$SCRIPT" testowner "$BASE"
	[[ "$output" == *"1 failures"* ]]
}

# --- Empty repo list ---

@test "empty repo list: reports 0 cloned" {
	mock_gh ""
	mock_git

	run env PATH="$MOCK_BIN:$PATH" bash "$SCRIPT" testowner "$BASE"
	[[ "$status" -eq 0 ]]
	[[ "$output" == *"Cloned 0 repos"* ]]
}

# --- Language edge cases in integration ---

@test "C# repo: directory named csharp" {
	mock_gh "$(printf 'dotnet-app\tC#\tPUBLIC')"
	mock_git

	run env PATH="$MOCK_BIN:$PATH" bash "$SCRIPT" testowner "$BASE"
	[[ -d "$BASE/Public/csharp/dotnet-app" ]]
}

@test "null language: directory named other" {
	mock_gh "$(printf 'config\tOther\tPUBLIC')"
	mock_git

	run env PATH="$MOCK_BIN:$PATH" bash "$SCRIPT" testowner "$BASE"
	[[ -d "$BASE/Public/other/config" ]]
}

# --- Cleanup ---

@test "cleanup: removes empty legacy language directory" {
	mock_gh "$(printf 'my-repo\tRust\tPUBLIC')"
	mock_git

	# Create empty legacy dir (as if migration already moved contents)
	mkdir -p "$BASE/rust"

	# Pre-create target so the loop skips cloning
	mkdir -p "$BASE/Public/rust/my-repo"

	run env PATH="$MOCK_BIN:$PATH" bash "$SCRIPT" testowner "$BASE"
	[[ ! -d "$BASE/rust" ]]
	[[ "$output" == *"Removed empty legacy folder"* ]]
}

@test "cleanup: preserves non-empty legacy directory" {
	mock_gh "$(printf 'my-repo\tRust\tPUBLIC')"
	mock_git

	# Create non-empty legacy dir
	mkdir -p "$BASE/rust/other-content"

	# Pre-create target so the loop skips cloning
	mkdir -p "$BASE/Public/rust/my-repo"

	run env PATH="$MOCK_BIN:$PATH" bash "$SCRIPT" testowner "$BASE"
	[[ -d "$BASE/rust/other-content" ]]
}

# --- Idempotency ---

@test "idempotent: second run clones 0, keeps all" {
	mock_gh "$(printf 'repo-a\tGo\tPUBLIC\nrepo-b\tGo\tPRIVATE')"
	mock_git

	# First run
	env PATH="$MOCK_BIN:$PATH" bash "$SCRIPT" testowner "$BASE"

	# Second run
	run env PATH="$MOCK_BIN:$PATH" bash "$SCRIPT" testowner "$BASE"
	[[ "$status" -eq 0 ]]
	[[ "$output" == *"Cloned 0 repos"* ]]
	[[ "$output" == *"kept 2 repos"* ]]
}
