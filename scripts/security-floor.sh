#!/usr/bin/env sh
# Decides whether a release raises the desktop bundle's security floor.
# Usage: scripts/security-floor.sh <git-range>
# Prints "raise" when at least one commit in the range carries the trailer
# `Security-Floor: raise` (a real git trailer: last paragraph of the message),
# otherwise "keep". The commit subject plays no part. A commit whose subject
# has the `security` scope but no trailer only produces a warning on stderr,
# so a forgotten mark is visible without raising the floor by accident.
set -eu
range=${1:?usage: security-floor.sh <git-range>}
if git log --format='%(trailers:key=Security-Floor,valueonly,separator=%x0a)' "$range" |
  tr -d '\r' | tr 'A-Z' 'a-z' | grep -qx '[[:space:]]*raise[[:space:]]*'; then
  echo raise
else
  echo keep
fi
git log --format='%H %s' "$range" | grep -E '^[0-9a-f]+ [a-z]+\([^)]*security[^)]*\)!?: ' |
  while read -r sha subject; do
    if ! git log -1 --format='%(trailers:key=Security-Floor,valueonly)' "$sha" | grep -qi raise; then
      echo "warning: ${sha%"${sha#????????}"} \"$subject\" has a security scope but no 'Security-Floor: raise' trailer; the floor is not raised for it" >&2
    fi
  done || true
