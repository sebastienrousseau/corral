---
author: "Sebastien Rousseau"
date: "2026-08-30"
language: "en-GB"
schema: "page"
changefreq: "weekly"
copyright_year: "2026"
locale_path: "/"
base_path: "/"
name: "Corral"
short_name: "CO"
slug_install: "installation"
slug_usage: "usage"
slug_mcp: "mcp"
slug_ref: "reference"
nav_home: "Home"
nav_install: "Installation"
nav_usage: "Usage"
nav_mcp: "MCP Server"
nav_ref: "Reference"
label_skip: "Skip to main content"
label_menu: "Menu"
label_nav: "Main"
label_theme: "Theme"
label_theme_system: "System"
label_docs: "Documentation"
label_footer_nav: "Documentation"
label_docs_nav: "Documentation sections"
label_crumbs: "Breadcrumb"
label_pager: "Page"
label_prev: "Previous"
label_next: "Next"
label_toc: "On this page"
screenshot_alt: "Corral organising GitHub repositories into a Finder-friendly directory hierarchy."
footer_note: "Corral clones and organises GitHub repositories into a Finder-friendly hierarchy. Published under GPL-3.0-only."
copyright: "© 2026 Sebastien Rousseau. Licensed under GPL-3.0-only."
translation_key: "mcp"
title: "MCP Server — Corral"
description: "Corral's Model Context Protocol server exposes your local, organised workspace to AI agents. Read-only by default, and it never contacts the GitHub API."
keywords: "mcp server, model context protocol, claude code, cursor, ai agent tools"
eyebrow: "Integration"
headline: "MCP Server"
lead: "A window onto the clones already on disk. In its default mode it makes no network calls and never contacts the GitHub API."
cur_install: ""
cur_usage: ""
cur_mcp: ' aria-current="page"'
cur_ref: ""
toc_1: "What it is"
toc_1_id: "what-it-is"
toc_2: "Tools"
toc_2_id: "tools"
toc_3: "Mutations and audit"
toc_3_id: "mutations-and-audit"
prev_href: "/usage/"
prev_label: "Usage"
next_href: "/reference/"
next_label: "Reference"
layout: "doc"
---

## What it is

Corral ships a Model Context Protocol server that exposes your local,
Corral-organised workspace to AI coding agents — Claude Code, Cursor, Cline,
Codex CLI, Aider, and anything else that speaks MCP.

Where GitHub's own MCP server covers the remote API surface — issues, pull
requests, search — `corral-mcp` covers the dimension only it can: your *local
mirror*, organised by visibility and language, queryable without a round trip.

**In its default read-only mode no network calls are made and the GitHub API is
never contacted.** The server reads the clones already on disk. That is the
whole security posture, and it is worth stating plainly because it is the
question an agent operator should ask first.

## Tools

Read-only, available by default:

| Tool | What it answers |
| --- | --- |
| `corral_list_repos` | Filter local clones by visibility, language, name or sync state |
| `corral_find_repo` | Resolve a fuzzy name to one clone, returning candidates when ambiguous |
| `corral_get_repo_metadata` | Full metadata for one clone, including its current branch |
| `corral_status_summary` | Workspace summary: counts by visibility and language |
| `corral_workspace_index` | The full structured index, in a single call |

## Mutations and audit

Write tools are opt-in and off unless you ask for them.

Passing `--enable-mutations` adds two tools that do reach the network, because
they shell out to `git`. The GitHub API is still never contacted.

| Tool | Behaviour | Requires |
| --- | --- | --- |
| `corral_sync_repo` | Runs `git pull --rebase --autostash` against one clone | `--enable-mutations` |
| `corral_clone_repo` | Clones a URL into a sandboxed target path | `--enable-mutations` |
| `corral_delete_repo` | Removes a clone; refuses on uncommitted or unpushed changes | `--enable-destructive-mutations` |

Deletion sits behind a second, separate flag on purpose, and it refuses
outright when a clone holds uncommitted or unpushed work rather than asking an
agent to decide.

Every mutation writes a JSONL audit record to
`$XDG_STATE_HOME/corral/mutations.log`, falling back to
`~/.local/state/corral/mutations.log`. What an agent did to your workspace is
therefore reconstructable after the fact, which is the point.
