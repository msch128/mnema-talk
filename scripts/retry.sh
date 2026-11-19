#!/usr/bin/env bash
# Runs a command again when it fails, a bounded number of times, with a
# growing pause between attempts. For CI steps that call an external service
# (Sigstore, a registry, the GitHub API) and fail now and then for reasons
# that have nothing to do with the change: a 502 from Fulcio or Rekor, a
# registry that resets the connection, a 5xx from the API.
#
# Usage: scripts/retry.sh [-n ATTEMPTS] [-d DELAY_SECONDS] -- COMMAND [ARG...]
#   -n  attempts in total, at least 1 (default 4, or RETRY_ATTEMPTS)
#   -d  base pause: attempt k is followed by k * DELAY seconds (default 10,
#       or RETRY_DELAY), so 10s, 20s, 30s with the defaults
#
# The exit status is that of the last attempt. A command that keeps failing
# fails the step; there is no skip and no continue-on-error. Only use it for
# commands that are safe to repeat (sign, verify, inspect, copy by digest,
# upload with --clobber).
set -uo pipefail

attempts="${RETRY_ATTEMPTS:-4}"
delay="${RETRY_DELAY:-10}"
while getopts ':n:d:' opt; do
  case "$opt" in
    n) attempts="$OPTARG" ;;
    d) delay="$OPTARG" ;;
    *) echo "usage: retry.sh [-n ATTEMPTS] [-d DELAY_SECONDS] -- COMMAND [ARG...]" >&2; exit 2 ;;
  esac
done
shift $((OPTIND - 1))
[ "${1:-}" = -- ] && shift
[ "$#" -gt 0 ] || { echo "retry.sh: no command given" >&2; exit 2; }
[[ "$attempts" =~ ^[1-9][0-9]*$ ]] || { echo "retry.sh: attempts must be a positive integer" >&2; exit 2; }
[[ "$delay" =~ ^[0-9]+$ ]] || { echo "retry.sh: delay must be a whole number of seconds" >&2; exit 2; }

n=1
while :; do
  "$@" && exit 0
  status=$?
  if [ "$n" -ge "$attempts" ]; then
    echo "::error::retry.sh: '$1' failed after $n attempts (last exit status $status)" >&2
    exit "$status"
  fi
  echo "::warning::retry.sh: '$1' failed with status $status (attempt $n of $attempts), trying again in $((delay * n))s" >&2
  sleep "$((delay * n))"
  n=$((n + 1))
done
