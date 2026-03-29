#!/usr/bin/env bash

set -euo pipefail

# ---------------------------------------------------------------------------
# Functions
# ---------------------------------------------------------------------------

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
	lang="${lang,,}"

	# Fast path: set caller variable directly (zero fork)
	if [[ -n "${2:-}" ]]; then
		printf -v "$2" '%s' "$lang"
	else
		echo "$lang"
	fi
}

normalize_visibility() {
	local result
	if [[ "$1" == "PRIVATE" ]]; then
		result="Private"
	else
		result="Public"
	fi

	if [[ -n "${2:-}" ]]; then
		printf -v "$2" '%s' "$result"
	else
		echo "$result"
	fi
}

execute() {
	if [[ "$DRY_RUN" == "true" ]]; then
		printf " [DRY-RUN] %s\n" "$*"
	else
		"$@"
	fi
}

cleanup_empty_legacy_language_folders() {
	# Uses _unique_langs associative array built during main loop
	if [[ -z "${_unique_langs[*]+set}" ]]; then
		return 0
	fi

	shopt -s dotglob nullglob
	local lang_dir folder
	for lang_dir in "${!_unique_langs[@]}"; do
		folder="$BASE_DIR/$lang_dir"
		if [[ -d "$folder" ]]; then
			local entries=("$folder"/*)
			if ((${#entries[@]} == 0)); then
				if execute rmdir "$folder" 2>/dev/null; then
					echo "Removed empty legacy folder: $folder"
				fi
			fi
		fi
	done
	shopt -u dotglob nullglob
}

# Allow sourcing for tests: __CORRAL_SOURCED=1 source corral.sh
if [[ "${__CORRAL_SOURCED:-}" == "1" ]]; then
	# shellcheck disable=SC2317
	return 0 2>/dev/null || exit 0
fi

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

DRY_RUN=false

while [[ $# -gt 0 ]]; do
	case "$1" in
		-n|--dry-run) DRY_RUN=true; shift ;;
		-*) echo "Unknown option: $1" >&2; exit 1 ;;
		*) break ;;
	esac
done

if [[ $# -lt 1 ]]; then
	echo "Usage: $(basename "$0") [-n|--dry-run] <owner> [base_dir] [limit]" >&2
	echo "  -n, --dry-run  preview changes without executing them" >&2
	echo "  owner:         GitHub username or organisation (required)" >&2
	echo "  base_dir:      root directory for cloned repos (default: \$HOME/Code)" >&2
	echo "  limit:         max repos to list (default: 1000)" >&2
	exit 1
fi

OWNER="$1"
BASE_DIR=${2:-"$HOME/Code"}
LIMIT=${3:-1000}
PARALLEL=${CORRAL_PARALLEL:-4}

if ((BASH_VERSINFO[0] < 4)); then
	echo "ERROR: Bash 4+ is required (found ${BASH_VERSION}). On macOS: brew install bash" >&2
	exit 1
fi

for cmd in gh git; do
	if ! command -v "$cmd" &>/dev/null; then
		echo "ERROR: Required command '$cmd' not found. Please install it first." >&2
		exit 1
	fi
done

mkdir -p "$BASE_DIR"

repo_list="$(mktemp)" || { echo "Failed to create temp file" >&2; exit 1; }
clone_ok="$(mktemp)" || { echo "Failed to create temp file" >&2; exit 1; }
clone_fail="$(mktemp)" || { echo "Failed to create temp file" >&2; exit 1; }
trap 'rm -f "$repo_list" "$clone_ok" "$clone_fail"' EXIT

if ! gh repo list "$OWNER" --limit "$LIMIT" --json name,primaryLanguage,visibility \
	--jq '.[] | [.name, (.primaryLanguage.name // "Other"), .visibility] | @tsv' \
	>"$repo_list"; then
	echo "ERROR: gh repo list failed for owner '$OWNER'" >&2
	exit 1
fi

existing=0
moved=0
failed=0
declare -A _unique_langs
_clone_names=()
_clone_targets=()
_clone_labels=()

# Phase 1: Classify — sequential, zero-fork normalization
while IFS=$'\t' read -r name lang visibility; do
	[[ -n "$name" ]] || continue
	normalize_language "$lang" lang_dir
	# shellcheck disable=SC2154
	normalize_visibility "$visibility" visibility_dir
	_unique_langs["$lang_dir"]=1

	legacy_dir="$BASE_DIR/$lang_dir/$name"
	# shellcheck disable=SC2154
	target_dir="$BASE_DIR/$visibility_dir/$lang_dir/$name"

	mkdir -p "$BASE_DIR/$visibility_dir/$lang_dir"

	if [[ -d "$target_dir" ]]; then
		existing=$((existing + 1))
		continue
	fi

	if [[ -d "$legacy_dir" ]]; then
		if execute mv "$legacy_dir" "$target_dir"; then
			moved=$((moved + 1))
		else
			failed=$((failed + 1))
			echo "FAILED MOVE: $OWNER/$name (left at: $legacy_dir)"
		fi
		continue
	fi

	_clone_names+=("$name")
	_clone_targets+=("$target_dir")
	_clone_labels+=("$visibility_dir/$lang_dir")
done <"$repo_list"

# Phase 2: Clone — parallel when PARALLEL>1, inline when sequential
if ((${#_clone_names[@]} > 0)); then
	if ((PARALLEL <= 1)); then
		for ((i = 0; i < ${#_clone_names[@]}; i++)); do
			echo "Cloning $OWNER/${_clone_names[i]} -> ${_clone_labels[i]}"
			if execute git clone "https://github.com/$OWNER/${_clone_names[i]}.git" "${_clone_targets[i]}"; then
				echo "${_clone_names[i]}" >>"$clone_ok"
			else
				echo "FAILED: $OWNER/${_clone_names[i]} (left at: ${_clone_targets[i]})"
				echo "${_clone_names[i]}" >>"$clone_fail"
			fi
		done
	else
		active=0
		for ((i = 0; i < ${#_clone_names[@]}; i++)); do
			echo "Cloning $OWNER/${_clone_names[i]} -> ${_clone_labels[i]}"
			(
				set +e
				execute git clone "https://github.com/$OWNER/${_clone_names[i]}.git" "${_clone_targets[i]}" 2>/dev/null
				rc=$?
				if ((rc == 0)); then
					echo "${_clone_names[i]}" >>"$clone_ok"
				else
					echo "FAILED: $OWNER/${_clone_names[i]} (left at: ${_clone_targets[i]})"
					echo "${_clone_names[i]}" >>"$clone_fail"
				fi
			) &
			active=$((active + 1))
			if ((active >= PARALLEL)); then
				wait
				active=0
			fi
		done
		wait
	fi
fi

cloned=$(wc -l <"$clone_ok" | tr -d ' ')
clone_failures=$(wc -l <"$clone_fail" | tr -d ' ')
failed=$((failed + clone_failures))

cleanup_empty_legacy_language_folders

if [[ "$failed" -gt 0 ]]; then
	echo "Done. Cloned $cloned repos, moved $moved existing repos, kept $existing repos, $failed failures."
else
	echo "Done. Cloned $cloned repos, moved $moved existing repos, kept $existing repos."
fi
