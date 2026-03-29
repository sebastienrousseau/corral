# Corral

![](https://img.shields.io/github/actions/workflow/status/sebastienrousseau/Corral/ci.yml?style=flat-square&logo=github) ![](https://img.shields.io/github/v/release/sebastienrousseau/Corral?style=flat-square) ![](https://img.shields.io/badge/License-GPL--3.0-blue?style=flat-square)

Corral clones every repository from a GitHub user or organisation and sorts them into a clean, navigable directory tree by visibility and language — one command, zero config, safe to re-run.

```
~/Code/
├── Public/
│   ├── rust/
│   │   └── my-crate/
│   ├── typescript/
│   │   └── my-app/
│   └── other/
│       └── dotfiles/
└── Private/
    └── python/
        └── internal-tool/
```

## Get started

Requires `gh`, `git`, and Bash 4+.

1. Install prerequisites:

    **macOS:**
    ```bash
    brew install bash git gh
    ```

    **Ubuntu / Debian / WSL2:**
    ```bash
    sudo apt install git
    ```
    Install `gh` separately — see the [GitHub CLI install guide](https://github.com/cli/cli/blob/trunk/docs/install_linux.md).

    **Fedora / RHEL:**
    ```bash
    sudo dnf install git gh
    ```

2. Clone and run:

    ```bash
    git clone https://github.com/sebastienrousseau/Corral.git
    cd Corral
    gh auth login
    ./corral.sh <owner>
    ```

> [!NOTE]
> macOS ships with Bash 3.2. After installing Bash 4+ via Homebrew, make sure it appears first in your `$PATH` or invoke the script explicitly with `/opt/homebrew/bin/bash corral.sh <owner>`.

> [!NOTE]
> WSL2 users should run all commands inside their Linux distribution, not from PowerShell or CMD.

## Usage

```bash
./corral.sh [options] <owner> [base_dir] [limit]
```

| Parameter | Required | Default | Description |
| :--- | :--- | :--- | :--- |
| `-n`, `--dry-run` | No | — | Preview changes without executing them |
| `owner` | Yes | — | GitHub username or organisation |
| `base_dir` | No | `$HOME/Code` | Root directory for the cloned tree |
| `limit` | No | `1000` | Maximum repositories to fetch |

Clone a personal account:

```bash
./corral.sh my-username
```

Clone an organisation into a custom directory:

```bash
./corral.sh my-org ~/Projects 500
```

Preview what would happen without making changes:

```bash
./corral.sh --dry-run my-username
```

Run on a schedule — the script is idempotent and non-interactive:

```bash
# crontab -e
0 2 * * * /path/to/corral.sh my-username
```

Private repositories require a `gh` token with appropriate access. Public repositories from any account are always available.

## How it works

Corral fetches your repository list via `gh`, then for each repo:

1. **Already cloned?** Skip it.
2. **Legacy flat layout?** Migrate it to the new `<Visibility>/<Language>/` structure.
3. **New repo?** Clone it into the right directory.
4. **Cleanup** — remove any empty legacy language directories left behind.

Repos are cloned in parallel (configurable via `CORRAL_PARALLEL`, default 4). Language names are normalised: C# becomes `csharp`, C++ becomes `cpp`, spaces and slashes become underscores, null languages default to `other/`.

## Troubleshooting

| Message | Cause | Fix |
| :--- | :--- | :--- |
| `ERROR: Bash 4+ is required` | macOS ships with Bash 3.2 | `brew install bash` |
| `ERROR: Required command 'gh' not found` | GitHub CLI not installed | See [Get started](#get-started) |
| `ERROR: gh repo list failed` | Not authenticated or owner doesn't exist | `gh auth login` and check the owner name |
| `FAILED: owner/repo` | Network issue or missing token access | Check connectivity and `gh auth status` |
| Script reports 0 repos | No repos visible to the current token | `gh repo list <owner> --limit 5` |
| `\r: command not found` (WSL2) | Windows line endings | `dos2unix corral.sh` |

## Reporting bugs

File a [GitHub issue](https://github.com/sebastienrousseau/Corral/issues). For security vulnerabilities, see [SECURITY.md](SECURITY.md).

## License

[GNU General Public License v3.0](LICENSE)
