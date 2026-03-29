# GitHub Repo Cloner

Bulk-clone all repositories from a GitHub user or organisation, organised by visibility and primary language.

```
~/Code/
├── Public/
│   ├── Rust/
│   │   └── my-crate/
│   ├── TypeScript/
│   │   └── my-app/
│   └── Other/
│       └── dotfiles/
└── Private/
    └── Python/
        └── internal-tool/
```

The script is **idempotent** — re-running it skips repos that are already cloned and only fetches new ones.

## Prerequisites

| Tool | Install |
|------|---------|
| [Git](https://git-scm.com/) | macOS: `brew install git` / Linux & WSL: `sudo apt install git` |
| [GitHub CLI (`gh`)](https://cli.github.com/) | macOS: `brew install gh` / Linux & WSL: `sudo apt install gh` or see [install docs](https://github.com/cli/cli/blob/trunk/docs/install_linux.md) |

After installing `gh`, authenticate:

```bash
gh auth login
```

## Usage

```bash
./clone-gh-repos.sh [owner] [base_dir] [limit]
```

| Parameter | Default | Description |
|-----------|---------|-------------|
| `owner` | `sebastienrousseau` | GitHub username or organisation |
| `base_dir` | `$HOME/Code` | Root directory for the cloned tree |
| `limit` | `1000` | Maximum number of repos to list via `gh` |

### Examples

Clone with defaults:

```bash
./clone-gh-repos.sh
```

Clone a different org into a custom directory:

```bash
./clone-gh-repos.sh my-org ~/Projects 500
```

## Legacy migration

If you previously cloned repos into a flat `~/Code/<Language>/<repo>` layout, the script detects these and moves them into the new `<Visibility>/<Language>/<repo>` structure automatically. Empty legacy language folders are cleaned up after migration.

## Troubleshooting

| Symptom | Cause | Fix |
|---------|-------|-----|
| `ERROR: Required command 'gh' not found` | `gh` not installed | Install per Prerequisites above |
| `ERROR: gh repo list failed for owner '...'` | Not authenticated, or owner doesn't exist | Run `gh auth login` and verify the owner name |
| `FAILED: owner/repo` during clone | Network issue, repo deleted, or SSH vs HTTPS mismatch | Check connectivity; ensure `gh` is configured for HTTPS (`gh config set git_protocol https`) |
| Script succeeds but clones 0 repos | Owner has no public repos visible to your token | Run `gh repo list <owner> --limit 5` manually to verify |

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).
