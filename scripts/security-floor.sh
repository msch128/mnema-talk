#!/usr/bin/env sh
# Decides the desktop bundle's security floor for a release.
# Usage: scripts/security-floor.sh <rev> <version>
#        scripts/security-floor.sh --check-commits <git-range>
#        scripts/security-floor.sh --check-message < message
# <rev> is the release commit (the release tag in the release job) and
# <version> its version. Prints the floor the release declares: the highest
# version of a release that raised it, or an empty line when none did (then
# web/desktop-compat.json applies). A release raises the floor to itself when
# one of its commits carries the trailer `Security-Floor: raise` (a real git
# trailer: last paragraph of the message). Such a commit belongs to the first
# release tag vX.Y.Z that contains it, or to <version> when no tag up to <rev>
# does yet, so every later release repeats the raise: the floor never falls
# back. Computed from git history alone, so a rebuild from the tag yields the
# same floor; it needs the full history with tags and refuses a shallow clone.
# The commit subject plays no part. A commit of this release whose subject has
# the `security` scope but no trailer only produces a warning on stderr, so a
# forgotten mark is visible without raising the floor by accident.
# A commit of this release (previous release tag..<rev>) with a
# `Security-Floor: raise` line that git does not read as a trailer (another
# paragraph follows it) fails the decision with a non-zero exit: the mark was
# meant and would otherwise be dropped without a word. Older releases are not
# checked again. The two --check modes apply the same check to the commits of
# a pull request and to its description (CI), before anything reaches main.
set -eu
marker='security-floor[[:space:]]*:[[:space:]]*raise'

# misplaced <name> <message> <parsed trailers>: an error when the message has
# more marker lines than parsed raise trailers.
misplaced() {
  marks=$(printf '%s\n' "$2" | tr -d '\r' | grep -Eic "^[[:space:]]*$marker[[:space:]]*\$" || true)
  trailers=$(printf '%s\n' "$3" | tr -d '\r' | grep -Eic "^$marker[[:space:]]*\$" || true)
  if [ "$marks" -gt "$trailers" ]; then
    echo "error: $1 has a 'Security-Floor: raise' line that is not a git trailer (it must be in the last paragraph of the message); no floor decision is made" >&2
    return 1
  fi
}

check_commits() {
  bad=0
  for sha in $(git log --format=%H "$1"); do
    misplaced "${sha%"${sha#????????}"}" "$(git log -1 --format=%B "$sha")" \
      "$(git log -1 --format='%(trailers:key=Security-Floor)' "$sha")" || bad=1
  done
  return "$bad"
}

case ${1:-} in
--check-commits)
  check_commits "${2:?usage: security-floor.sh --check-commits <git-range>}"
  exit
  ;;
--check-message)
  message=$(cat)
  misplaced "the message" "$message" "$(printf '%s\n' "$message" | tr -d '\r' | git interpret-trailers --parse)"
  exit
  ;;
esac

usage='usage: security-floor.sh <rev> <version>'
rev=${1:?$usage}
version=${2:?$usage}
semver='^[0-9]+\.[0-9]+\.[0-9]+$'
printf '%s\n' "$version" | grep -Eq "$semver" || { echo "error: version '$version' is not X.Y.Z" >&2; exit 1; }
if [ "$(git rev-parse --is-shallow-repository)" != false ]; then
  echo "error: shallow clone; the floor needs the full history and the release tags" >&2
  exit 1
fi
git rev-parse -q --verify "$rev^{commit}" >/dev/null || { echo "error: '$rev' is not a commit" >&2; exit 1; }
prev=$(git describe --tags --abbrev=0 --match 'v[0-9]*' "$rev^" 2>/dev/null || true)
check_commits "${prev:+$prev..}$rev"
floor=
for sha in $(git log --format='@@sha %H%n%(trailers:key=Security-Floor,valueonly,separator=%x0a)' "$rev" |
  tr -d '\r' | awk '/^@@sha /{sha=$2; next} {v=tolower($0); gsub(/^[ \t]+|[ \t]+$/, "", v); if (v == "raise") print sha}'); do
  tag=$(git tag --contains "$sha" --merged "$rev" --list 'v*' --sort=v:refname | grep -E "^v${semver#^}" | head -n 1 || true)
  raised=${tag#v}
  raised=${raised:-$version}
  floor=$(printf '%s\n%s\n' "$floor" "$raised" | grep . | sort -t. -k1,1n -k2,2n -k3,3n | tail -n 1)
done
echo "$floor"
git log --format='%H %s' "${prev:+$prev..}$rev" | grep -E '^[0-9a-f]+ [a-z]+\([^)]*security[^)]*\)!?: ' |
  while read -r sha subject; do
    if ! git log -1 --format='%(trailers:key=Security-Floor,valueonly)' "$sha" | grep -qi raise; then
      echo "warning: ${sha%"${sha#????????}"} \"$subject\" has a security scope but no 'Security-Floor: raise' trailer; the floor is not raised for it" >&2
    fi
  done || true
