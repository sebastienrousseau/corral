# Software Bill of Materials (SBOM)

**Project:** Corral
**Format:** Go Modules (`go.mod` / `go.sum`)

Corral has been migrated from a single-file Bash script to a compiled Go application. 
The canonical source of truth for all runtime dependencies, version constraints, and cryptographic checksums is the `go.mod` and `go.sum` files located at the root of the repository.

## Core Dependencies

| Component | Purpose | License |
|:----------|:--------|:--------|
| `github.com/spf13/cobra` | CLI Framework | Apache 2.0 |
| `github.com/google/go-github/v60` | GitHub API Client | BSD-3-Clause |
| `golang.org/x/oauth2` | OAuth2 Client (GitHub Auth) | BSD-3-Clause |
| `github.com/charmbracelet/bubbletea` | TUI Architecture | MIT |
| `github.com/charmbracelet/lipgloss` | TUI Styling | MIT |
| `github.com/charmbracelet/bubbles` | TUI Components | MIT |
| `github.com/mattn/go-isatty` | Terminal Detection | MIT |

## System Dependencies

| Component | Type | Version Constraint | License | Source | Verification |
|:----------|:-----|:-------------------|:--------|:-------|:-------------|
| Git | CLI tool | Any | GPL-2.0 | OS package manager | `git --version` |

## Development & Build Dependencies

| Component | Type | Version Constraint | License |
|:----------|:-----|:-------------------|:--------|
| Go | Compiler/Toolchain | 1.21+ | BSD-3-Clause |
| GoReleaser | Release Automation | v2+ | MIT |
| GNU Make | Build tool | Any | GPL-3.0 |

## Supply Chain Notes

- All third-party code is managed via Go modules with cryptographic checksums recorded in `go.sum`.
- Binaries are compiled natively via GitHub Actions and GoReleaser.
- All commits are cryptographically signed (ED25519/GPG).
