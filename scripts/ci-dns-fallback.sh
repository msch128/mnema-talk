#!/usr/bin/env bash
# CI only: when the runner's own resolver does not answer for a host that apt
# needs, point the runner at public resolvers. Usage:
#   scripts/ci-dns-fallback.sh archive.ubuntu.com azure.archive.ubuntu.com
#
# Why: on 2026-11-11 the resolver of the GitHub runners failed for the Ubuntu
# archive names for more than an hour ("Temporary failure resolving") while the
# names resolved everywhere else, so every job that installs a package failed.
# The script changes nothing while the resolver answers.
set -uo pipefail

answers() { getent hosts "$1" >/dev/null 2>&1; }

for host in "$@"; do
  answers "$host" && continue
  echo "==> the runner's resolver does not answer for $host: switching to public resolvers"
  link="$(ip route show default 2>/dev/null | awk '{print $5; exit}')"
  if [ -n "$link" ] && command -v resolvectl >/dev/null 2>&1 &&
    sudo resolvectl dns "$link" 1.1.1.1 8.8.8.8 && sudo resolvectl flush-caches; then
    :
  else
    printf 'nameserver 1.1.1.1\nnameserver 8.8.8.8\n' | sudo tee /etc/resolv.conf >/dev/null
  fi
  if answers "$host"; then
    echo "==> $host resolves now"
  else
    echo "==> $host still does not resolve" >&2
  fi
  break
done
exit 0
