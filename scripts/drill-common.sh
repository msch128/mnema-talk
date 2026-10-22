#!/usr/bin/env bash
# Shared by upgrade-drill.sh and backup-drill.sh (sourced, not executed):
# which released versions a drill starts from, and how a released image is
# pulled. Tested by scripts/tests/test_drill_versions.py (`make test-scripts`).

# The releases every drill starts from unless FROM_VERSIONS says otherwise.
# 0.8.16 is the last 0.8; 0.9.0 is the first release with notification levels,
# role mentions and onboarding. Add the previous 1.x release here once it exists.
DRILL_DEFAULT_FROM_VERSIONS='0.8.16 0.9.0'
# The seed uses the roles, webhook and bot APIs, which start with 0.8.
DRILL_MIN_FROM_VERSION='0.8.0'
REGISTRY_IMAGE="${REGISTRY_IMAGE:-ghcr.io/msch128/mnema-talk}"

drill_fail() { echo "$*" >&2; return 1; }

drill_semver_ok() { [[ "${1:-}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; }

# drill_version_ge A B: A is B or newer.
drill_version_ge() {
  [ "$(printf '%s\n%s\n' "$1" "$2" | sort -V | head -n 1)" = "$2" ]
}

# drill_resolve_versions LIST: validates a space- or comma-separated list of
# released versions and prints them one per line, in order, without repeats.
# An empty list means the default matrix. A malformed or too old entry is an
# error, never silently dropped.
drill_resolve_versions() {
  local list="${1:-$DRILL_DEFAULT_FROM_VERSIONS}" ver seen=' ' out=()
  list="${list//,/ }"
  for ver in $list; do
    ver="${ver#v}"
    drill_semver_ok "$ver" || { drill_fail "FROM_VERSIONS: '$ver' is not a released version like 0.9.0"; return 1; }
    drill_version_ge "$ver" "$DRILL_MIN_FROM_VERSION" ||
      { drill_fail "FROM_VERSIONS: $ver is older than $DRILL_MIN_FROM_VERSION, which the seed data needs"; return 1; }
    case "$seen" in *" $ver "*) continue ;; esac
    seen="$seen$ver "
    out+=("$ver")
  done
  [ "${#out[@]}" -gt 0 ] || { drill_fail 'FROM_VERSIONS is empty'; return 1; }
  printf '%s\n' "${out[@]}"
}

# drill_pull_image IMAGE: pulls with a bounded retry (DRILL_PULL_ATTEMPTS,
# default 4, DRILL_PULL_DELAY seconds times the attempt number, default 15).
# A missing tag fails at once; an unreachable registry fails after the last
# attempt with a message that says so. The drill is never skipped.
drill_pull_image() {
  local image="$1" attempts="${DRILL_PULL_ATTEMPTS:-4}" delay="${DRILL_PULL_DELAY:-15}" n=1 out
  while :; do
    if out="$(docker pull --quiet "$image" 2>&1)"; then
      return 0
    fi
    if grep -qiE 'manifest unknown|not found|name unknown|no such manifest' <<<"$out"; then
      drill_fail "image $image does not exist in the registry: $out"
      return 1
    fi
    if [ "$n" -ge "$attempts" ]; then
      drill_fail "registry unavailable: could not pull $image after $n attempts ($out). Failing the drill on purpose; it is not skipped. Re-run the job once the registry is back."
      return 1
    fi
    echo "pull of $image failed (attempt $n of $attempts), retrying in $((delay * n))s: $out" >&2
    sleep "$((delay * n))"
    n=$((n + 1))
  done
}
