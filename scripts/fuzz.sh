#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
# SPDX-License-Identifier: GPL-3.0-only
#
# Run one fuzz target, and tell a finding apart from the clock running out.
#
#   scripts/fuzz.sh <Target> <package> <fuzztime>
#
# Why this exists
#
# `go test -fuzz=X -fuzztime=30s` intermittently exits non-zero with nothing
# but:
#
#     --- FAIL: FuzzParseOwnerFromURL (31.00s)
#         context deadline exceeded
#
# No failing input, no crash, no corpus entry — the coordinator simply did
# not wind its workers down inside the deadline. It happened twice in one
# day here, on two different targets, while the same targets passed on
# re-run and passed repeatedly on main.
#
# A required check that fails at random is worse than no check: people learn
# to re-run it, and then they re-run it on the day it was right. So the two
# outcomes are separated on evidence rather than on the exit code.
#
# The discriminator is the corpus. When fuzzing finds something it writes
# the input to testdata/fuzz/<Target>/ and says so. A deadline writes
# nothing. So:
#
#   - a new corpus file      -> failure, always, whatever else was printed
#   - "Failing input written"-> failure
#   - a panic                -> failure
#   - a bare deadline        -> warn and pass
#   - anything else non-zero -> failure
#
# The bias is deliberate: every rule but one ends in failure, and the single
# pass is the narrowest case that can be described.
set -uo pipefail

if [ "$#" -ne 3 ]; then
  echo "usage: $0 <FuzzTarget> <package> <fuzztime>" >&2
  exit 2
fi

target="$1"
pkg="$2"
fuzztime="$3"

# The corpus for a target lives beside its package.
corpus_dir="${pkg#./}/testdata/fuzz/${target}"

corpus_count() {
  if [ -d "$corpus_dir" ]; then
    find "$corpus_dir" -type f | wc -l | tr -d ' '
  else
    echo 0
  fi
}

before="$(corpus_count)"

echo "==> fuzzing ${target} in ${pkg} for ${fuzztime}"
output="$(go test "-fuzz=${target}" "-fuzztime=${fuzztime}" "$pkg" 2>&1)"
status=$?
echo "$output"

after="$(corpus_count)"

if [ "$status" -eq 0 ]; then
  exit 0
fi

# A new corpus entry means fuzzing found something and saved it. That is a
# finding regardless of anything else in the output.
if [ "$after" -gt "$before" ]; then
  echo "::error::${target} found a new failing input (corpus grew ${before} -> ${after}); it is saved under ${corpus_dir}" >&2
  exit 1
fi

case "$output" in
  *"Failing input written to"*|*"panic:"*|*"FUZZ FAILED"*)
    echo "::error::${target} failed with a reproducible input" >&2
    exit 1
    ;;
esac

# The narrow pass: the only complaint is that time ran out.
if [[ "$output" == *"context deadline exceeded"* ]]; then
  echo "::warning::${target} hit the ${fuzztime} deadline without winding down (no failing input, no new corpus entry). Treating as a pass; this is a known Go fuzzing artefact, not a finding." >&2
  exit 0
fi

echo "::error::${target} failed for a reason that is not a deadline" >&2
exit "$status"
