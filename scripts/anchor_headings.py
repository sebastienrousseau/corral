#!/usr/bin/env python3
"""Give rendered headings stable ids, and verify every in-page link resolves.

ssg renders Markdown with pulldown-cmark configured for HTML output only.
pulldown-cmark does not generate heading ids, and the `{#id}` attribute
syntax is not enabled either — it renders as literal text. So a page whose
front matter declares an "On this page" list produces a table of contents
whose every entry points at an id that was never emitted.

Nothing catches that on its own: a link checker treats `/page/#thing` as a
request for `/page/`, which is a 200, and axe has no rule for it. Hence the
second half of this script, which resolves fragments itself and fails the
build when one is dangling.
"""
from __future__ import annotations

import pathlib
import re
import sys
import unicodedata

HEADING = re.compile(r"<(h[2-4])([^>]*)>(.*?)</\1>", re.S)
TAG = re.compile(r"<[^>]+>")
ID_ATTR = re.compile(r'\bid\s*=\s*"([^"]*)"')
PROSE = re.compile(r'<div class="prose">(.*?)</div>\s*<nav class="pager"', re.S)


def slug(text: str) -> str:
    text = TAG.sub("", text)
    text = text.replace("&amp;", "and").replace("&#39;", "").replace("&quot;", "")
    # Decompose, then drop the combining marks: without this every accented
    # character becomes a separator, so "Conformité" slugs to "conformit".
    # These pages are English today, but an id scheme that only survives
    # ASCII is a trap for whoever adds a translation.
    text = unicodedata.normalize("NFKD", text)
    text = "".join(c for c in text if not unicodedata.combining(c))
    text = text.lower().strip()
    text = re.sub(r"[^a-z0-9]+", "-", text)
    return re.sub(r"-{2,}", "-", text).strip("-")


def add_ids(html: str) -> tuple[str, set[str]]:
    """Add an id to every h2-h4 that lacks one. Returns the html and all ids."""
    seen: set[str] = set(ID_ATTR.findall(html))
    added: set[str] = set()

    def repl(m: re.Match[str]) -> str:
        tag, attrs, inner = m.group(1), m.group(2), m.group(3)
        if "id=" in attrs:
            return m.group(0)
        base = slug(inner)
        if not base:
            return m.group(0)
        # Two packages can both export Execute; duplicate ids are invalid
        # HTML and give a screen reader two targets with one name.
        candidate, n = base, 2
        while candidate in seen:
            candidate, n = f"{base}-{n}", n + 1
        seen.add(candidate)
        added.add(candidate)
        return f'<{tag}{attrs} id="{candidate}">{inner}</{tag}>'

    return HEADING.sub(repl, html), seen


def main() -> int:
    root = pathlib.Path(sys.argv[1] if len(sys.argv) > 1 else "public")
    pages = sorted(root.rglob("index.html"))
    if not pages:
        print(f"anchor: no pages under {root}", file=sys.stderr)
        return 1

    dangling: list[str] = []
    total_ids = 0
    for page in pages:
        html = page.read_text(encoding="utf-8")
        out, ids = add_ids(html)
        if out != html:
            page.write_text(out, encoding="utf-8")
        total_ids += len(ids)

        # Every same-page fragment must now resolve.
        for href in re.findall(r'href="#([^"]+)"', out):
            if href not in ids:
                rel = page.relative_to(root)
                dangling.append(f"{rel}: #{href}")

    if dangling:
        print(f"anchor: {len(dangling)} dangling fragment(s):", file=sys.stderr)
        for d in dangling[:20]:
            print(f"  {d}", file=sys.stderr)
        return 1

    print(f"anchor: {len(pages)} page(s), {total_ids} ids, every fragment resolves")
    return 0


if __name__ == "__main__":
    sys.exit(main())
