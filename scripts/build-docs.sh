#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
# SPDX-License-Identifier: GPL-3.0-only

set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

# Builds https://doc.corrallib.com.
#
# Three steps, in this order and for a reason:
#
#   1. generate_docs.go walks the module and writes docs-site/content/
#      reference.md. It is generated on every build rather than committed,
#      so the published reference cannot drift from the source.
#   2. ssg renders the content through the vendored Lucid theme.
#   3. the theme's own assets are copied in. ssg renders templates; it does
#      not copy the stylesheet and scripts that sit beside them, so a site
#      that skips this step builds cleanly and serves unstyled HTML.
#
# The CNAME is stamped last: ssg writes an empty one, and an empty CNAME
# unbinds the custom domain.

OUT="public"

if ! command -v ssg >/dev/null 2>&1; then
  echo "error: no 'ssg' binary on PATH — install with: cargo binstall ssg" >&2
  exit 1
fi

echo "==> generating the package reference"
go run scripts/generate_docs.go

echo "==> building the site"
rm -rf "${OUT}" public.build-tmp
ssg build -f docs-site/ssg.toml

echo "==> copying theme assets"
for asset in styles.css main.js theme-init.js; do
  cp -f "docs-site/_layouts/${asset}" "${OUT}/${asset}"
done
mkdir -p "${OUT}/assets" "${OUT}/images"
cp -R docs-site/assets/. "${OUT}/assets/"
cp -R docs-site/images/. "${OUT}/images/"
cp -f docs-site/_layouts/favicon.ico "${OUT}/favicon.ico"

# ssg emits no heading ids, so every "On this page" link would point at
# nothing. This adds them and fails the build on a dangling fragment.
echo "==> anchoring headings"
python3 scripts/anchor_headings.py "${OUT}"

# The stylesheet says every colour pair it renders is asserted at AAA. It said
# so while scripts/contrast.py did not exist, which is the same class of
# untrue claim as the zero-byte favicon: something a reader would rely on,
# with nothing behind it. It exists now, and runs here.
echo "==> checking contrast"
python3 scripts/contrast.py

echo "doc.corrallib.com" > "${OUT}/CNAME"

# A page that references a stylesheet which is not there builds green and
# serves unstyled, which is how this was missed the first time.
#
# -s, not -f. The favicon shipped as a zero-byte file at both
# docs-site/assets/favicon.ico and docs-site/_layouts/favicon.ico, and this
# loop waved it through for months because the file existed. An empty asset
# is exactly as broken as a missing one and harder to notice — the browser
# just falls back to a blank page icon. Same reasoning as the CNAME check
# below, which already used -s for the same reason.
missing=0
for f in styles.css main.js theme-init.js favicon.ico assets/logo.svg; do
  [[ -s "${OUT}/${f}" ]] || { echo "missing or empty ${OUT}/${f}" >&2; missing=1; }
done
[[ -s "${OUT}/CNAME" ]] || { echo "CNAME is empty" >&2; missing=1; }
pages=$(find "${OUT}" -name index.html | wc -l | tr -d ' ')
[[ "${pages}" -ge 5 ]] || { echo "expected at least 5 pages, found ${pages}" >&2; missing=1; }
[[ "${missing}" -eq 0 ]] || exit 1

echo "==> ${pages} pages, assets present, CNAME stamped"
