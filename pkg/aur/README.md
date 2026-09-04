<!--
SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
SPDX-License-Identifier: GPL-3.0-only
-->

# Arch User Repository

**Recipe:** the `aurs` section of [`.goreleaser.yaml`](../../.goreleaser.yaml),
which generates and pushes the `PKGBUILD`. Not duplicated here; see
[`../README.md`](../README.md) for why.

## Package

`corralctl-bin` — the released binary, not a source build. The `-bin`
suffix is the Arch convention and it is accurate: the package installs
the artefact this project's release workflow built and signed, rather
than compiling from source on the user's machine.

```bash
yay -S corralctl-bin
```

## Install

```bash
git clone https://aur.archlinux.org/corralctl-bin.git
cd corralctl-bin
makepkg -si
```

`makepkg` checks the `sha256sums` in the `PKGBUILD`. For the signature
and provenance, see [`../VERIFY.md`](../VERIFY.md).
