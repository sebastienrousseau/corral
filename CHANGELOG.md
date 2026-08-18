# Changelog

All notable changes to this project are documented here.
The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.0.23] — 2026-08-18

### Changed

- **`google/go-github` upgraded v74 → v90**, sixteen major versions in one step.
  The surface corral uses is small — twelve symbols in a single non-test file —
  so a staged walk through each major would have been ceremony rather than
  safety. Two breaking changes had to be adapted:
  - `gh.NewClient` is now a variadic options constructor returning an error,
    and `WithAuthToken` moved from a chained client method to a
    `ClientOptionsFunc`.
  - `Client.BaseURL` is a read-only accessor; the base URL is set at
    construction through the `WithURLs` option. This only affected the test
    client.

### Added

- **Field-mapping tests covering every value corral reads from go-github**,
  decoded from a realistic API payload through go-github's own struct tags.
  Sixteen majors of a generated API client is exactly where a renamed JSON tag
  starts silently yielding zero values, and most such regressions would not
  fail a build: corral would simply report every repository as language
  "Other", visibility "Public", or with a zero `pushed_at` — which disables
  smart-sync by making every repository look never-synced. A full sync of
  everything looks like working software, so this needed an explicit
  assertion rather than an end-to-end smoke test. Confirmed to fail against a
  simulated mapping regression.


## [0.0.22] — 2026-08-18

### Fixed

- **The MCP registry publish reported success while publishing nothing.** The
  step was gated on an `MCP_REGISTRY_TOKEN` secret, but it authenticates with
  `mcp-publisher login github-oidc`, which uses the GitHub Actions OIDC token
  the job already holds via `id-token: write` and needs no secret at all. With
  the secret unset the step took its skip branch and exited 0, so the registry
  entry stayed at 0.0.13 through v0.0.21 — including the release where the
  publisher itself was finally working. Gate removed.


## [0.0.21] — 2026-08-18

Usability release. The v0.0.20 work made corral safe; this makes it
configurable.

### Fixed

- **28 of 31 flags were unreachable from the commands that consumed them.**
  `plan`, `prune` and `profile` all build their options from the same variables
  as the root command, but the flags were registered on the root command only:

      $ corralctl plan acme --limit 5
      unknown flag: --limit

  So `corralctl plan` always ran at limit=1000, concurrency=1, visibility=all —
  unconfigurable and unstated. The fetch/filter and clone/sync groups are now
  shared flag sets registered on each command that acts on them. `prune`
  deliberately gets only the fetch group, since it removes clones and never
  creates one.
- **The config file covered 5 settings of 31, and only `corralctl profile` read
  it.** Settings are now keyed by flag name — `"concurrency": 8` is exactly
  `--concurrency 8` — so the file covers the whole surface and picks up new
  flags automatically, and it applies to every command. Precedence, highest
  first: an explicit flag, the selected profile, the defaults block, the flag's
  own default. Configs written before this release keep working: the old
  snake_case profile fields are still parsed.
- **A mistyped setting used to be ignored silently.** An unknown key is now an
  error naming the key, because a setting the user believes took effect and did
  not is worse than one that fails. A key naming a flag owned by a *different*
  command is not an error — one file serves the whole CLI.
- **Config values were validated differently from typed ones.** A profile's
  settings went through a hand-written allow-list covering five fields. Every
  value now goes through the flag's own parser and the shared
  `validateCommonFlags()`, so `"protocol": "carrier-pigeon"` is rejected with
  the same message as `--protocol carrier-pigeon`. `plan`, `prune` and
  `profile` run that validation too; previously a bad value reached the engine,
  which printed an error and terminated the process.
- **`--concurrency` defaulted to 1**, so the documented concurrency feature was
  off unless asked for and the README's "10x-50x faster" claim rested entirely
  on `pushed_at` caching. It now sizes from the host, bounded to 4–8.
- **`go install` produced a binary named `corral`, not `corralctl`**, because
  the module basename is `corral`. `main.go` moved to `cmd/corralctl/`. This is
  the same repo-vs-binary mismatch that makes mise's `ubi:` backend install an
  unusable `corral` binary; `github:` is the backend that works.
- **The README advertised "Homebrew (macOS / Linux)"** while shipping a cask,
  which is a macOS-only mechanism — `brew install` on Linux refuses it. The
  README now says macOS and points Linux users at the .deb/.rpm packages and
  tarballs from the same release. (The cask stays: goreleaser has deprecated
  `brews:`.)

### Changed

- **MCP tool responses are bounded.** `corral_list_repos` and
  `corral_workspace_index` returned every match, indented, at ~526 bytes per
  repository: ~55,000 tokens for 500 repositories against a 25,000-token client
  budget, i.e. over budget from roughly 190 repositories. Both now page
  (`limit`, `offset`, `next_offset`; default 50, max 200) and default to a
  concise projection, with `response_format: "detailed"` for the full entry.
  Measured on a synthetic 500-repository workspace: **~55,000 tokens → ~1,957**.
- `corral_workspace_index`'s description no longer invites the most expensive
  call it can make; it points at `corral_list_repos` for filtered work.
- `Index.Truncated` is finally surfaced in tool payloads. It was set when a
  workspace exceeded the scan cap and never reported, so an over-cap workspace
  looked complete to the caller.
- `corral_list_repos` renames `count` to `total_matched`. With a page window a
  bare count is ambiguous between matched and returned.

### Added

- `corralctl config --init` writes a commented starter config documenting the
  real flag surface, and refuses to overwrite an existing file.
- `corralctl config --explain` reports each effective setting and where it came
  from. A layered config is only debuggable if you can ask it why a value is
  what it is.


## [0.0.20] — 2026-08-17

Safety release. Everything below was found by an audit of the v0.0.19 tree;
several of these were reachable from a single mistyped argument.

### Fixed

- **The sync sidecar no longer dirties every clone.** `.corral-state.json` was
  written into each clone's working tree, so `git status` reported every
  corral-managed repository as modified. Because `corralctl status`,
  `corralctl prune` and the MCP delete tool all refuse to act on a repository
  with local changes, `prune` could never prune anything and `status` reported
  every repo dirty. The sidecar now lives at `<gitdir>/corral-state.json`,
  which is outside the working tree by construction. The old location is still
  read, so existing clones keep their smart-sync state, and it is removed on
  the first write.
- **A typo'd subcommand no longer starts a live run.** `corralctl statuss` was
  a valid invocation meaning "owner=statuss" and began fetching from GitHub and
  cloning into `$HOME/Code`. Arguments within edit distance 2 of a real
  subcommand are now rejected with a suggestion; `corralctl [flags] -- <owner>`
  forces the owner reading.
- **Positional arguments no longer swallow `base_dir`.** Ten ordinary directory
  names — `forks`, `stars`, `name`, `public`, `private`, `templates` and others
  — were consumed as filter keywords, and the target directory silently fell
  back to `$HOME/Code`. Repository type and sort are now `--type` and `--sort`,
  and the positional grammar is the documented `<owner> [base_dir] [limit]`.
- **The preflight confirmation is no longer a no-op off-TTY.** It returned true
  whenever stdin was not a terminal, so it protected nobody in scripts, pipes,
  cron or CI. Creating a new target directory without a TTY now refuses, with a
  non-zero exit so callers can tell "did nothing" from "succeeded".
- **Files in subdirectories are readable over MCP.** The file resource used RFC
  6570 simple expansion, which does not match `/`, so nothing below a
  repository's top level resolved at all.
- **MCP file reads no longer expose credentials.** `.git` was hidden from the
  tree listing but not the file reader, so `.git/config` — and `.env`, `.npmrc`,
  private keys — were readable. Now denied, alongside `.ssh`, `.aws` and
  `.gnupg`.
- **A workspace root that is itself a repository no longer collapses the
  index.** The scan matched the root, appended it as the only entry and aborted,
  and `corral_delete_repo` could then resolve that entry back to the root.
- **Detached-HEAD commits block deletion.** The guard counted
  `rev-list --branches`, which covers `refs/heads/**` only, so work committed in
  detached HEAD was invisible and the delete proceeded. Widened to `--all`.
- **Gitignored content blocks deletion.** `git status --porcelain` excludes
  ignored files, so local `.env` files and databases — the least recoverable
  content in a clone — were destroyed silently.
- **Submodules with unpushed commits block deletion.**
- **`corralctl prune` refuses a truncated upstream listing.** It compared
  against at most `--limit` repositories, so for an owner with more than that,
  every repository past the cap looked like an orphan and was deleted.
- **Non-clone directories are no longer relocated.** Legacy migration treated a
  matching *name* as sufficient grounds for `os.Rename`, so an unrelated folder
  sharing a name with one of the owner's repositories was moved — unprompted,
  and invisible in `--dry-run`. Migration now requires a `.git` directory and a
  matching origin remote.
- **Errors print once.** Cobra printed them and then `ExecuteContext` printed
  them again, inside a full usage dump: 52 lines with the message duplicated at
  both ends, now 3.
- **A malformed `--layout` fails immediately** rather than after a full
  paginated GitHub fetch.
- **`server.json` is published.** Nothing shipped it, so the MCP registry entry
  sat at 0.0.13 across five releases, advertising a stale image tag. The release
  workflow now publishes it and fails if the file and the tag disagree.

### Changed

- **MCP tool annotations are set.** mcp-go's zero value serialises as
  `destructiveHint: true`, so all five read-only tools advertised themselves as
  destructive — and `corral_delete_repo` carried the identical annotation,
  making the signal worthless. Clients use these to decide whether to
  auto-approve, so reads were paying a confirmation tax while deletion gave no
  warning.
- **The MCP server sends `instructions`**, describing the on-disk layout, which
  tool to start with, and whether writes are enabled.
- Resource subscriptions are no longer advertised. The capability was announced
  and never implemented, so a subscribing client waited forever.
- Go toolchain 1.26.1 → 1.26.6 in `go.mod` and all four workflows.
- `mcp.json` replaced by `examples/mcp-client-config.json`. It was stale at
  0.0.8, invalid against the schema it declared, and used the filename
  convention of a *client* config — so copying it into `.cursor/mcp.json`
  produced a non-functional file.

### Added

- Machine-readable SPDX SBOMs per release archive. `SBOM.md` was
  hand-maintained and had already drifted, omitting `mcp-go` — a direct
  dependency powering the entire MCP server.
- A keyless cosign Sigstore bundle over `checksums.txt`
  (`checksums.txt.sigstore.json`), attached as a release asset, so a consumer
  who downloads a tarball has something next to it to verify against. Until now
  SLSA provenance lived only in GitHub's attestation store and the cosign
  signatures covered only the OCI images — neither is a release asset.
  Verify with `cosign verify-blob --bundle checksums.txt.sigstore.json ...`
  (see `.goreleaser.yaml` for the full command). Note this is the modern bundle
  format rather than a separate `.sig`/`.pem` pair, because cosign v3 removed
  `--output-signature`/`--output-certificate` from `sign-blob`; whether OpenSSF
  Scorecard's Signed-Releases check credits a `.sigstore.json` bundle has not
  been verified.
- `go test -race -shuffle=on`, fixed shuffle seeds, and `govulncheck` in CI.
- **Seam-binding tests.** The suite reported 99.8% coverage with seven packages
  at 100%, yet 18 of 33 injected mutants survived — every one a default
  indirection binding. Replacing `main`'s `executeContext` with a no-op turned
  the whole binary into a program that does nothing and the suite stayed green.
  Four tests now pin 30 seams to their production implementations.

### Security

- The two MCP fixes above are a pair: repairing the `{+path}` routing without
  the denylist would have converted an unreachable resource into a working
  credential-exfiltration primitive for any prompt-injected agent. Verified
  locally — with routing fixed and no denylist, reading `.git/config` returned a
  token.

## [0.0.19] — 2026-08-16

### Changed

- Dependency refresh: `github.com/mark3labs/mcp-go` 0.57.0 → 0.58.0 and
  `golang.org/x/sys` 0.46.0 → 0.47.0.
- Pinned GitHub Actions refreshed, including `github/codeql-action/upload-sarif`
  to v4.37.7 and `hadolint/hadolint-action` to v3.4.0. Dependabot now groups
  `github-actions` updates into a single pull request.

### Fixed

- The Dockerfile runtime user is created with an explicit numeric uid/gid
  (`65532:65532`) so Kubernetes `runAsNonRoot` can verify the container is not
  root, and hadolint 2.15.0's DL3066 is satisfied.
- `server.json` now tracks the released version. It had been stale at 0.0.13
  since that release, so the MCP registry entry advertised an outdated
  `ghcr.io/sebastienrousseau/corral:0.0.13` image tag.

## [0.0.18] — 2026-08-02

### Changed

- Release container tooling now uses the Node.js 24-compatible Docker QEMU,
  Buildx, and registry login actions pinned to immutable release commits.

## [0.0.17] — 2026-08-01

### Added

- Native macOS Finder Tags for repository lifecycle, visibility, ecosystem,
  ownership, and repository type while preserving user-managed tags.
- Canonical `Public`, `Private`, `Forks`, and `Work` collection folders.

### Changed

- The default layout now uses Finder-facing ecosystem buckets such as `Go`,
  `Rust`, `Python`, and `Web`. Forks are separated under `Forks` and archived
  repositories are included by default so they can be tagged `On Hold`.

### Fixed

- Repositories whose names end in `.github.io` are always organized under the
  `Web` bucket, regardless of GitHub's detected primary language.
- Repository discovery reports every duplicate clone location instead of
  silently retaining whichever matching remote was encountered first.
- Dry runs no longer perform legacy migrations, case normalization, collection
  creation, or empty-folder cleanup.

### Performance

- Local discovery prunes dependency, build, cache, and virtual-environment
  trees, reducing a 185-repository layout audit from roughly 90 seconds to
  about 2.5 seconds on the reference workspace.

## [0.0.16] — 2026-08-01

### Fixed

- Case-only path aliases on case-insensitive filesystems no longer produce
  false target-collision errors. Corral now compares filesystem identity before
  deciding whether an existing clone and desired target are distinct.

## [0.0.15] — 2026-08-01

### Fixed

- Homebrew cask updates now open pull requests against the protected tap.
- AUR availability no longer blocks GitHub artifacts, checksums, container
  images, or build-provenance attestations.

## [0.0.14] — 2026-08-01

Security hardening, operational resilience, and complete unit coverage.

### Added

- Native `make install` support, installing `corralctl` under
  `~/.local/bin` by default with `PREFIX` and `DESTDIR` overrides.
- Direct mise installation through the GitHub release backend.
- Bounded repository discovery and MCP indexing, cancellation-aware git
  execution, mutation audit durability, and stricter workspace containment.
- Full tests for command, engine, git, GitHub, MCP, TUI, and entry-point paths,
  bringing project statement coverage to 100%.

### Security

- Repository paths, clone URLs, redirects, response bodies, git output, audit
  records, and concurrent work are now validated or explicitly bounded.
- Unsafe Git transports, credential-bearing URLs, symlink escapes, ambiguous
  remotes, insecure API origins, and option-like repository arguments fail
  closed before network or filesystem mutation.
- Release tags are verified as semantic versions pointing at commits on
  `main`, and release source is tested before packaging.

### Maintenance

- GitHub Actions and Go dependencies were updated to their current pinned
  releases.
- Governance, assurance-case, DCO, SPDX, and fuzzing coverage were expanded.

### Governance

- **Per-file SPDX headers** — every `.go` file now carries
  `SPDX-FileCopyrightText` and `SPDX-License-Identifier: GPL-3.0-only`
  headers at the top (after any `//go:build` constraint). Applied via
  a one-shot codegen tool committed at `scripts/spdx_sweep.go`, safe
  to re-run on new files. Satisfies CII Best Practices Silver
  `copyright_per_file` and `license_per_file` criteria.
- **DCO enforcement** — every PR commit must carry a matching
  `Signed-off-by:` trailer, checked by a new
  `.github/workflows/dco.yml`. Contributor flow (`git commit -s`,
  `git rebase --signoff`) documented in `CONTRIBUTING.md`. Satisfies
  the CII Best Practices Silver `dco` criterion.
- **Formal assurance case** at `docs/security-model.md` — trust
  boundaries, security claims C1–C5 with linked source evidence,
  threats considered vs out of scope, assumptions, and compensating
  controls for the single-maintainer bus factor. Satisfies CII Silver
  `assurance_case` and OSPS Baseline `OSPS-SA-03.02`.
- **`MAINTAINERS.md`** cataloguing every load-bearing external
  service (repo, ghcr.io, Homebrew tap, AUR, MCP Registry, docs DNS,
  SSH signing key, Sigstore) with the specific configuration file a
  successor must edit, plus voluntary hand-off, community-fork, and
  emergency compromise procedures. Referenced from `GOVERNANCE.md`.

### Changed

- `GOVERNANCE.md` succession section now points at `MAINTAINERS.md`
  for the detailed catalogue and at `docs/security-model.md` for the
  assurance-case perspective, so hand-over context is not tribal.
- `.bestpractices.json` refined: `dco` and `assurance_case` (both
  Silver) flipped to Met with evidence links; `bus_factor`,
  `two_person_review`, `contributors_unassociated` remain honestly
  Unmet with strengthened compensating-controls justifications rather
  than misrepresenting the solo-maintainer reality.

### Dependencies

- Bumped 10 indirect dependencies to latest: `go-udiff`,
  `bits-and-blooms/bitset`, `charmbracelet/x/exp/golden`,
  `cpuguy83/go-md2man/v2`, `dlclark/regexp2`, `rogpeppe/go-internal`,
  `sahilm/fuzzy`, `golang.org/x/exp`, `golang.org/x/mod`,
  `golang.org/x/tools`, plus direct bumps of `google/jsonschema-go` and
  `spf13/cast`. Full test suite green (unit + `-race`).

## [0.0.13] — 2026-07-01

Preflight visibility + real-world sync robustness.

### Fixed

- **Empty upstream repositories no longer surface as sync errors.**
  When a GitHub repo is created but never pushed to, its local clone
  has an unborn HEAD and `git pull` bails with `no such ref was fetched`.
  Corral now detects that state locally via a cheap
  `git rev-parse --verify HEAD^{commit}` before calling pull and returns
  `SKIP: empty repository (no commits yet)` instead of `ERROR`.
  Verified against six real cases on `sebastienrousseau` — all now
  clean-skip instead of erroring at the tail of the run.

### Added

- **Preflight banner + confirmation** in front of every non-interactive
  run. Prints `Owner: <owner>` and `Target: <absolute base_dir>` so an
  arg typo like `corralctl i sebastienrousseau` (owner=`i`,
  base_dir=`sebastienrousseau`) is visible before any GitHub API call
  or clone. When the base directory does not yet exist and stdin is a
  TTY, additionally prompts `Continue? [y/N]` and aborts on anything
  other than `y` / `yes`. Skipped when `--yes` / `-y` is passed, when
  `--dry-run` is set (no side effects to warn about), and in
  `--interactive` mode (the TUI has its own /exit confirmation).
- **`internal/git.IsEmpty(targetDir)`** helper — a cheap local
  `git rev-parse --verify HEAD^{commit}` check that the engine now
  calls before every pull. Exposed for reuse.

### Stats

- 7 packages, `-race -count=1` green across Ubuntu + macOS + Windows.
- Doc coverage: 64/64 (100 %).
- Adds 5 new tests (2 engine, 3 git) + tightens the shared
  `withGitPullStub` helper so pre-v0.0.13 tests continue to exercise
  the pull path.

## [0.0.12] — 2026-07-01

Write tools + prompts + container security scanning.

### Added

- **Three MCP write tools**, gated behind `--enable-mutations`:
  - **`corral_sync_repo`** — runs `git pull --rebase --autostash` against
    one clone. Reuses the existing non-interactive git environment and
    smart-sync sidecar semantics.
  - **`corral_clone_repo`** — clones a URL into a caller-provided
    target path relative to the sandbox root. Supports optional
    `depth` / `blobless`. Refuses when the target already exists or
    escapes the sandbox.
  - **`corral_delete_repo`** — removes a clone. Additionally gated by
    `--enable-destructive-mutations`. Refuses when uncommitted
    changes exist, unpushed commits exist, or the target is not a git
    repository.
- **Mutation audit log** — every mutation attempt (successful or
  refused) is appended as a JSONL record to
  `$XDG_STATE_HOME/corral/mutations.log` (or
  `~/.local/state/corral/mutations.log` per XDG spec) capturing
  timestamp, tool, target, args, result, and any error message.
  Path is overridable with `--audit-log`.
- **Two MCP prompts**:
  - **`explain_workspace`** — pre-canned instructions asking the agent
    to survey the workspace via read-only tools and produce a
    human-readable summary.
  - **`identify_stale_repos`** — pre-canned instructions asking the
    agent to find clones whose `.corral-state.json` says they haven't
    been synced in more than `threshold_days` days (default 30).
- **Container security workflow** at `.github/workflows/container-scan.yml`:
  - **hadolint** static-lints the Dockerfile on every PR that touches
    it; results uploaded as SARIF to the Code Scanning surface.
  - **Trivy** CVE-scans the published multi-arch OCI image after each
    release; also uploaded as SARIF. Both jobs are advisory
    initially (findings do not block the merge/release).
- **OpenSSF Best Practices Passing badge earned** (project #13455). All 67
  Passing-tier criteria answered via `.bestpractices.json` and accepted
  by the badge app. Badge is displayed in the README badge row.

### Changed

- **MCP server** advertises `prompts` capability. `server.json`
  updated to reflect the new capability + tool inventory.

### Stats

- 7 packages, `-race -count=1` green.
- Doc coverage: 63/63 (100 %).
- `internal/mcp` gains 15 new tests covering mutation happy paths,
  refusal cascades, and the audit log.

## [0.0.11] — 2026-07-01

Supply-chain hardening + coverage lifts.

### Added

- **SLSA v1.0 build provenance for every release artifact.**
  `actions/attest-build-provenance` runs after goreleaser and attests
  the contents of `dist/checksums.txt` — so every `.tar.gz`, `.deb`,
  `.rpm`, and the checksums file itself carries a cryptographic
  attestation that binds it to this exact commit and workflow run.
  Users verify with `gh attestation verify <file> --owner sebastienrousseau`.
- **Keyless cosign signing of Docker images.** `docker_signs:` in
  `.goreleaser.yaml` signs both the per-arch images and the
  `docker_manifests:` fan-out to `:{version}` + `:latest`. Uses the
  Actions OIDC token — no long-lived signing key. Users verify with
  `cosign verify` against the workflow identity documented in the
  goreleaser config.

### Changed

- **OpenSSF Scorecard Signed-Releases check.** Both mechanisms above
  feed the check; expect a jump from 0/10 → 10/10 on the next scan.
- **Test coverage** for the two remaining gaps flagged in the v0.0.10
  post-release audit:
  - `cmd/mcp.go` `runMCP`: 0 % → 90 % (all validation, wiring, and
    error-propagation paths).
  - `internal/github` `matchesFilters`: 55.9 % → 100 % (17 subtests
    covering every `opts.Type` branch — sources / forks / archived /
    mirrors / templates / sponsored / public / private / unknown).
  - Project total: 88.9 % → 90.2 %.

## [0.0.10] — 2026-07-01

MCP hardening pass — four v0 quality issues surfaced by post-release
review, all closed in one PR.

### Fixed

- **Nested-namespace URI resolution.** `corral://repo/{owner}/{name}/…`
  now resolves when `{owner}` matches **any** namespace segment in the
  origin URL, not only the direct parent. Self-hosted GitLab / Gitea
  layouts like `https://git.example.com/parent/subgroup/team/repo.git`
  are queryable via `parent`, `subgroup`, *or* `team` as the owner
  argument. `parseOwnerFromURL` return type changed from `string` to
  `[]string` accordingly (internal, non-breaking to MCP clients).
- **Silent git diagnostics.** `currentBranch` and `readState` used to
  swallow errors, hiding detached-HEAD, corrupted-refs, and
  permission-denied cases behind an empty-string result. Both now log
  to `stderr` (never `stdout` — that's the JSON-RPC protocol stream)
  with the repo path and underlying error, while preserving the same
  return contract so tool results stay backward compatible.
- **Docker permission failures under strict host mounts.** The README
  Docker snippet now includes `--user 1000:1000` (documented as "replace
  with `$(id -u):$(id -g)`") plus notes on the read-only `:ro` mount
  and the `--root /workspace` sandbox. Without this, the containerised
  scanner ran as a system UID and hit `permission denied` on any
  workspace directory made group- or user-private on the host.

### Changed

- **In-memory scan cache with a 5-second TTL.** `Server.scan()` now
  amortises filesystem walks across a burst of tool/resource calls in
  a single client session — critical for workspaces with hundreds of
  clones where an agent typically fires 5–10 tool calls in quick
  succession. Cache is invalidated after `scanTTL` (5s) so a
  just-cloned repo appears on the next call the agent makes.
  `invalidateScanCache()` gives tests deterministic control without
  needing time.Sleep.

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

[Unreleased]: https://github.com/sebastienrousseau/corral/compare/v0.0.16...HEAD
[0.0.16]: https://github.com/sebastienrousseau/corral/compare/v0.0.15...v0.0.16
[0.0.15]: https://github.com/sebastienrousseau/corral/compare/v0.0.14...v0.0.15
[0.0.14]: https://github.com/sebastienrousseau/corral/compare/v0.0.13...v0.0.14
[0.0.13]: https://github.com/sebastienrousseau/corral/compare/v0.0.12...v0.0.13
[0.0.12]: https://github.com/sebastienrousseau/corral/compare/v0.0.11...v0.0.12
[0.0.11]: https://github.com/sebastienrousseau/corral/compare/v0.0.10...v0.0.11
[0.0.10]: https://github.com/sebastienrousseau/corral/compare/v0.0.9...v0.0.10
[0.0.9]: https://github.com/sebastienrousseau/corral/compare/v0.0.8...v0.0.9
[0.0.8]: https://github.com/sebastienrousseau/corral/compare/v0.0.7...v0.0.8
[0.0.7]: https://github.com/sebastienrousseau/corral/compare/v0.0.6...v0.0.7
