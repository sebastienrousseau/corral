#!/usr/bin/env bats
# CLI argument validation and pre-flight checks.

setup() {
	load helpers/setup
}

@test "no arguments: prints usage and exits 1" {
	run bash "$SCRIPT"
	[[ "$status" -eq 1 ]]
	[[ "$output" == *"Usage:"* ]]
}

@test "no arguments: usage includes owner parameter" {
	run bash "$SCRIPT"
	[[ "$output" == *"owner"* ]]
}

@test "missing gh: prints error and exits 1" {
	create_test_env
	mock_git
	# No gh mock — isolated PATH has git but not gh
	ISO_PATH="$(build_isolated_path)"

	run env PATH="$ISO_PATH" bash "$SCRIPT" testowner "$TEST_DIR/out"
	[[ "$status" -eq 1 ]]
	[[ "$output" == *"Required command 'gh' not found"* ]]
	teardown_test_env
}

@test "missing git: prints error and exits 1" {
	create_test_env
	mock_gh ""
	# No git mock — isolated PATH has gh but not git
	ISO_PATH="$(build_isolated_path)"

	run env PATH="$ISO_PATH" bash "$SCRIPT" testowner "$TEST_DIR/out"
	[[ "$status" -eq 1 ]]
	[[ "$output" == *"Required command 'git' not found"* ]]
	teardown_test_env
}

# --- --protocol flag ---

@test "--protocol ssh: accepted" {
	create_test_env
	mock_gh "$(printf 'repo\tRust\tPUBLIC')"
	mock_git

	run env PATH="$MOCK_BIN:$PATH" bash "$SCRIPT" --protocol ssh testowner "$TEST_DIR/repos"
	[[ "$status" -eq 0 ]]
	teardown_test_env
}

@test "--protocol https: accepted" {
	create_test_env
	mock_gh "$(printf 'repo\tRust\tPUBLIC')"
	mock_git

	run env PATH="$MOCK_BIN:$PATH" bash "$SCRIPT" --protocol https testowner "$TEST_DIR/repos"
	[[ "$status" -eq 0 ]]
	teardown_test_env
}

@test "-p shorthand: accepted" {
	create_test_env
	mock_gh "$(printf 'repo\tRust\tPUBLIC')"
	mock_git

	run env PATH="$MOCK_BIN:$PATH" bash "$SCRIPT" -p ssh testowner "$TEST_DIR/repos"
	[[ "$status" -eq 0 ]]
	teardown_test_env
}

@test "--protocol bogus: exits 1" {
	run bash "$SCRIPT" --protocol bogus testowner
	[[ "$status" -eq 1 ]]
	[[ "$output" == *"--protocol must be 'ssh' or 'https'"* ]]
}

@test "--protocol without value: exits 1" {
	run bash "$SCRIPT" --protocol
	[[ "$status" -eq 1 ]]
	[[ "$output" == *"--protocol requires a value"* ]]
}

# --- --sync flag ---

@test "--sync: accepted" {
	create_test_env
	mock_gh "$(printf 'repo\tRust\tPUBLIC')"
	mock_git

	run env PATH="$MOCK_BIN:$PATH" bash "$SCRIPT" --sync testowner "$TEST_DIR/repos"
	[[ "$status" -eq 0 ]]
	teardown_test_env
}

@test "-s shorthand: accepted" {
	create_test_env
	mock_gh "$(printf 'repo\tRust\tPUBLIC')"
	mock_git

	run env PATH="$MOCK_BIN:$PATH" bash "$SCRIPT" -s testowner "$TEST_DIR/repos"
	[[ "$status" -eq 0 ]]
	teardown_test_env
}

# --- --help flag ---

@test "--help: prints usage and exits 0" {
	run bash "$SCRIPT" --help
	[[ "$status" -eq 0 ]]
	[[ "$output" == *"Usage:"* ]]
	[[ "$output" == *"--help"* ]]
	[[ "$output" == *"--dry-run"* ]]
}

@test "-h shorthand: prints usage and exits 0" {
	run bash "$SCRIPT" -h
	[[ "$status" -eq 0 ]]
	[[ "$output" == *"Usage:"* ]]
}

# --- --dry-run flag ---

@test "--dry-run: accepted" {
	create_test_env
	mock_gh "$(printf 'repo\tRust\tPUBLIC')"
	mock_git

	run env PATH="$MOCK_BIN:$PATH" bash "$SCRIPT" --dry-run testowner "$TEST_DIR/repos"
	[[ "$status" -eq 0 ]]
	teardown_test_env
}

@test "-n shorthand: accepted" {
	create_test_env
	mock_gh "$(printf 'repo\tRust\tPUBLIC')"
	mock_git

	run env PATH="$MOCK_BIN:$PATH" bash "$SCRIPT" -n testowner "$TEST_DIR/repos"
	[[ "$status" -eq 0 ]]
	teardown_test_env
}

@test "--dry-run without owner: shows usage and exits 1" {
	run bash "$SCRIPT" --dry-run
	[[ "$status" -eq 1 ]]
	[[ "$output" == *"Usage:"* ]]
}
