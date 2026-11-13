#!/usr/bin/env bash
# Runs staticcheck (default checks) over the Go modules (the main one with and
# without the integration tag, the load-test viewer, the bot SDK with and
# without its contract tag), and fails on every finding that is not listed in
# scripts/staticcheck-allowlist.txt. The allowlist only shrinks: an entry that
# no longer has a finding fails the check too, so it gets removed. A
# //lint:ignore directive counts as a finding of its file, so it needs an entry
# as well, and staticcheck.conf files fail the check: the allowlist is the only
# place that excuses anything.
# Usage: scripts/staticcheck.sh <staticcheck-version>
set -euo pipefail
cd "$(dirname "$0")/.."

version="${1:?staticcheck version}"
allow="scripts/staticcheck-allowlist.txt"
tool="honnef.co/go/tools/cmd/staticcheck@${version}"

# run <module dir> [flags]: the findings as "<repo path>:<line>:<col>: <text> (<ID>)".
# Exit status 1 means "there are findings", which is what we compare ourselves.
# "go: downloading" lines of a cold module cache are not findings.
run() {
	local dir="$1" prefix=""
	shift
	[ "$dir" = . ] || prefix="$dir/"
	(cd "$dir" && go run "$tool" "$@" ./... 2>&1 || true) | sed -e '/^exit status 1$/d' -e '/^go: downloading /d' -e "/^[^[:space:]]/s|^|$prefix|"
}

raw="$(
	run .
	run . -tags=integration
	run loadtest/viewer
	run sdk/go
	run sdk/go -tags=contract
)"

# A line that is neither a finding nor the indented detail of one is a failure
# of the tool itself (no network, a package that does not compile).
broken="$(printf '%s\n' "$raw" | grep -vE '^[^[:space:]]+\.go:[0-9]+:[0-9]+: .* \([A-Za-z]+[0-9]*\)$|^[[:space:]]|^$' || true)"
if [ -n "$broken" ]; then
	echo "staticcheck did not run cleanly:"
	echo "$broken" | sed 's/^/  /'
	exit 1
fi

# "<path>:<line>:	//lint:ignore SA1019,U1000 reason" becomes one finding per ID.
silenced="$(git grep -nE '^[[:space:]]*//lint:(file-)?ignore[[:space:]]' -- '*.go' |
	awk '{ split($1, at, ":"); n = split($3, ids, ","); for (i = 1; i <= n; i++) print at[1] ":" at[2] ":1: silenced by a //lint:ignore directive (" ids[i] ")" }' || true)"

findings="$(printf '%s\n%s\n' "$raw" "$silenced" | grep -E '^[^[:space:]]' | sort -u || true)"
found="$(printf '%s\n' "$findings" | sed -E 's/^([^:]+):[0-9]+:[0-9]+: .* \(([A-Za-z]+[0-9]*)\)$/\1 \2/' | sed '/^$/d' | sort -u)"
expected="$( (grep -v '^#' "$allow" || true) | awk 'NF { print $1 " " $2 }' | sort -u)"

new="$(comm -23 <(printf '%s\n' "$found") <(printf '%s\n' "$expected") | sed '/^$/d')"
stale="$(comm -13 <(printf '%s\n' "$found") <(printf '%s\n' "$expected") | sed '/^$/d')"
configs="$(git ls-files -co --exclude-standard -- 'staticcheck.conf' '**/staticcheck.conf')"

status=0
if [ -n "$new" ]; then
	echo "staticcheck findings (fix the code, do not silence it):"
	while read -r path id; do
		printf '%s\n' "$findings" | grep -F "$path:" | grep -F "($id)" | sed 's/^/  /'
	done <<<"$new"
	status=1
fi
if [ -n "$stale" ]; then
	echo "no finding any more, remove from $allow:"
	echo "$stale" | sed 's/^/  /'
	status=1
fi
if [ -n "$configs" ]; then
	echo "staticcheck runs with its default checks, remove:"
	echo "$configs" | sed 's/^/  /'
	status=1
fi
exit "$status"
