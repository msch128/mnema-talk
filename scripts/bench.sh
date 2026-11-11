#!/usr/bin/env bash
# Runs the stored benchmarks and compares them with scripts/bench-baseline.txt:
# the hub's fan-out (internal/ws), one load of the permission snapshot with
# 10,000 members (internal/server; needs Docker or TEST_DATABASE_URL) and the
# SFU's forward path per packet (internal/sfu).
# Usage: scripts/bench.sh compare|record|allocs
#   compare  run everything and print the comparison; only more allocations
#            per operation on the SFU forward path fail it
#   record   run everything and rewrite the baseline (on an idle machine)
#   allocs   the SFU forward path alone with a fixed iteration count: the
#            allocation gate CI runs, no database needed
set -euo pipefail
cd "$(dirname "$0")/.."

mode="${1:?compare, record or allocs}"
baseline="scripts/bench-baseline.txt"
gate='BenchmarkForward'
out="$(mktemp)"
trap 'rm -f "$out"' EXIT

bench_sfu() { go test -run '^$' -bench "$gate" -benchmem "$@" ./internal/sfu; }
bench_all() {
	go test -run '^$' -bench '^Benchmark(VoiceRoomEvent|BroadcastChannel)$' -benchmem ./internal/ws
	PERM_SNAPSHOT_SCALE=full go test -tags=integration -run '^$' -bench '^BenchmarkPermissionSnapshotLoad$' -benchmem ./internal/server
	bench_sfu
}

case "$mode" in
allocs)
	bench_sfu -benchtime=20000x | tee "$out"
	;;
compare)
	bench_all | tee "$out"
	;;
record)
	{
		echo "# machine: $(uname -sm), $( (sysctl -n machdep.cpu.brand_string 2>/dev/null || grep -m1 'model name' /proc/cpuinfo | cut -d: -f2) | xargs), $(getconf _NPROCESSORS_ONLN) CPUs"
		echo "# go: $(go env GOVERSION), GOMAXPROCS=${GOMAXPROCS:-unset}"
		echo "# recorded: $(date -u +%Y-%m-%d), 1-minute load before the run: $(uptime | sed -E 's/.*load averages?: *([0-9.]+).*/\1/')"
		bench_all
	} | tee "$out"
	grep -E '^(#|goos:|goarch:|pkg:|cpu:|Benchmark)' "$out" >"$baseline"
	echo "wrote $baseline"
	exit 0
	;;
*)
	echo "usage: scripts/bench.sh compare|record|allocs" >&2
	exit 2
	;;
esac
echo
node scripts/bench-compare.mjs "$baseline" "$out" --gate-allocs "$gate"
