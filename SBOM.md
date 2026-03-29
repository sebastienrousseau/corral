# Software Bill of Materials (SBOM)

**Project:** Corral
**Generated:** 2026-03-29
**Format:** Manual (no package manager dependencies)

## Runtime Dependencies

| Component | Type | Version Constraint | License | Source | Verification |
|:----------|:-----|:-------------------|:--------|:-------|:-------------|
| Bash | System shell | 4.0+ | GPL-3.0 | OS package manager | `bash --version` |
| Git | CLI tool | Any | GPL-2.0 | OS package manager | `git --version` |
| GitHub CLI (`gh`) | CLI tool | Any | MIT | [cli.github.com](https://cli.github.com/) | `gh --version` |

## Development Dependencies

| Component | Type | Version Constraint | License | Source | Verification |
|:----------|:-----|:-------------------|:--------|:-------|:-------------|
| ShellCheck | Linter | Any | GPL-3.0 | OS package manager | `shellcheck --version` |
| BATS | Test framework | Any | MIT | OS package manager | `bats --version` |
| GNU Make | Build tool | Any | GPL-3.0 | OS package manager | `make --version` |

## CI Dependencies

| Component | Version | SHA | License |
|:----------|:--------|:----|:--------|
| actions/checkout | v4 | `34e114876b0b11c390a56381ad16ebd13914f8d5` | MIT |

## Third-Party Code

None. This project contains no vendored, bundled, or embedded third-party source code.

## Supply Chain Notes

- All dependencies are well-established open-source tools installed via OS package managers.
- No transitive dependency trees. No lock files required.
- CI action pinned to immutable SHA, not mutable tag.
- All commits cryptographically signed (ED25519).
