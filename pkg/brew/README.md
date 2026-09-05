<!--
SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
SPDX-License-Identifier: GPL-3.0-only
-->

# Homebrew

**Recipe:** the `homebrew_casks` section of
[`.goreleaser.yaml`](../../.goreleaser.yaml), which generates the cask
from the release's own checksums. Not duplicated here; see
[`../README.md`](../README.md) for why.

Publishing happens in the `brew` job of
[`release.yml`](../../.github/workflows/release.yml), not in goreleaser.
The job opens a pull request on `sebastienrousseau/homebrew-tap` and
merges it immediately — the tap protects `main` with `enforce_admins`, so
a pull request is the only way in for anyone, but it requires no review
and no status checks, so nothing waits for a human.

That arrangement was arrived at twice. It first opened a pull request and
left it there: seven accumulated while the tap served v0.0.25 through
v0.0.32. v0.0.33 then made goreleaser commit straight to `main`, which the
branch protection refuses — and because that ran inside the release job,
the `409` failed the release after its artefacts were published but
before their SLSA provenance. Hence a separate job, which cannot fail the
release, exactly as the AUR publish already worked.

A cask rather than a formula: goreleaser deprecated `brews:` in favour
of casks for pre-built binaries, and corralctl ships as one.

## Install

```bash
brew tap sebastienrousseau/tap
brew install --cask corralctl
```

macOS quarantines downloaded binaries. The cask removes the quarantine
attribute on install, which is why `corralctl` runs without a Gatekeeper
prompt; if you install the tarball by hand instead, you will need
`xattr -d com.apple.quarantine` yourself.

Verify the artefact first — see [`../VERIFY.md`](../VERIFY.md).
