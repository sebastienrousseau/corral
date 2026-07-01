# CII Best Practices — Answer Sheet (Passing tier)

This document is a drafting aid for submitting Corral to
[bestpractices.dev](https://www.bestpractices.dev). It maps each of the ~65
questions on the **Passing** badge form to a concrete answer sourced from
current repo state, with URLs the reviewer can click.

**How to use:**

1. Sign in at <https://www.bestpractices.dev> with your GitHub account.
2. Register the project (repo URL: <https://github.com/sebastienrousseau/corral>).
3. Walk the sections below, copying URLs / one-line answers into the form.

The form itself does not have an API, so this can't be automated end-to-end.
Expected time: **30–45 minutes**.

Once submitted and marked "Passing," the badge URL (visible after the form is
saved) can be embedded in `README.md`'s badge row.

---

## Section 1 — Basics

| Question | Answer |
|---|---|
| **Project URL** | <https://github.com/sebastienrousseau/corral> |
| **Homepage** | <https://doc.corrallib.com> |
| **Description** | "Automatically clone and organise GitHub repositories by visibility and language. Ships an MCP server so AI coding agents can query the local mirror." |
| **License(s)** | GPL-3.0 (see [LICENSE](../LICENSE)) |
| **Interact discussion mechanism** | GitHub Issues: <https://github.com/sebastienrousseau/corral/issues> |
| **English** | Yes — README + all docs are in English |

## Section 2 — Change Control

| Question | Answer |
|---|---|
| **Public VCS** | Yes — Git on GitHub (public repo) |
| **Interim versions accessible** | Yes — every commit on `main` is a distinct SHA; PR merge history is visible |
| **Uses semantic versioning** | Yes — tags `v0.0.7`, `v0.0.8`, `v0.0.9`, `v0.0.10`, `v0.0.11` follow semver 2.0 |
| **Release notes** | Yes — [CHANGELOG.md](../CHANGELOG.md) with a per-version section documenting Added / Changed / Fixed / Security |
| **CHANGELOG.md documented** | Yes — links to <https://keepachangelog.com/en/1.1.0/> |

## Section 3 — Reporting

| Question | Answer |
|---|---|
| **Report tracker** | GitHub Issues |
| **Response commitment** | See [SECURITY.md](../SECURITY.md) — private disclosure via `sebastian.rousseau@gmail.com` for security issues; public issues for non-security bugs |
| **Vulnerability report process** | [SECURITY.md](../SECURITY.md) documents supported versions and reporting channel |
| **Vulnerability report acknowledgement** | Documented in SECURITY.md |
| **Vulnerability disclosure timeline** | 90-day coordinated disclosure per SECURITY.md |

## Section 4 — Quality

| Question | Answer |
|---|---|
| **Working build system** | Yes — `Makefile` targets: `build`, `test`, `test-race`, `vet`, `lint`, `format`, `clean`. `.goreleaser.yaml` handles the release build. |
| **Automated test suite** | Yes — `make test` runs `go test ./...`. CI runs it on Ubuntu, macOS, Windows via [ci.yml](../.github/workflows/ci.yml) |
| **New functionality added tests** | Yes — every PR since v0.0.7 has included tests; project total coverage is 90.2 % of statements |
| **Warning flags on** | Yes — CI runs `go vet ./...` (equivalent to `-Wall` for Go). No warnings in the current build. |
| **Warnings-as-errors** | Effectively yes — `golangci-lint run ./...` per Makefile lint target |

## Section 5 — Security

| Question | Answer / Evidence |
|---|---|
| **Developer knows security basics** | Yes — signed commits, dependency review, CodeQL, gosec, OpenSSF Scorecard all wired |
| **Uses cryptography correctly** | The project itself doesn't implement crypto; it delegates to `git` (SSH/HTTPS auth) and Go stdlib. No home-rolled cryptography. |
| **Publicly-facing cryptography** | N/A — no public network service |
| **Cryptographic keys** | GitHub-issued ephemeral OIDC token for signing (no long-lived keys). See `.github/workflows/release.yml`. |
| **Uses TLS** | Yes — all github.com API calls are HTTPS; git operations default to HTTPS/SSH |
| **Known unpatched vulns** | None. Zero open code-scanning alerts as of v0.0.11: <https://github.com/sebastienrousseau/corral/security/code-scanning> |
| **Uses safe defaults** | Yes — read-only MCP tools by default (`--enable-mutations` required to unlock any write); path-traversal defence with root canonicalisation; non-interactive git env everywhere |

## Section 6 — Analysis

| Question | Answer |
|---|---|
| **Static code analysis** | Yes — CodeQL runs on every PR (see `.github/workflows/security.yml`). Also gosec via `Security / Go Security Audit` job. |
| **Dynamic code analysis** | Partial — `go test -race` runs on every PR; no fuzz suite yet (candidate for follow-up) |
| **Static analysis of dependencies** | Yes — Dependency Review action + OpenSSF Scorecard |
| **Security-focused build** | Yes — release build strips debug info (`-s -w`) and pins base image (`FROM alpine:3.20@sha256:d9e853...`) + all Actions to commit SHAs |

## Section 7 — Documentation

| Question | Answer |
|---|---|
| **Documentation basics** | Yes — [README.md](../README.md) with Quick Start, Architecture diagram, Features, Interactive TUI, Layout Customization, Smart Syncing, Exec Mode, MCP Server, Usage & Flags, Examples, Troubleshooting, FAQ |
| **Documentation architecture** | Yes — mermaid diagram in README's Architecture section |
| **Documentation security** | Yes — [SECURITY.md](../SECURITY.md) + a "Safety" subsection in the MCP Server section of README |
| **Documentation quick-start** | Yes — README "Quick Start" section |
| **Documentation reference** | Yes — API docs auto-generated to <https://doc.corrallib.com> from `scripts/generate_docs.go`. Doc coverage gated at 100 %. |

## Section 8 — Contribution

| Question | Answer |
|---|---|
| **Contribution processes documented** | Yes — [CONTRIBUTING.md](../CONTRIBUTING.md) |
| **Coding standards documented** | Yes — CONTRIBUTING.md links to Go standards; Makefile has `format` (gofmt) and `lint` (golangci-lint) |
| **Automated tests on contributions** | Yes — CI blocks merge until Go CI + Docs + Security + Scorecard checks pass |
| **Signed contributions** | Yes — Branch ruleset requires signed commits (see repo Settings → Rules) |

## Section 9 — Release / Distribution

| Question | Answer |
|---|---|
| **Release build reproducible** | Yes — Docker base image and `docker/dockerfile:1.7` frontend are both SHA-pinned; goreleaser produces the same binaries from the same source tree |
| **Release notes** | Yes — CHANGELOG.md per-version + GitHub release pages |
| **Cryptographic signatures on releases** | **Yes — as of v0.0.11**: SLSA v1.0 provenance on every tarball / .deb / .rpm (via `actions/attest-build-provenance`, verifiable with `gh attestation verify`), cosign keyless signatures on the Docker image (verifiable with `cosign verify` — command in .goreleaser.yaml). |

---

## Section-specific gotchas

- The form has a **"non-security bugs"** section that asks about response time. Since Corral is a single-maintainer project, be honest: "best-effort within 7 days".
- **"Public discussions of major changes"** — the answer is *the PR itself is public*; the CHANGELOG documents rationale. If the reviewer pushes back, cite specific PRs (e.g. <https://github.com/sebastienrousseau/corral/pull/35> for v0.0.11).
- **"Test coverage"** metric — answer with 90.2 % (link to a coverage badge or the doc-coverage line from CI).

## After submission

Add the earned badge to `README.md`:

```markdown
<a href="https://www.bestpractices.dev/projects/<YOUR-PROJECT-ID>"><img src="https://www.bestpractices.dev/projects/<YOUR-PROJECT-ID>/badge" alt="OpenSSF Best Practices"></a>
```

Expected OpenSSF Scorecard bump: `CII-Best-Practices 0/10` → **8–10/10** on the next scan (some points are only reachable at the Silver/Gold tiers, which require additional work).

---

*Generated for Corral v0.0.11. Refresh if the repo changes materially before submission.*
