<!-- SPDX-License-Identifier: GPL-3.0-only -->

# 0002 — `internal/github` imports nothing internal

**Status:** Accepted · **Date:** 2026-08-18

## Context

`internal/github` needs an edit-distance function to suggest a correction
when an owner lookup 404s on a near-miss of the authenticated user's login.
`cmd` already has one for subcommand suggestions.

The obvious move is to extract `levenshtein` into a shared package.

## Decision

Duplicate the twenty lines. `internal/github` imports only the standard
library and `go-github`.

## Consequences

Keeping the package a leaf means it can be read, tested and reasoned about
without loading the rest of the module, and it can never participate in an
import cycle as the dependency graph grows. `internal/engine` already
imports both `internal/git` and `internal/github`; a shared utility package
imported by all three is the shape that later becomes a cycle.

The cost is two copies of a well-understood, never-changing algorithm. If
the two ever disagree, nothing breaks — they serve different suggestions in
different commands.

## What would make this wrong

A third or fourth copy. Two is duplication; four is a missing package.
