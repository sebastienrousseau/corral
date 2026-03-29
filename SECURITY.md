# Security Policy

## Reporting a Vulnerability

Report security issues by emailing the maintainer directly. Do not open a public issue.

You should receive a response within 48 hours. If confirmed, a fix will be released as soon as possible.

## Supported Versions

Only the latest release on the `main` branch is supported.

## Security Measures

- All commits are cryptographically signed (ED25519).
- CI actions pinned to immutable SHAs, not mutable tags.
- Gitleaks scans run on every push and pull request.
- No third-party code is vendored or embedded.
- Full software bill of materials available in [SBOM.md](SBOM.md).
