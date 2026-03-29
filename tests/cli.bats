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

# --- Dry-run flag ---

@test "--dry-run flag is accepted" {
	create_test_env
	mock_gh ""
	mock_git

	run env PATH="$MOCK_BIN:$PATH" bash "$SCRIPT" --dry-run testowner "$TEST_DIR/out"
	[[ "$status" -eq 0 ]]
	teardown_test_env
}

@test "-n shorthand works" {
	create_test_env
	mock_gh ""
	mock_git

	run env PATH="$MOCK_BIN:$PATH" bash "$SCRIPT" -n testowner "$TEST_DIR/out"
	[[ "$status" -eq 0 ]]
	teardown_test_env
}

@test "unknown flag exits 1" {
	run bash "$SCRIPT" --bogus testowner
	[[ "$status" -eq 1 ]]
	[[ "$output" == *"Unknown option: --bogus"* ]]
}
