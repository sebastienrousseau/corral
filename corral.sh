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
	echo "${lang,,}"
}

normalize_visibility() {
	if [[ "$1" == "PRIVATE" ]]; then
		echo "Private"
	else
		echo "Public"
	fi
}

clone_url() {
	if [[ "$PROTOCOL" == "ssh" ]]; then
		echo "git@github.com:${1}/${2}.git"
	else
		echo "https://github.com/${1}/${2}.git"
	fi
}

show_help() {
	cat <<EOF
Usage: $(basename "$0") [options] <owner> [base_dir] [limit]

Arguments:
  owner      GitHub username or organisation (required)
  base_dir   root directory for cloned repos (default: \$HOME/Code)
  limit      max repos to list (default: 1000)

Options:
  -h, --help                  Show this help message
  -n, --dry-run               Preview actions without making changes
  -p, --protocol <ssh|https>  Clone protocol (default: https)
  -s, --sync                  Pull latest changes for existing repos
EOF
}

execute() {
	if [[ "$DRY_RUN" == "true" ]]; then
		echo "[DRY-RUN] $*"
		return 0
	fi
	"$@"
}

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

# Allow sourcing for tests: __CORRAL_SOURCED=1 source corral.sh
if [[ "${__CORRAL_SOURCED:-}" == "1" ]]; then
	# shellcheck disable=SC2317
	return 0 2>/dev/null || exit 0
fi

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

PROTOCOL=https
SYNC=false
DRY_RUN=false

while [[ $# -gt 0 ]]; do
	case "$1" in
	-h | --help)
		show_help
		exit 0
		;;
	-n | --dry-run)
		DRY_RUN=true
		shift
		;;
	-p | --protocol)
		if [[ -z "${2:-}" ]]; then
			echo "ERROR: --protocol requires a value (ssh or https)" >&2
			exit 1
		fi
		PROTOCOL="$2"
		shift 2
		;;
	-s | --sync)
		SYNC=true
		shift
		;;
	-*)
		echo "ERROR: Unknown option: $1" >&2
		exit 1
		;;
	*)
		break
		;;
	esac
done

if [[ "$PROTOCOL" != "https" && "$PROTOCOL" != "ssh" ]]; then
	echo "ERROR: --protocol must be 'ssh' or 'https' (got: '$PROTOCOL')" >&2
	exit 1
fi

if [[ $# -lt 1 ]]; then
	show_help >&2
	exit 1
fi

OWNER="$1"
BASE_DIR=${2:-"$HOME/Code"}
LIMIT=${3:-1000}

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
synced=0

while IFS=$'\t' read -r name lang visibility; do
	lang_dir="$(normalize_language "$lang")"
	visibility_dir="$(normalize_visibility "$visibility")"
	legacy_dir="$BASE_DIR/$lang_dir/$name"
	target_dir="$BASE_DIR/$visibility_dir/$lang_dir/$name"

	mkdir -p "$BASE_DIR/$visibility_dir/$lang_dir"

	if [[ -d "$target_dir" ]]; then
		if [[ "$SYNC" == "true" ]]; then
			if [[ -d "$target_dir/.git" ]]; then
				echo "Syncing $OWNER/$name"
				if execute git -C "$target_dir" pull --rebase --autostash; then
					synced=$((synced + 1))
				else
					echo "SYNC FAILED: $OWNER/$name"
					failed=$((failed + 1))
				fi
			else
				echo "WARNING: $target_dir exists but is not a git repo, skipping sync"
				existing=$((existing + 1))
			fi
		else
			existing=$((existing + 1))
		fi
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

	echo "Cloning $OWNER/$name -> $visibility_dir/$lang_dir"

	if ! execute git clone "$(clone_url "$OWNER" "$name")" "$target_dir"; then
		echo "FAILED: $OWNER/$name (left at: $target_dir)"
		failed=$((failed + 1))
		continue
	fi

	cloned=$((cloned + 1))
done <"$repo_list"

cleanup_empty_legacy_language_folders

summary="Done. Cloned $cloned repos, moved $moved existing repos, kept $existing repos"
if [[ "$SYNC" == "true" ]]; then
	summary+=", synced $synced repos"
fi
if [[ "$failed" -gt 0 ]]; then
	summary+=", $failed failures"
fi
echo "${summary}."
