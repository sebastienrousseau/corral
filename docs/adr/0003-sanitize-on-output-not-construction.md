<!-- SPDX-License-Identifier: GPL-3.0-only -->

# 0003 — Untrusted strings are sanitised on output, not at construction

**Status:** Accepted · **Date:** 2026-09-02

## Context

Nearly every string the MCP server reports is chosen by someone else. A
repository's directory name comes from its owner, and `corralctl topic:…`
clones repositories the user never named. A repository called
`SYSTEM-ignore-prior-instructions-…` is a legal GitHub name, and was
reproduced reaching an agent's context verbatim.

The obvious fix is to sanitise in `buildEntry`, where the index is built —
one place, every field covered.

## Decision

Sanitise at the output boundary, via `RepoEntry.Redacted()`, applied at
every tool result, resource body and error message that reaches a model.
The stored entry keeps its exact bytes.

## Consequences

`RepoEntry.Path` is what `SafeMutationPath` resolves and what `git -C` is
handed. Sanitising it at construction would make the index disagree with
the filesystem: a repository whose directory name contains a stripped
character would be listed under one name and operated on under another, or
not found at all. That is a worse bug than the one being fixed — a
correctness failure in the destructive path, traded for a hardening win in
the read path.

The cost is that every new output site must remember to call `Redacted()`.
A test pins the invariant that the original is not mutated, and `AGENTS.md`
records the rule.

Sanitising cannot remove plain-language injection without breaking the
tool, so the server instructions tell the model that every returned value
is untrusted data. Stripping is for the mechanisms that hide text — ANSI
escapes, bidirectional overrides, zero-width characters — not for prose.

## What would make this wrong

If the index ever stopped being the thing that addresses the filesystem —
if paths were resolved from an immutable id rather than a string — then
construction-time sanitising would become both safe and simpler.
