<!--
SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
SPDX-License-Identifier: GPL-3.0-only
-->

# Arch User Repository

**Recipe:** [`scripts/publish_aur.sh`](../../scripts/publish_aur.sh), run by
the `aur` job in the release workflow. The checksums it writes are read
from the signed release rather than recomputed, so the `PKGBUILD` cannot
disagree with what was published.

Publishing is a separate job on purpose: `aur.archlinux.org` being
unreachable must not cost a release its artefacts, signatures or
attestations. It is also `continue-on-error`, so a failure is visible
without failing the release.

Before v0.0.33 this was left to manual publishing, and manual publishing
happened once — the package sat at 0.0.13 while the project shipped
0.0.32.

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
