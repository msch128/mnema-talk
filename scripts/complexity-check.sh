#!/usr/bin/env bash
# Fails when a Go function in production code has a cognitive complexity above
# 15 (gocognit rules) unless it is listed in scripts/complexity-allowlist.txt.
# The allowlist only shrinks: an entry for a function that is within the limit
# now fails the check too, so it gets removed.
# Usage: scripts/complexity-check.sh <gocognit-version>
set -euo pipefail
cd "$(dirname "$0")/.."

version="${1:?gocognit version}"
allow="scripts/complexity-allowlist.txt"

# gocognit prints "<score> <pkg> <func> <path>:<line>:<col>"; exit status 1
# means "something is over the limit", which is what we compare ourselves.
found="$( (go run "github.com/uudashr/gocognit/cmd/gocognit@${version}" -over 15 -ignore '_test\.go' cmd internal loadtest/viewer || true) |
	awk '{ fn = $3; sub(/^\([^)]*\)\./, "", fn); path = $4; sub(/:[0-9]+:[0-9]+$/, "", path); sub(/\/[^\/]*$/, "", path); print path " " fn }' |
	sort -u)"

expected="$(grep -v '^#' "$allow" | sed '/^$/d' | sort -u)"

new="$(comm -23 <(printf '%s\n' "$found") <(printf '%s\n' "$expected") | sed '/^$/d')"
stale="$(comm -13 <(printf '%s\n' "$found") <(printf '%s\n' "$expected") | sed '/^$/d')"

status=0
if [ -n "$new" ]; then
	echo "cognitive complexity above 15 (split the function into smaller ones):"
	echo "$new" | sed 's/^/  /'
	status=1
fi
if [ -n "$stale" ]; then
	echo "within the limit now, remove from $allow:"
	echo "$stale" | sed 's/^/  /'
	status=1
fi
exit "$status"
