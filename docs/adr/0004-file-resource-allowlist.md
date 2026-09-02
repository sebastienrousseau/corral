<!-- SPDX-License-Identifier: GPL-3.0-only -->

# 0004 — The MCP file resource serves by allowlist

**Status:** Accepted · **Date:** 2026-09-02

## Context

`corral://repo/{owner}/{name}/file/{+path}` lets an agent read a file from
a clone. The original content policy was a denylist of credential
filenames: `.env`, `.netrc`, `id_rsa`, `.git/**` and so on.

An audit drove the compiled binary over real MCP stdio and read back, in
full, eight files the denylist had not anticipated: `.kube/config`,
`kubeconfig`, `credentials.json`, `.pgpass`, `terraform.tfvars`,
`.htpasswd`, `.yarnrc.yml` and `deploy.ppk`.

The near-misses show why a denylist could not have worked: `credentials`
was refused but `credentials.json` — the actual service-account filename —
was not; `terraform.tfstate` was refused but `.tfvars`, where the secrets
are typed, was not; `.npmrc` was refused but `.yarnrc.yml` and its
`npmAuthToken` was not.

## Decision

Invert the policy. Serve recognised source, documentation and non-secret
configuration by extension, plus the conventional extensionless project
files. Keep the denylist *behind* the allowlist. Provide
`--allow-file-ext` to widen the allowlist, which cannot re-enable a
denylisted file.

## Consequences

A denylist puts the burden on the list author to have thought of every
credential filename in advance, on a surface a prompt-injected agent can
reach. An allowlist fails closed: an unrecognised extension is refused, and
the cost is an error message naming the flag that permits it.

The denylist is still required, because some credential stores wear an
extension the allowlist serves — `credentials.json` is `.json`,
`.yarnrc.yml` is `.yml`. Extension alone cannot decide them.

The cost is that a workspace with house extensions needs
`--allow-file-ext`. That is a one-line flag, and the refusal says so.

## What would make this wrong

If the allowlist proved so tight that users routinely ran with a broad
`--allow-file-ext`, the policy would be theatre. Watch for that; the answer
would be a better default list, not a return to the denylist.
