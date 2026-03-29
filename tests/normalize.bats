#!/usr/bin/env bats
# Unit tests for normalize_language and normalize_visibility.

setup() {
	load helpers/setup
	source_functions
}

# --- normalize_language ---

@test "normalize_language: empty string returns other" {
	result="$(normalize_language "")"
	[[ "$result" == "other" ]]
}

@test "normalize_language: standard language lowercased" {
	result="$(normalize_language "Rust")"
	[[ "$result" == "rust" ]]
}

@test "normalize_language: TypeScript lowercased" {
	result="$(normalize_language "TypeScript")"
	[[ "$result" == "typescript" ]]
}

@test "normalize_language: C# becomes csharp" {
	result="$(normalize_language "C#")"
	[[ "$result" == "csharp" ]]
}

@test "normalize_language: C++ becomes cpp" {
	result="$(normalize_language "C++")"
	[[ "$result" == "cpp" ]]
}

@test "normalize_language: spaces replaced with underscores" {
	result="$(normalize_language "Objective C")"
	[[ "$result" == "objective_c" ]]
}

@test "normalize_language: slashes replaced with underscores" {
	result="$(normalize_language "A/B")"
	[[ "$result" == "a_b" ]]
}

@test "normalize_language: mixed case fully lowercased" {
	result="$(normalize_language "HTML")"
	[[ "$result" == "html" ]]
}

# --- normalize_visibility ---

@test "normalize_visibility: PRIVATE returns Private" {
	result="$(normalize_visibility "PRIVATE")"
	[[ "$result" == "Private" ]]
}

@test "normalize_visibility: PUBLIC returns Public" {
	result="$(normalize_visibility "PUBLIC")"
	[[ "$result" == "Public" ]]
}

@test "normalize_visibility: empty string returns Public" {
	result="$(normalize_visibility "")"
	[[ "$result" == "Public" ]]
}

@test "normalize_visibility: unknown value returns Public" {
	result="$(normalize_visibility "INTERNAL")"
	[[ "$result" == "Public" ]]
}
