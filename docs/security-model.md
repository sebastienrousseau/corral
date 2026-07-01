# Corral — Security Model & Assurance Case

**Status:** Living document. Last full review: 2026-07-01.
**Owner:** Sebastien Rousseau ([@sebastienrousseau](https://github.com/sebastienrousseau)).
**Scope:** the `corralctl` binary, the MCP server it ships, the release
pipeline that produces its artefacts, and the on-disk state it manages.

This document is Corral's **assurance case**: a structured argument that the
project is secure to a stated level, with the evidence that backs each
claim. It is deliberately narrower and more explicit than a marketing-style
"security policy". Reviewers, packagers, and downstream users should be
able to read this document and understand *what Corral protects*, *what it
does not protect*, *what could go wrong*, and *what compensating controls
exist*.

It maps onto the CII Best Practices Silver-tier `assurance_case` criterion
and the OSPS Baseline `SEC-*` controls; individual claims are tagged with
the specific criterion or control they satisfy.

---

## 1. What Corral is

Corral (`corralctl`) is a Go CLI that clones and organises a user's own
GitHub repositories into a local directory tree grouped by visibility and
language. It also ships an MCP server that lets an LLM query the local
mirror without network access.

It is:

- A **read-mostly** tool from GitHub's perspective (clones, fetches).
- A **write** tool from the local-filesystem perspective (creates
  directories, `git clone`, `git pull`).
- **Never** a tool that pushes, force-updates, deletes remote content, or
  modifies GitHub state via the API.

## 2. Trust boundaries

Corral operates across four trust boundaries:

| # | Boundary                              | Direction     | What crosses it                                  |
|---|---------------------------------------|---------------|--------------------------------------------------|
| 1 | User's shell → `corralctl` process    | in            | CLI flags, environment (`GITHUB_TOKEN`), stdin   |
| 2 | `corralctl` → GitHub API              | out           | REST requests with the user's PAT                |
| 3 | `corralctl` → local filesystem        | out           | `git clone`/`git pull` writes under target dir   |
| 4 | MCP client (LLM host) → MCP server    | in            | JSON-RPC tool calls; no filesystem writes        |

Boundary 2 also includes `git clone` traffic to `github.com` over HTTPS.

## 3. Security properties (claims)

We claim the following properties. Each is followed by the evidence.

### C1. Corral cannot exfiltrate credentials to a third party

**Argument.** The only credential Corral reads is `GITHUB_TOKEN` (or the
value passed via `--token`). It is used exclusively as an
`Authorization: token …` header on requests to `https://api.github.com`
and as the `git`-embedded credential for `https://github.com` clones. No
other outbound host is contacted.

**Evidence.**
- `git grep -n 'http'` in `internal/github/` shows every request goes to
  `api.github.com`.
- No telemetry / analytics dependency. `go list -deps ./...` includes no
  analytics SDK.
- `--dry-run` prints the exact operations without executing them, so an
  auditor can inspect intended I/O.

### C2. Corral cannot write outside the user-specified target directory

**Argument.** The single target directory is derived from a positional
argument or CLI flag, resolved once via `filepath.Abs`, and used as the
root for every subsequent `git clone`/`git pull`. The engine does not
consume any path from GitHub API responses; only the API-provided
repository *name* is joined onto the resolved root, and repository names
containing `/`, `..`, or path separators are rejected upstream by GitHub.

**Evidence.**
- Preflight (`cmd/preflight.go`) prints the absolute target path before
  any network call and requires a TTY confirmation when the directory
  doesn't exist and `--yes`/`--dry-run` isn't set.
- Engine tests assert no writes occur when `--dry-run` is passed.
- `internal/git/git.go` invokes `git` with `-C <targetDir>` and never
  interpolates untrusted input into shell.

### C3. The release artefacts you download are the artefacts we built

**Argument.** Every release is:

1. Built by a GitHub-hosted runner from a signed tag on `main`.
2. Signed keylessly with cosign (Sigstore/Fulcio/Rekor).
3. Accompanied by an SLSA v1.0 provenance attestation produced by
   `actions/attest-build-provenance`.
4. Published together to the same GitHub Release under the tag.

Users can verify with:

```bash
gh attestation verify corralctl_*_linux_amd64.tar.gz \
  --owner sebastienrousseau
cosign verify-blob corralctl_*_linux_amd64.tar.gz \
  --signature corralctl_*_linux_amd64.tar.gz.sig \
  --certificate corralctl_*_linux_amd64.tar.gz.pem \
  --certificate-identity-regexp 'https://github.com/sebastienrousseau/corral' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

**Evidence.**
- `.github/workflows/release.yml` shows the full pipeline; every action
  is SHA-pinned per OpenSSF Scorecard `Pinned-Dependencies`.
- v0.0.13 release page shows the `.pem`/`.sig` pair per artefact and the
  Sigstore bundle inline in the attestation.

### C4. The MCP server does not modify the local mirror

**Argument.** The MCP server (`cmd/mcp/…`) registers only read-shaped
tools: `list_repos`, `find_repo`, `stats`, etc. There is no tool that
invokes `git`, writes files, or shells out. It reads the JSON manifest
Corral produced during the last `sync`.

**Evidence.**
- `git grep -n 'exec.Command\|os.WriteFile\|os.Create' cmd/mcp internal/mcp`
  returns no hits.

### C5. Corral fails closed on empty or hostile upstream state

**Argument.** For repositories with no commits, Corral detects the
condition with `git rev-parse --verify -q HEAD^{commit}` (`internal/git`)
and marks the repo `SKIP` instead of running `git pull` (which would
error with "couldn't find remote ref HEAD"). No further work is
attempted on that repo.

**Evidence.**
- `internal/engine/engine_empty_test.go` asserts SKIP + no pull.

## 4. Threats considered and out of scope

### In scope

- **Malicious CLI arg**: mitigated by preflight banner + confirm (C2).
- **Token leakage in logs**: PAT is never logged; only length is printed.
- **Supply chain against release**: mitigated by SHA-pinned actions +
  cosign + SLSA (C3).
- **Dependency compromise**: mitigated by Dependabot on `go.mod` and by
  `govulncheck` in CI; `go.sum` locks transitive hashes.

### Out of scope

- **Compromise of the maintainer's laptop or GitHub account.** The
  maintainer's account is the root of trust; if it is compromised, a
  malicious release could be signed and shipped. Sigstore's Rekor
  transparency log makes such a release publicly auditable after the
  fact but does not prevent it. Users concerned about this scenario
  should pin to a specific release tag+digest.
- **Attacks against GitHub itself.** Corral trusts `api.github.com` and
  `github.com` as authoritative for repository state.
- **Local privilege escalation via git hooks.** `corralctl` runs `git
  clone`, and `git` executes hooks from the cloned repository during
  some operations. Users cloning arbitrary attacker-controlled repos
  should be aware of `core.hooksPath` behaviour.
- **Denial of service via GitHub rate limits.** Corral respects
  `X-RateLimit-*` headers and backs off, but cannot prevent a mistuned
  cron job from exhausting the user's own rate budget.

## 5. Assumptions

- The user's `GITHUB_TOKEN` has only the scopes Corral needs
  (`repo`, `read:user`). Corral does not need `write:*` scopes.
- `git` is installed and version ≥ 2.20 (for `-C`, `-c`, and modern
  transports).
- The user's clock is roughly correct (needed for TLS validation and
  Rekor entry timestamps).
- The user runs a supported OS: recent Linux, macOS ≥ 14, or Windows 11.
- The filesystem under the target directory is not simultaneously
  written to by another Corral process.

## 6. Compensating controls (bus factor + solo maintainer)

Corral has a single maintainer. This is a real risk to sustained
security response. Mitigations:

- **Public assurance case (this doc)**: a successor maintainer or
  reviewing packager can pick up where the current maintainer left off
  without back-channel context.
- **Documented signing keys**: `GOVERNANCE.md` records the SSH signing
  key fingerprint; a successor can publish a new key and users can
  reason about the transition.
- **Documented external services**: `MAINTAINERS.md` catalogues every
  external account (Homebrew tap, AUR, ghcr.io, DNS) so continuity is
  auditable rather than tribal.
- **Fork-and-continue is explicit**: GPL-3.0 licensing + the six-month
  unresponsive-maintainer clause in `GOVERNANCE.md` normalise the
  community-fork path.

## 7. Review and update

This document is re-reviewed on every release that touches:

- Any file in `internal/git/`, `internal/github/`, `internal/engine/`.
- Any file in `.github/workflows/`.
- Any `.bestpractices.json` entry.
- Any new outbound-network dependency.

Otherwise it is re-reviewed annually.

If you have questions or believe a claim above is not adequately
supported by the linked evidence, please file a security advisory per
[SECURITY.md](../SECURITY.md).
