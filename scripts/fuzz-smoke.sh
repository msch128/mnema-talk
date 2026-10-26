#!/usr/bin/env bash
# Runs every Go fuzz target for FUZZTIME (default 5s each). Seeds always run as
# ordinary tests in `go test`; this runs the mutation engine on top of them.
# Usage: scripts/fuzz-smoke.sh [package-pattern]   e.g. FUZZTIME=60s scripts/fuzz-smoke.sh ./internal/chat
set -euo pipefail
cd "$(dirname "$0")/.."

fuzztime="${FUZZTIME:-5s}"
pattern="${1:-./internal/...}"
status=0

# One "package target" pair per line; go test -fuzz accepts a single package and target at a time.
while read -r pkg target; do
  [ -n "$target" ] || continue
  echo "== $pkg $target ($fuzztime)"
  if ! go test "$pkg" -run '^$' -fuzz "^${target}\$" -fuzztime "$fuzztime"; then
    status=1
  fi
done < <(
  for pkg in $(go list "$pattern"); do
    dir="$(go list -f '{{.Dir}}' "$pkg")"
    grep -h -o '^func Fuzz[A-Za-z0-9_]*' "$dir"/*_test.go 2>/dev/null | sed 's/^func //' | sort -u | sed "s|^|$pkg |" || true
  done
)
exit "$status"
