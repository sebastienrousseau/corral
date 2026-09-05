#!/usr/bin/env python3

# SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
# SPDX-License-Identifier: GPL-3.0-only

"""Assert the documentation theme's colour pairs at WCAG AAA.

docs-site/_layouts/styles.css has claimed since it was written that "every
pair the theme actually renders is asserted at WCAG AAA by
scripts/contrast.py, so a colour change that breaks contrast fails the build
rather than reaching a reader".

That script did not exist. The claim was load-bearing in exactly the way a
claim should not be: it is the reason someone would feel safe changing a
colour, and nothing behind it would have caught them. This is that script.

Every token block in the stylesheet is checked independently, because the
theme has three (`:root`/light, `[data-theme="dark"]`, and the
`prefers-color-scheme` block that repeats the dark values for viewers who
have set no explicit preference). The third is where a recolour silently
half-lands: change the two explicit blocks, forget the media query, and the
default-setting majority get one theme's text on the other theme's ground.

    python3 scripts/contrast.py [--verbose]

Exits non-zero, naming every failing pair and the ratio it reached.
"""

import re
import sys

CSS = "docs-site/_layouts/styles.css"

# Text has to clear 7:1 for AAA. Non-text — focus rings, borders — is held to
# the 3:1 that WCAG 1.4.11 asks of a user-interface component, which is the
# right bar for something you locate rather than read.
AAA_TEXT = 7.0
NON_TEXT = 3.0

# The pairs the theme actually renders: a foreground token drawn on a
# background token. A pair nobody paints would be noise, and a pair the theme
# paints but that is missing here is the gap this file exists to close, so
# each entry names where it shows up.
PAIRS = [
    # (foreground, background, minimum, what renders it)
    ("--ink", "--bg", AAA_TEXT, "body text"),
    ("--ink", "--surface", AAA_TEXT, "text on cards and code blocks"),
    ("--ink", "--surface-soft", AAA_TEXT, "text on the active sidebar item"),
    ("--ink-soft", "--bg", AAA_TEXT, "secondary prose"),
    ("--ink-soft", "--surface", AAA_TEXT, "secondary prose on a card"),
    ("--ink-muted", "--bg", AAA_TEXT, "captions and metadata"),
    ("--ink-muted", "--surface", AAA_TEXT, "captions on a card"),
    ("--accent", "--bg", AAA_TEXT, "links in prose"),
    ("--accent", "--surface", AAA_TEXT, "links on a card"),
    ("--accent-hover", "--bg", AAA_TEXT, "a link under the cursor"),
    ("--accent-ink", "--accent", AAA_TEXT, "button label on the accent fill"),
    ("--on-accent-soft", "--accent-soft", AAA_TEXT, "text on the accent tint"),
    ("--focus", "--bg", NON_TEXT, "the focus ring"),
    ("--focus", "--surface", NON_TEXT, "the focus ring over a card"),
    ("--line", "--bg", NON_TEXT, "rules and borders"),
]


def srgb_to_linear(c: float) -> float:
    """One sRGB channel, 0-1, to linear light."""
    return c / 12.92 if c <= 0.04045 else ((c + 0.055) / 1.055) ** 2.4


def luminance(hex_colour: str) -> float:
    """Relative luminance per WCAG 2.x."""
    h = hex_colour.lstrip("#")
    if len(h) == 3:
        h = "".join(ch * 2 for ch in h)
    r, g, b = (int(h[i:i + 2], 16) / 255 for i in (0, 2, 4))
    return (
        0.2126 * srgb_to_linear(r)
        + 0.7152 * srgb_to_linear(g)
        + 0.0722 * srgb_to_linear(b)
    )


def ratio(fg: str, bg: str) -> float:
    """Contrast ratio between two hex colours, lighter over darker."""
    a, b = luminance(fg), luminance(bg)
    lo, hi = sorted((a, b))
    return (hi + 0.05) / (lo + 0.05)


COMMENT = re.compile(r"/\*.*?\*/", re.S)
BLOCK = re.compile(r"([^{}]+)\{([^{}]*)\}", re.S)
TOKEN = re.compile(r"(--[a-z-]+)\s*:\s*(#[0-9a-fA-F]{3,8})\s*;")


def blocks(css: str):
    """Yield (selector, {token: hex}) for every block that defines tokens."""
    # Comments are stripped first: without that, a block's "selector" is
    # everything since the previous brace, which for the first rule in the
    # file is the entire licence header.
    for m in BLOCK.finditer(COMMENT.sub(" ", css)):
        selector = " ".join(m.group(1).split())
        tokens = dict(TOKEN.findall(m.group(2)))
        if tokens:
            yield selector, tokens


def main() -> int:
    verbose = "--verbose" in sys.argv
    try:
        with open(CSS, encoding="utf-8") as fh:
            css = fh.read()
    except OSError as exc:
        print(f"contrast: reading {CSS}: {exc}", file=sys.stderr)
        return 1

    found = list(blocks(css))
    if not found:
        print(f"contrast: no colour tokens in {CSS}", file=sys.stderr)
        return 1

    failures = []
    checked = 0
    for selector, tokens in found:
        for fg, bg, minimum, what in PAIRS:
            if fg not in tokens or bg not in tokens:
                # A block that defines only some tokens inherits the rest,
                # which is fine; a block defining neither half of a pair has
                # nothing to check.
                if fg in tokens or bg in tokens:
                    failures.append(
                        f"{selector}: defines only one of {fg}/{bg}, so {what} "
                        f"takes one theme's colour on another theme's ground"
                    )
                continue
            got = ratio(tokens[fg], tokens[bg])
            checked += 1
            if verbose:
                print(f"  {got:5.2f}:1  {fg} on {bg}  [{selector}] — {what}")
            if got + 1e-9 < minimum:
                failures.append(
                    f"{selector}: {fg} ({tokens[fg]}) on {bg} ({tokens[bg]}) "
                    f"is {got:.2f}:1, below {minimum:.0f}:1 — {what}"
                )

    if failures:
        print("contrast check failed:", file=sys.stderr)
        for f in failures:
            print("  - " + f, file=sys.stderr)
        return 1

    print(
        f"Contrast: {checked} pairs across {len(found)} token blocks, "
        f"all at or above AAA"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
