<!-- SPDX-License-Identifier: Apache-2.0 OR MIT -->

<p align="center">
  <img src=".github/logo.svg" alt="Corral logo" width="128" />
</p>

<h1 align="center">Corral</h1>

<p align="center">
  Automatically clone and organise GitHub repositories by visibility and language.
</p>

<p align="center">
  <a href="https://github.com/sebastienrousseau/corral/actions"><img src="https://img.shields.io/github/actions/workflow/status/sebastienrousseau/corral/ci.yml?style=for-the-badge&logo=github" alt="Build Status" /></a>
  <a href="https://pkg.go.dev/github.com/sebastienrousseau/corral"><img src="https://img.shields.io/badge/go.dev-reference-007d9c?style=for-the-badge&logo=go&logoColor=white" alt="Go Reference" /></a>
  <a href="https://goreportcard.com/report/github.com/sebastienrousseau/corral"><img src="https://img.shields.io/goreportcard/report/github.com/sebastienrousseau/corral?style=for-the-badge" alt="Go Report Card" /></a>
  <a href="https://codecov.io/gh/sebastienrousseau/corral"><img src="https://img.shields.io/codecov/c/github/sebastienrousseau/corral?style=for-the-badge&logo=codecov" alt="Code Coverage" /></a>
  <a href="https://scorecard.dev/viewer/?uri=github.com/sebastienrousseau/corral"><img src="https://img.shields.io/ossf-scorecard/github.com/sebastienrousseau/corral?style=for-the-badge&label=OpenSSF%20Scorecard&logo=openssf" alt="OpenSSF Scorecard" /></a>
  <a href="https://www.bestpractices.dev/projects/13455"><img src="https://www.bestpractices.dev/projects/13455/badge" alt="OpenSSF Best Practices" /></a>
  <a href="https://doc.corrallib.com"><img src="https://img.shields.io/badge/docs-doc.corrallib.com-brightgreen?style=for-the-badge&logo=github" alt="Documentation" /></a>
  <a href="https://github.com/sebastienrousseau/corral/releases/latest"><img src="https://img.shields.io/github/v/release/sebastienrousseau/corral?style=for-the-badge" alt="Release Version" /></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-GPL--3.0-blue?style=for-the-badge" alt="License" /></a>
</p>

<p align="center">
  <img src=".github/demo.gif" alt="Corral Demo" width="100%" />
</p>

---

## Contents

**Getting started**

- [Install](#install) — Homebrew, Arch, source, or Docker
- [Quick Start](#quick-start) — clone and organise in one command

**Features & Capabilities**

- [Features](#features) — structured layout, concurrency, and security
- [Architecture](#architecture) — end-to-end flow from API fetch to per-repo dispatch
- [Interactive TUI Mode](#interactive-tui-mode) — keybindings, commands, and autocomplete
- [Layout Customization](#layout-customization) — templated visibility and language organization
- [Smart Syncing](#smart-syncing) — network-optimised incremental updates
- [Exec Mode](#exec-mode) — concurrent batch execution of Git commands
- [MCP Server](#mcp-server-for-ai-agents) — expose your local workspace to AI coding agents

**Reference & Operational**

- [Usage & Flags](#usage--flags) — complete CLI parameter reference
- [Examples](#examples) — index of runnable programmatic examples
- [Troubleshooting](#troubleshooting) — quick solutions to common errors
- [Frequently Asked Questions](#frequently-asked-questions) — design decisions and Windows/WSL support
- [License](#license)

---

## Install

### Homebrew (macOS / Linux)

```bash
brew install sebastienrousseau/tap/corralctl
```

### Arch Linux (AUR)

```bash
yay -S corralctl-bin    # or: paru -S corralctl-bin
```

### Build from source

Requires Go 1.26+ and Git:

```bash
git clone https://github.com/sebastienrousseau/corral.git
cd corral
make build              # compiles ./corralctl
```

### Platform Prerequisites

<details>
<summary><b>macOS</b></summary>

```bash
brew install go git gh
```
</details>

<details>
<summary><b>Ubuntu / Debian / WSL2</b></summary>

```bash
sudo apt install golang git
```

Install `gh` separately following the [GitHub CLI installation guide](https://github.com/cli/cli/blob/trunk/docs/install_linux.md).
</details>

<details>
<summary><b>Fedora / RHEL</b></summary>

```bash
sudo dnf install golang git gh
```
</details>

---

## Quick Start

Run Corral with an owner name (GitHub username or organization) to clone and automatically sort all repositories into a clean local directory hierarchy:

```bash
# Log in to GitHub CLI first (or set GITHUB_TOKEN)
gh auth login

# Run Corral for your profile
./corralctl my-username
```

This converges your local directory structure into a structured mirror:

```
~/Code/
├── Public/
│   ├── go/
│   │   └── corral/
│   ├── rust/
│   │   └── my-crate/
│   └── other/
│       └── dotfiles/
└── Private/
    └── python/
        └── internal-tool/
```

---

## Features

| Feature | Description |
| :--- | :--- |
| **Structured Layout** | Automatically sorts repositories into `Public/` and `Private/` trees, sub-grouped by primary language (e.g. `go`, `rust`, `python`). |
| **Smart Syncing** | Compares remote `pushed_at` metadata to skip redundant network calls, speeding up syncs by 10x-50x. |
| **Interactive Selection** | A fully featured Terminal UI (TUI) selector dashboard to search, preview, and select repositories to clone. |
| **Legacy Migration** | Automatically moves existing flat directory layouts into the new structure and cleans up empty folders. |
| **Concurrency** | Processes clones and pulls concurrently with configurable worker limits (`--concurrency`). |
| **Batch Commands** | Batch execute Git commands concurrently across all cloned repositories using `exec`. |
| **Zero Configuration** | No configuration files required — simple, sensible defaults that work out of the box. |

---

## Architecture

A single run resolves git, fetches every repository concurrently from GitHub, optionally lets you pick a subset interactively, then dispatches clone / smart-sync / skip decisions across a worker pool. Smart sync consults a per-repository `.corral-state.json` sidecar to skip a `git pull` when the upstream `pushed_at` is unchanged.

```mermaid
graph TD
    A[User Shell] --> B{corralctl}
    B --> C[Pre-flight: exec.LookPath git]
    C -- Missing --> Z1[Exit: git not found on PATH]
    C -- OK --> D[Resolve auto/token/gh auth]
    D --> E[GitHub API: list repos]
    E --> E1["First page<br/>+ resp.LastPage"]
    E1 --> E2{LastPage > 1?}
    E2 -- Yes --> E3["Concurrent fetch<br/>pages 2..N (max 5)"]
    E2 -- No --> F
    E3 --> F[Filtered repository set]
    F --> F1{TUI selector?}
    F1 -- "--select" --> F2[Interactive TUI<br/>/sort, /all, /none, search]
    F1 -- No --> G
    F2 --> G[Layout template render<br/>Visibility/Language/Name]
    G --> H["Worker pool<br/>(--concurrency)"]
    H --> I{Already cloned?}
    I -- No --> J["git clone (+ blobless/<br/>depth/single-branch)"]
    I -- "Yes (--no-sync)" --> K[SKIP]
    I -- Yes --> L{Smart sync:<br/>pushed_at advanced?}
    L -- No --> M[SKIP up-to-date]
    L -- "Yes (or --force-sync)" --> N[git pull --rebase --autostash]
    N --> N1["+ optional submodule update<br/>(--ignore-submodule-failures)"]
    J & N1 --> O[Stamp .corral-state.json]
    O & K & M --> P{All workers done?}
    P -- No --> H
    P -- Yes --> Q[Cleanup empty legacy dirs]
    Q --> R{--orphans?}
    R -- Yes --> S[Walk baseDir<br/>parse .git/config]
    R -- No --> T[Print summary]
    S --> T
```

---

## Interactive TUI Mode

By passing the `-i` or `--interactive` flag, you can launch the selection dashboard:

```bash
./corralctl -i my-username
```

### Keybindings

- `[space]` — Toggle selection of the current repository.
- `[ctrl+a]` — Select all currently filtered repositories.
- `[ctrl+n]` — Deselect all currently filtered repositories.
- `[/]` — Enter command / filter mode.
- `[enter]` — Confirm selection and begin cloning/syncing.
- `[esc]` — Exit the application silently.

### In-Session Commands

Press `/` inside the TUI to enter Command Mode. Commands support prefix-based autocompletion (press `[tab]` or `[right-arrow]` to autocomplete):

- `/sort <field>` — Sort repositories. Fields:
  - `name` — Alphabetical sort by repository name.
  - `language` / `lang` — Alphabetical sort by language.
  - `visibility` / `vis` — Alphabetical sort by visibility (Private/Public).
  - `public` — Prioritize public repositories at the top.
  - `private` — Prioritize private repositories at the top.
- `/all` — Select all filtered repositories.
- `/none` — Deselect all filtered repositories.
- `/exit` / `/quit` — Cancel and exit silently.
- `/help` — Display the in-session help panel overlay.

---

## Layout Customization

By default, Corral uses the layout `{{.Visibility}}/{{.Language}}/{{.Name}}`. You can override this using the `--layout` flag:

```bash
./corralctl --layout "{{.Owner}}/{{.Name}}" my-org
```

Supported placeholders:
* `{{.Owner}}` — GitHub owner name.
* `{{.Name}}` — Repository name.
* `{{.Language}}` — Primary language normalized to lowercase.
* `{{.Visibility}}` — Repository visibility (`Public` or `Private`).

---

## Smart Syncing

Corral stores synchronization metadata next to each repository's `.git/` folder inside a `.corral-state.json` sidecar file:
* **No Redundant Pulls:** If the remote repository has not received new pushes since the last sync, `git pull` is skipped completely.
* **Overrides:** To bypass smart checks and force Corral to perform a full `git pull`, pass the `--force-sync` flag.
* **Skip Syncing entirely:** Pass `--no-sync` to skip updates on all cloned repositories.

---

## Exec Mode

Execute arbitrary shell commands concurrently across your organized repositories:

```bash
# Check git status for all Go/Rust private repositories
./corralctl exec "git status -s" --languages go,rust --visibility private
```

---

## MCP Server (for AI agents)

Corral ships a Model Context Protocol server that exposes your local, Corral-organised workspace to AI coding agents — Claude Code, Cursor, Cline, Codex CLI, Aider, and anything else that speaks MCP. **No network calls are made and the GitHub API is not contacted**; the server is a read-only window into the clones already on disk.

Where GitHub's own MCP server covers the remote API surface (issues, PRs, search), `corral-mcp` covers the dimension only it can — your *local mirror*, organised by visibility and language, queryable without a round-trip.

### Tools

| Name | Purpose |
| :--- | :--- |
| `corral_list_repos` | Filter local clones by visibility / language / name / sync state |
| `corral_find_repo` | Resolve a fuzzy name to one clone (returns candidates on ambiguity) |
| `corral_get_repo_metadata` | Full metadata for one clone, including current branch |
| `corral_status_summary` | Workspace summary: counts by visibility and language |
| `corral_workspace_index` | Full structured index in a single call |

### Resources

- `corral://workspace/index`
- `corral://repo/{owner}/{name}/state`
- `corral://repo/{owner}/{name}/tree`
- `corral://repo/{owner}/{name}/file/{path}` (bounded at 1 MiB; path-traversal protected)

### Install

**Claude Code:**

```bash
claude mcp add corral -- corralctl mcp
```

**Cursor / Cline** (`mcp.json`):

```json
{
  "mcpServers": {
    "corral": {
      "command": "corralctl",
      "args": ["mcp"]
    }
  }
}
```

**Docker (no local install required)** — the same binary the MCP Registry advertises, mounted against your workspace:

```json
{
  "mcpServers": {
    "corral": {
      "command": "docker",
      "args": [
        "run", "--rm", "-i",
        "--user", "1000:1000",
        "-v", "${HOME}/Code:/workspace:ro",
        "ghcr.io/sebastienrousseau/corral:latest",
        "mcp", "--root", "/workspace"
      ]
    }
  }
}
```

Notes on the args:

- **`--user 1000:1000`** — replace with your host UID:GID (`id -u`:`id -g`) so the containerised scanner reads the mounted workspace with the same permissions your host user has. Without this the image runs as a system UID inside the container and hits `permission denied` on any directory your workspace makes group- or user-private.
- **`-v … :ro`** — read-only mount. The v0 tools are read-only anyway; mounting `:ro` documents that and defends against a hostile agent asking the server for a write it doesn't have.
- **`--root /workspace`** — sandbox root inside the container. Every tool and resource path check is scoped to this prefix; requests outside it are rejected regardless of what the agent asks for.

**Sandbox a different root** (defaults to `--base-dir`, then `$HOME/Code`):

```bash
corralctl mcp --root /custom/workspace
```

### Safety

- **Read-only by default.** Phase-3 write tools (`corral_sync_repo`, `corral_clone_repo`) are reserved for a follow-up release; `--enable-mutations` is a placeholder flag today.
- **Path-traversal protected.** File-resource lookups canonicalise both the configured root and the candidate path before comparison, so a malicious `{path}` cannot escape the sandbox via `..` or symlinks.
- **stdio-only.** No HTTP endpoint, no listening port — the server only ever speaks to the parent process that launched it.

---

## Usage & Flags

### Positional Arguments

```bash
corralctl <owner> [base_dir] [limit]
```

- `<owner>` — GitHub username or organization (Required).
- `[base_dir]` — Root directory to save repositories (Default: `$HOME/Code`).
- `[limit]` — Maximum repositories to fetch (Default: `1000`).

### Command Options

| Option | Short | Default | Description |
| :--- | :--- | :--- | :--- |
| `--base-dir` | — | `$HOME/Code` | Root directory for cloned repos |
| `--limit` | `-l` | `1000` | Maximum repositories to fetch |
| `--concurrency` | `-c` | `1` | Number of concurrent worker threads |
| `--dry-run` | `-n` | off | Preview actions without making changes |
| `--orphans` | `-o` | off | Detect local repositories no longer on GitHub |
| `--protocol` | `-p` | `https` | Protocol to clone: `ssh` or `https` |
| `--no-sync` | — | off | Skip pulling latest changes for existing clones |
| `--force-sync` | — | off | Force git pull regardless of cached state |
| `--layout` | — | `...` | Templated path layout for repositories |
| `--interactive` | `-i` | off | Launch the interactive selector TUI dashboard |
| `--recurse-submodules`| — | off | Initialise submodules on clone and sync |
| `--output` | — | `text` | Output format: `text`, `json`, or `ndjson` |
| `--auth` | — | `auto` | Auth mode: `auto`, `token`, or `gh` |
| `--visibility` | — | `all` | Filter by visibility: `all`, `public`, `private` |
| `--include-forks` | — | off | Include forked repositories |
| `--include-archived` | — | off | Include archived repositories |
| `--languages` | — | — | Comma-separated language filter (e.g. `go,rust`) |
| `--exclude-languages`| — | — | Comma-separated language exclude list |
| `--clone-depth` | — | `0` | Shallow clone depth (`0` disables shallow clone) |

---

## Examples

To inspect the package layout and programmatically run Corral modules, see the self-contained, copy-pasteable Go code examples in the [examples](examples/) directory:

1. **[Interactive Selector](examples/interactive_selection.go)** — Programmatically configure and launch the selection checklist TUI in AltScreen mode.
2. **[GitHub Repository Fetcher](examples/github_fetch.go)** — Query the GitHub REST API using `github.FetchReposWithOptions` with stars sorting and language constraints.
3. **[Git Syncing](examples/git_clone.go)** — Call the `git` helper package to perform clones, query branches, and resolve origin URLs.
4. **[Engine Orchestrator](examples/engine_run.go)** — Integrate the core engine `engine.Run` to run repository syncing with custom filters, layout structures, and dry-run pre-flights.

---

## Troubleshooting

| Error Message | Cause | Solution |
| :--- | :--- | :--- |
| `ERROR: git not found on PATH` | Git is not installed or missing from the current PATH environment. | Install git via your package manager. |
| `ERROR: GITHUB_TOKEN environment variable not set` | `--auth token` was specified but no environment variable is present. | Run `export GITHUB_TOKEN=$(gh auth token)` or switch to `--auth auto`. |
| `FAILED: owner/repo` | Authentication error or network failure during clone/pull. | Check connectivity and confirm `gh auth status` displays a valid session. |

---

## Frequently Asked Questions

- **Does it work with GitLab or other hosts?**  
  No. Corral is specifically built to integrate with the GitHub API and GitHub CLI (`gh`).
- **What happens to repositories deleted on GitHub?**  
  Corral never deletes your local checkouts. To see repositories that no longer exist upstream, run Corral with the `--orphans` flag.
- **Can I run it inside Cron or systemd timers?**  
  Yes. The command runs non-interactively by default. All Git command credential prompts are bypassed to ensure automated jobs never hang.
- **How are repositories with no primary language stored?**  
  They default to the `other/` language category (e.g. `Public/other/my-repo`).

---

**THE ARCHITECT** ᛫ [Sebastien Rousseau](https://sebastienrousseau.com)  
**THE ENGINE** ᛞ [EUXIS](https://euxis.co) ᛫ Enterprise Unified Execution Intelligence System

---

## License

Licensed under the **[GNU General Public License v3.0](LICENSE)**.

<p align="right"><a href="#corral">Back to Top</a></p>
