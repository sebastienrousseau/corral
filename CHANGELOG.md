# Changelog

All notable changes to this project are documented here.
The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.0.9] — 2026-07-01

Docker distribution + MCP Registry submission.

### Added

- **Docker image published to `ghcr.io/sebastienrousseau/corral`** on
  every release. Multi-arch (linux/amd64 + linux/arm64) with the
  `io.modelcontextprotocol.server.name=io.github.sebastienrousseau/corral`
  ownership label required by the MCP Registry for OCI verification.
  Tags: `:<version>` (e.g. `:0.0.9`) and `:latest`.
- **`server.json`** at the repo root — the manifest consumed by the
  official `mcp-publisher` CLI (schema `2025-12-11`). Registers Corral
  in the MCP Registry under `io.github.sebastienrousseau/corral`.
- **README install-via-Docker snippet** for editors that cannot easily
  install a Go binary but can shell out to `docker run`.
- **`corral_find_repo` and resource-URI resolution now consult the
  remote origin URL**, so `corral://repo/{owner}/{name}/…` works when
  `{owner}` matches the GitHub org from `.git/config`'s origin URL —
  not only the layout's visibility directory. New
  `TestResolveURIRepoWithOwner` covers both HTTPS and SSH remote URL
  forms.

### Changed

- **`.goreleaser.yaml`** gains `dockers:` and `docker_manifests:`
  sections. `.github/workflows/release.yml` gains `packages: write`
  permission and SHA-pinned `docker/{login,setup-buildx,setup-qemu}-action`
  steps so goreleaser can push to ghcr.io during the release job.
- **`mcp.json` removed** — it was a speculative artifact that the
  registry does not consume. `server.json` is the canonical manifest.

## [0.0.8] — 2026-07-01

The MCP release. Corral becomes the canonical local index for AI coding
agents, alongside cron-grade cancellation visibility, a docs migration
to native GitHub Pages publishing on a custom domain, and every
GitHub-owned Action SHA-pinned to close 7 open OpenSSF Scorecard alerts.

### Added

- **`corralctl mcp` subcommand** — a Model Context Protocol server on
  stdio that exposes the local Corral-organised workspace to AI coding
  agents (Claude Code, Cursor, Cline, Codex CLI, Aider). Read-only in
  v0; ships five tools (`corral_list_repos`, `corral_find_repo`,
  `corral_get_repo_metadata`, `corral_status_summary`,
  `corral_workspace_index`) and four resources
  (`corral://workspace/index`, `corral://repo/{owner}/{name}/state`,
  `corral://repo/{owner}/{name}/tree`,
  `corral://repo/{owner}/{name}/file/{path}`). Sandboxes to a
  configurable `--root` (defaults to `--base-dir`); the file resource
  is bounded at 1 MiB with path-traversal defence canonicalising both
  the root and the candidate via `EvalSymlinks`. Reserved
  `--enable-mutations` flag is a placeholder for the Phase-3 write
  tools planned in v0.0.9.
- **`mcp.json` registry manifest** at the repo root for submission to
  `registry.modelcontextprotocol.io`.
- **Cancellation visibility for scripted callers.** When a run is
  interrupted by SIGINT/SIGTERM, the JSON output payload now carries
  `summary.canceled: true`, NDJSON emits a terminal
  `{"action":"CANCELED",...}` record, and the non-TTY text path logs a
  single `operation canceled (…)` line. The interactive TUI path stays
  silent (no regression of the existing UX). Exit code on cancellation
  is now `130` (POSIX 128+SIGINT) instead of `0`, so scripts can
  distinguish an aborted run from a clean one.

### Changed

- **Docs site** now publishes via the native GitHub Pages workflow
  (`actions/upload-pages-artifact` + `actions/deploy-pages`) instead of
  `peaceiris/actions-gh-pages`. The legacy `gh-pages` branch has been
  deleted.
- **Documentation URL** moved to <https://doc.corrallib.com> with HTTPS
  enforced via Let's Encrypt-issued cert.
- **Orphan detection is skipped on cancellation.** A mid-run abort can
  leave the local tree in a partial state where orphan reporting would
  be misleading.

### Security

- **All GitHub-owned Actions are now SHA-pinned** in `ci.yml` and
  `docs.yml`, closing the 7 open `PinnedDependenciesID` OpenSSF
  Scorecard / CodeQL alerts (#11, #12, #17, #21, #22, #23, #24).
  Convention matches `release.yml` and `scorecard.yml`: immutable SHA
  followed by `# vX.Y.Z` comment.

### Stats

- 7 packages, 100 % doc coverage (56 / 56 exported symbols).
- `internal/mcp` ships at 88.4 % statement coverage with 26 new tests
  covering scan / find / SafePath traversal defence / every tool and
  every resource.

## [0.0.7] — 2026-06-30

The first release after the binary rename to `corralctl`. Smart sync,
interactive TUI, `exec` subcommand, layout templating, and a complete
cron-safety overhaul.

### Added

- **Smart sync** — every clone now carries a `.corral-state.json` sidecar
  recording the last-observed upstream `pushed_at`. Subsequent runs skip
  the `git pull` round-trip when nothing has changed upstream, delivering
  10×–50× faster syncs on read-mostly workspaces.
- **`--force-sync`** flag to bypass the sidecar cache and pull regardless.
- **`--ignore-submodule-failures`** flag — with `--recurse-submodules`,
  swallow submodule update errors so a single inaccessible nested repo
  doesn't block the parent sync.
- **`--layout`** flag — text/template path renderer with vars `{{.Owner}}`,
  `{{.Name}}`, `{{.Visibility}}`, `{{.Language}}`, `{{.Fork}}`,
  `{{.Archived}}`. Default preserves `Visibility/Language/Name`.
- **`corralctl exec`** — concurrent batch executor for arbitrary shell
  commands across all (or a filtered subset of) cloned repos. Supports
  `--languages`, `--exclude-languages`, `--visibility`, `--concurrency`,
  and `--dry-run`.
- **Interactive TUI selector** (`--select`) with slash commands
  (`/help`, `/exit`, `/all`, `/none`, `/sort name|language|visibility`,
  `/sort public|private`, `/sort <language>`), Tab autocomplete,
  `topic:` / `language:` search queries, default-select-all, brand
  footer, and AltScreen mode (no scrollback pollution).
- **Concurrent GitHub API pagination** — pages 2…N are fetched in
  parallel (max 5 in-flight) once the first response advertises
  `resp.LastPage`. Sequential fallback for endpoints that don't report
  it. Substantial speed-up on accounts/orgs with hundreds of repos.
- **`git` binary pre-resolution** — `exec.LookPath("git")` runs once at
  startup; a missing `git` exits 1 with a clear error instead of failing
  mid-clone with a noisier message.
- **Subprocess-free orphan detection** — `RemoteOriginFromConfig` parses
  `.git/config` directly, ~5–15 ms saved per repo over spawning
  `git remote get-url origin`.
- **Documentation coverage CI gate** at 100 % (40 / 40 exported symbols)
  via `scripts/doc_coverage.go`.
- **GitHub Pages site** (`https://sebastienrousseau.github.io/corral/`)
  generated from `scripts/generate_docs.go` and deployed via
  `peaceiris/actions-gh-pages` on every push to `main`.
- **Animated terminal demo** (`demo.gif`) embedded in the README.
- **README architecture diagram** restored (mermaid) covering the full
  flow from API fetch through worker pool to summary.
- **CHANGELOG.md** — this file.

### Changed

- **One-time language-directory case normalisation** — on case-insensitive
  filesystems (APFS, HFS+, NTFS), pre-existing title-case folders like
  `Public/JavaScript/` are renamed to the documented lowercase form
  (`Public/javascript/`) on the next run. Unrelated dirs (e.g.
  `Public/Configurations/`) are untouched. Idempotent.
- **Strict non-interactive `git` environment** — every clone/pull now
  sets `GIT_TERMINAL_PROMPT=0`, `GIT_ASKPASS=/bin/true`,
  `SSH_ASKPASS=/bin/true`, `GCM_INTERACTIVE=Never`, and the rebase replay
  overrides `commit.gpgsign=false` + `gpg.format=openpgp`. Cron jobs can
  no longer hang on a credential prompt, SSH passphrase, or GPG/SSH
  signing pinentry, even when the user has `commit.gpgsign=true` set
  globally.
- **Version is now `-ldflags` injected** in both `Makefile` (via
  `git describe --tags --always --dirty`) and `.goreleaser.yaml`, into
  both `cmd.Version` *and* `internal/tui.Version`. The hard-coded
  fallback is now `"dev"` so an un-injected build is obvious instead of
  pretending to be `0.0.6`.
- **README rewritten** to a flatter, scannable layout (Quick Start →
  Features → Architecture → TUI → Layouts → Smart Sync → Exec → Flags →
  Examples → Troubleshooting → FAQ).
- **`Pull` signature** is now `Pull(ctx, dir, PullOptions)` instead of
  `Pull(ctx, dir, recurseSubmodules bool)`. **Breaking** for direct
  callers of `internal/git`; the engine layer is unaffected.
- **`internal/github.Repo`** carries a `PushedAt time.Time` field
  populated from the API response.
- **Default binary name** is `corralctl` (was `corral`, renamed in v0.0.6
  to avoid clashing with the `corral` formula in `homebrew-core`).
  Project name and import path are unchanged.
- **SBOM** refreshed: `go-github` v60 → v74 (matches `go.mod`); removed
  stale `golang.org/x/oauth2` reference (auth uses go-github's
  `WithAuthToken` helper now); Go toolchain pin 1.21 → 1.26.

### Fixed

- **`.corral-state.json` and `public/index.html` leaks** — both were
  accidentally tracked in version control. Now in `.gitignore`.
- **README absolute filesystem links** (`file:///Users/seb/...`) replaced
  with relative paths.
- **`runExecCommands` test coverage** lifted from 0 % to 91 % — the
  flagship `exec` path is now exercised under the race detector,
  including success / non-zero exit / pre-cancelled context / empty
  input / no-matching-repos branches.
- **`tui.go:57`** double-slash comment typo (`// // Init …`).
- **Layout `--orphans` walk** now uses `.git/config` parsing instead of
  per-repo `git remote get-url origin` subprocess spawns.

### Security

- All commits and merge commits are cryptographically signed
  (ED25519 / GPG); CI verifies signatures.
- CI actions remain pinned to immutable SHAs.
- Dependency Review, CodeQL, OpenSSF Scorecard, and gosec checks gate
  every PR.

### Stats

- 6 packages, 88.9 % statement coverage (up from 86.2 % mid-cycle),
  100 % doc coverage.
- All tests green under `-race -count=1`.

[Unreleased]: https://github.com/sebastienrousseau/corral/compare/v0.0.9...HEAD
[0.0.9]: https://github.com/sebastienrousseau/corral/compare/v0.0.8...v0.0.9
[0.0.8]: https://github.com/sebastienrousseau/corral/compare/v0.0.7...v0.0.8
[0.0.7]: https://github.com/sebastienrousseau/corral/compare/v0.0.6...v0.0.7
