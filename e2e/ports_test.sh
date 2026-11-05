#!/usr/bin/env bash
# Behaviour test for e2e/ports.sh: the TURN port never lies in the UDP range,
# even when the kernel keeps offering ports from inside it.
set -u
. "$(dirname "$0")/ports.sh"
fail=0

# The first three "free" ports are inside 40000-40050 (the old logic took the
# first one); the fourth is outside.
offers=(40000 40025 40050 40051)
# free_port runs in a command substitution, so the position lives in a file.
pos="$(mktemp)"; trap 'rm -f "$pos"' EXIT; echo 0 > "$pos"
free_port() { local i; i="$(cat "$pos")"; echo $((i + 1)) > "$pos"; echo "${offers[$i]}"; }
got="$(pick_turn_port 40000 40050)"
if [ "$got" != 40051 ]; then echo "FAIL: got '$got', want 40051 (outside 40000-40050)"; fail=1; fi

# Boundaries are inside the range.
for p in 40000 40050; do port_in_range "$p" 40000 40050 || { echo "FAIL: $p should be in range"; fail=1; }; done
for p in 39999 40051; do port_in_range "$p" 40000 40050 && { echo "FAIL: $p should be out of range"; fail=1; }; done

# A kernel that only ever offers in-range ports ends in failure, not in a bad port.
free_port() { echo 40010; }
if got="$(pick_turn_port 40000 40050)"; then echo "FAIL: returned '$got' from inside the range"; fail=1; fi

[ "$fail" = 0 ] && echo "ok"
exit "$fail"
