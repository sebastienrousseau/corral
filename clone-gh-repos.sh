#!/usr/bin/env bash

set -euo pipefail

OWNER=${1:-sebastienrousseau}
BASE_DIR=${2:-"$HOME/Code"}
LIMIT=${3:-1000}

for cmd in gh git; do
	if ! command -v "$cmd" &>/dev/null; then
		echo "ERROR: Required command '$cmd' not found. Please install it first." >&2
		exit 1
	fi
done

normalize_language() {
	local lang="$1"

	if [[ -z "$lang" ]]; then
		lang="Other"
	else
		case "$lang" in
		"C#") lang="CSharp" ;;
		"C++") lang="Cpp" ;;
		esac
	fi

	lang="${lang// /_}"
	lang="${lang//\//_}"
	echo "$lang"
}

normalize_visibility() {
	if [[ "$1" == "PRIVATE" ]]; then
		echo "Private"
	else
		echo "Public"
	fi
}

mkdir -p "$BASE_DIR"

repo_list="$(mktemp)" || { echo "Failed to create temp file" >&2; exit 1; }
trap 'rm -f "$repo_list"' EXIT

if ! gh repo list "$OWNER" --limit "$LIMIT" --json name,primaryLanguage,visibility \
	--jq '.[] | [.name, (.primaryLanguage.name // "Other"), .visibility] | @tsv' \
	>"$repo_list"; then
	echo "ERROR: gh repo list failed for owner '$OWNER'" >&2
	exit 1
fi

cloned=0
existing=0
moved=0
failed=0

cleanup_empty_legacy_language_folders() {
	shopt -s dotglob nullglob
	local seen_languages=()
	while IFS=$'\t' read -r _ lang _; do
		seen_languages+=("$(normalize_language "$lang")")
	done <"$repo_list"

	local unique_langs
	unique_langs="$(printf '%s\n' "${seen_languages[@]}" | sort -u)"

	while IFS= read -r lang_dir; do
		local folder="$BASE_DIR/$lang_dir"
		if [[ -d "$folder" ]]; then
			local entries=("$folder"/*)
			if ((${#entries[@]} == 0)); then
				rmdir "$folder"
				echo "Removed empty legacy folder: $folder"
			fi
		fi
	done <<<"$unique_langs"
	shopt -u dotglob nullglob
}

while IFS=$'\t' read -r name lang visibility; do
	lang_dir="$(normalize_language "$lang")"
	visibility_dir="$(normalize_visibility "$visibility")"
	legacy_dir="$BASE_DIR/$lang_dir/$name"
	target_dir="$BASE_DIR/$visibility_dir/$lang_dir/$name"

	mkdir -p "$BASE_DIR/$visibility_dir/$lang_dir"

	if [[ -d "$target_dir" ]]; then
		existing=$((existing + 1))
		continue
	fi

	if [[ -d "$legacy_dir" ]]; then
		if mv "$legacy_dir" "$target_dir"; then
			moved=$((moved + 1))
		else
			failed=$((failed + 1))
			echo "FAILED MOVE: $OWNER/$name (left at: $legacy_dir)"
		fi
		continue
	fi

	echo "Cloning $OWNER/$name -> $visibility_dir/$lang_dir"

	if ! git clone "https://github.com/$OWNER/$name.git" "$target_dir"; then
		echo "FAILED: $OWNER/$name (left at: $target_dir)"
		failed=$((failed + 1))
		continue
	fi

	cloned=$((cloned + 1))
done <"$repo_list"

cleanup_empty_legacy_language_folders

if [[ "$failed" -gt 0 ]]; then
	echo "Done. Cloned $cloned repos, moved $moved existing repos, kept $existing repos, $failed failures."
else
	echo "Done. Cloned $cloned repos, moved $moved existing repos, kept $existing repos."
fi
