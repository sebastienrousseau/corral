<!--
SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
SPDX-License-Identifier: GPL-3.0-only
-->

# RPM package

**Recipe:** the `nfpms` section of [`.goreleaser.yaml`](../../.goreleaser.yaml).
Not duplicated here; see [`../README.md`](../README.md) for why.

Contents, dependencies and provenance are identical to the
[Debian package](../deb/README.md) — one nfpm configuration produces
both.

## Install

```bash
sudo rpm -i corralctl_<version>_linux_amd64.rpm
```

Verify it first — see [`../VERIFY.md`](../VERIFY.md).

## Packaging for Fedora

Build from source. `make install` honours `PREFIX` and `DESTDIR`, and
`docs/packaging.md` covers the offline build with vendored dependencies
that a Fedora build root requires.
