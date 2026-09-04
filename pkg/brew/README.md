<!--
SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
SPDX-License-Identifier: GPL-3.0-only
-->

# Homebrew

**Recipe:** the `homebrew_casks` section of
[`.goreleaser.yaml`](../../.goreleaser.yaml), which generates the cask
and commits it to `sebastienrousseau/homebrew-tap`. Not duplicated here;
see [`../README.md`](../README.md) for why.

It commits directly rather than opening a pull request. goreleaser has no
auto-merge, so a proposed change waits for a human — and seven of them
waited while the tap served v0.0.25 through v0.0.32.

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
