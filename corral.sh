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
	if [[ "$1" == "PRIVATE" || "$1" == "INTERNAL" ]]; then
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
  -c, --concurrency <n>       Number of concurrent operations (default: 1)
  -n, --dry-run               Preview actions without making changes
  -o, --orphans               Detect and list local repositories not on GitHub
  -p, --protocol <ssh|https>  Clone protocol (default: https)
      --no-sync               Skip pulling latest changes for existing repos
      --recurse-submodules    Initialize submodules on clone and sync
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
	shopt -s nullglob
	for folder in "$BASE_DIR"/*; do
		if [[ -d "$folder" && "$folder" != "$BASE_DIR/Public" && "$folder" != "$BASE_DIR/Private" ]]; then
			if rmdir "$folder" 2>/dev/null; then
				echo "Removed empty legacy folder: $folder"
			fi
		fi
	done
	shopt -u nullglob
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
SYNC=true
DRY_RUN=false
CONCURRENCY=1
ORPHANS=false
RECURSE_SUBMODULES=false

while [[ $# -gt 0 ]]; do
	case "$1" in
	-h | --help)
		show_help
		exit 0
		;;
	-c | --concurrency)
		if [[ -z "${2:-}" ]]; then
			echo "ERROR: --concurrency requires a value" >&2
			exit 1
		fi
		CONCURRENCY="$2"
		if [[ ! "$CONCURRENCY" =~ ^[1-9][0-9]*$ ]]; then
			echo "ERROR: --concurrency must be a positive integer" >&2
			exit 1
		fi
		shift 2
		;;
	-n | --dry-run)
		DRY_RUN=true
		shift
		;;
	-o | --orphans)
		ORPHANS=true
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
	--no-sync)
		SYNC=false
		shift
		;;
	--recurse-submodules)
		RECURSE_SUBMODULES=true
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

if [[ ! "$LIMIT" =~ ^[0-9]+$ ]]; then
	echo "ERROR: limit must be a positive integer" >&2
	exit 1
fi

for cmd in gh git; do
	if ! command -v "$cmd" &>/dev/null; then
		echo "ERROR: Required command '$cmd' not found. Please install it first." >&2
		exit 1
	fi
done

if ! gh auth status &>/dev/null; then
	echo "ERROR: gh is not authenticated. Please run 'gh auth login' first." >&2
	exit 1
fi

mkdir -p "$BASE_DIR"

repo_list="$(mktemp)" || { echo "Failed to create temp file" >&2; exit 1; }
counter_dir="$(mktemp -d)" || { echo "Failed to create temp dir" >&2; exit 1; }
touch "$counter_dir/events"
trap 'rm -rf "$repo_list" "$counter_dir"' EXIT

if ! gh repo list "$OWNER" --limit "$LIMIT" --json name,primaryLanguage,visibility,defaultBranchRef \
	--jq '.[] | [.name, (.primaryLanguage.name // "Other"), .visibility, (.defaultBranchRef.name // "main")] | @tsv' \
	>"$repo_list"; then
	echo "ERROR: gh repo list failed for owner '$OWNER'" >&2
	exit 1
fi

repo_count=$(wc -l < "$repo_list" | tr -d ' ')
if (( repo_count == LIMIT && LIMIT > 0 )); then
	echo "WARNING: Fetched exactly $LIMIT repositories. There may be more. Increase the limit argument if needed."
fi

process_repo() {
	local name="$1" lang="$2" visibility="$3" default_branch="$4"
	local lang_dir visibility_dir legacy_dir target_dir
	
	lang_dir="$(normalize_language "$lang")"
	visibility_dir="$(normalize_visibility "$visibility")"
	legacy_dir="$BASE_DIR/$lang_dir/$name"
	target_dir="$BASE_DIR/$visibility_dir/$lang_dir/$name"

	execute mkdir -p "$BASE_DIR/$visibility_dir/$lang_dir"

	if [[ -d "$target_dir/.git" ]]; then
		if [[ "$SYNC" == "true" ]]; then
			local current_branch
			current_branch=$(git -C "$target_dir" rev-parse --abbrev-ref HEAD 2>/dev/null || echo "")
			if [[ -n "$current_branch" && "$current_branch" != "$default_branch" ]]; then
				echo "WARNING: $OWNER/$name is on branch '$current_branch' (default is '$default_branch'), skipping sync"
				echo "existing" >> "$counter_dir/events"
			else
				echo "Syncing $OWNER/$name"
				local sync_cmd=(git -C "$target_dir" pull --rebase --autostash)
				[[ "$RECURSE_SUBMODULES" == "true" ]] && sync_cmd+=(--recurse-submodules)
				if execute "${sync_cmd[@]}"; then
					echo "synced" >> "$counter_dir/events"
				else
					echo "SYNC FAILED: $OWNER/$name"
					echo "failed" >> "$counter_dir/events"
				fi
			fi
		else
			echo "existing" >> "$counter_dir/events"
		fi
		return 0
	elif [[ -d "$target_dir" ]]; then
		echo "WARNING: $target_dir exists but is not a git repo, skipping"
		echo "existing" >> "$counter_dir/events"
		return 0
	fi

	if [[ -d "$legacy_dir" ]]; then
		if execute mv "$legacy_dir" "$target_dir"; then
			echo "moved" >> "$counter_dir/events"
		else
			echo "FAILED MOVE: $OWNER/$name (left at: $legacy_dir)"
			echo "failed" >> "$counter_dir/events"
		fi
		return 0
	fi

	echo "Cloning $OWNER/$name -> $visibility_dir/$lang_dir"
	local clone_cmd=(git clone)
	[[ "$RECURSE_SUBMODULES" == "true" ]] && clone_cmd+=(--recurse-submodules)
	clone_cmd+=("$(clone_url "$OWNER" "$name")" "$target_dir")

	if ! execute "${clone_cmd[@]}"; then
		echo "FAILED: $OWNER/$name (left at: $target_dir)"
		echo "failed" >> "$counter_dir/events"
	else
		echo "cloned" >> "$counter_dir/events"
	fi
}

job_count=0
while IFS=$'\t' read -r name lang visibility default_branch; do
	if (( CONCURRENCY > 1 )); then
		process_repo "$name" "$lang" "$visibility" "$default_branch" &
		(( ++job_count == CONCURRENCY )) && { wait; job_count=0; }
	else
		process_repo "$name" "$lang" "$visibility" "$default_branch"
	fi
done <"$repo_list"
wait

cloned=$(grep -c "^cloned$" "$counter_dir/events" || true)
existing=$(grep -c "^existing$" "$counter_dir/events" || true)
moved=$(grep -c "^moved$" "$counter_dir/events" || true)
failed=$(grep -c "^failed$" "$counter_dir/events" || true)
synced=$(grep -c "^synced$" "$counter_dir/events" || true)

cleanup_empty_legacy_language_folders

if [[ "$ORPHANS" == "true" ]]; then
	echo "--- Orphan Detection ---"
	orphans_found=0
	while IFS= read -r -d '' git_dir; do
		repo_dir="$(dirname "$git_dir")"
		repo_name="$(basename "$repo_dir")"
		
		remote_url=$(git -C "$repo_dir" remote get-url origin 2>/dev/null || true)
		if [[ "$remote_url" == *"/$OWNER/"* || "$remote_url" == *":$OWNER/"* ]]; then
			if ! grep -q "^${repo_name}"$'\t' "$repo_list"; then
				echo "Orphan found: $repo_dir"
				orphans_found=$((orphans_found + 1))
			fi
		fi
	done < <(find "$BASE_DIR" -name .git -type d -print0 2>/dev/null)
	if (( orphans_found == 0 )); then
		echo "No orphaned repositories found for $OWNER."
	fi
fi

summary="Done. Cloned $cloned repos, moved $moved existing repos, kept $existing repos"
if [[ "$SYNC" == "true" ]]; then
	summary+=", synced $synced repos"
fi
if [[ "$failed" -gt 0 ]]; then
	summary+=", $failed failures"
fi
echo "${summary}."
