<!--
SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
SPDX-License-Identifier: GPL-3.0-only
-->

# Homebrew

**Recipe:** the `homebrew_casks` section of
[`.goreleaser.yaml`](../../.goreleaser.yaml), which generates the cask
and pushes it to `sebastienrousseau/homebrew-tap`. Not duplicated here;
see [`../README.md`](../README.md) for why.

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
