#!/usr/bin/env sh
# Decides whether a release raises the desktop bundle's security floor.
# Usage: scripts/security-floor.sh <git-range>
# Prints "raise" when at least one commit in the range carries the trailer
# `Security-Floor: raise` (a real git trailer: last paragraph of the message),
# otherwise "keep". The commit subject plays no part. A commit whose subject
# has the `security` scope but no trailer only produces a warning on stderr,
# so a forgotten mark is visible without raising the floor by accident.
# A message with a `Security-Floor: raise` line that git does not read as a
# trailer (another paragraph follows it) fails the decision with a non-zero
# exit: the mark was meant and would otherwise be dropped without a word.
set -eu
range=${1:?usage: security-floor.sh <git-range>}
marker='security-floor[[:space:]]*:[[:space:]]*raise'
misplaced=$(git log --format=%H "$range" | while read -r sha; do
  marks=$(git log -1 --format=%B "$sha" | tr -d '\r' | grep -Eic "^[[:space:]]*$marker[[:space:]]*\$" || true)
  trailers=$(git log -1 --format='%(trailers:key=Security-Floor)' "$sha" | tr -d '\r' | grep -Eic "^$marker[[:space:]]*\$" || true)
  if [ "$marks" -gt "$trailers" ]; then echo "${sha%"${sha#????????}"}"; fi
done)
if [ -n "$misplaced" ]; then
  for sha in $misplaced; do
    echo "error: $sha has a 'Security-Floor: raise' line that is not a git trailer (it must be in the last paragraph of the message); no floor decision is made" >&2
  done
  exit 1
fi
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
