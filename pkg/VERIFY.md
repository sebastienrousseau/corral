<!--
SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
SPDX-License-Identifier: GPL-3.0-only
-->

# Verifying a release artefact

Every release carries three independent things a packager can check. Do
all three: a checksum alone proves only that the file matches a list
that came from the same place the file did.

Set the version once:

```bash
VERSION=0.0.29
```

## 1. Checksums

```bash
curl -fsSLO "https://github.com/sebastienrousseau/corral/releases/download/v${VERSION}/checksums.txt"
sha256sum --check --ignore-missing checksums.txt
```

## 2. Signature (keyless cosign)

The checksum file is signed with a short-lived certificate bound to the
release workflow's identity, and the signature is logged in Rekor. There
is no long-lived key to steal, and the identity is what to check —
`--certificate-identity-regexp` below pins the signature to this
repository's release workflow, not merely to "some GitHub Actions run".

```bash
cosign verify-blob \
  --certificate       checksums.txt.pem \
  --signature         checksums.txt.sig \
  --certificate-identity-regexp '^https://github\.com/sebastienrousseau/corral/\.github/workflows/release\.yml@refs/tags/v' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  checksums.txt
```

## 3. Provenance (SLSA)

```bash
gh attestation verify "corralctl_${VERSION}_linux_amd64.tar.gz" \
  --repo sebastienrousseau/corral
```

This states which workflow, at which commit, built the artefact — the
claim a checksum cannot make.

## 4. SBOM

A CycloneDX SBOM is attached to each release, and `SBOM.md` in the
repository is checked against `go.mod` by CI on every change. If your
distribution records dependencies, take them from the release SBOM
rather than transcribing them.

## If verification fails

Do not package the artefact, and please open a security advisory rather
than an issue: <https://github.com/sebastienrousseau/corral/security/advisories/new>.
