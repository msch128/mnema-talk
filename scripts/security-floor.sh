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
# A commit of this release (previous release tag..<rev>) with a line that
# starts with `Security-Floor` but is not exactly the trailer
# `Security-Floor: raise` in the message's trailer block (another paragraph
# follows it, or a near miss such as `Security-Floor: raise.`) fails the
# decision with a non-zero exit: the mark was meant and would otherwise be
# dropped without a word. Older releases are not checked again. The two
# --check modes apply the same check (the same function) to the commits of a
# pull request and to its description (CI), before anything reaches main.
set -eu

# raises <message>: how many `Security-Floor: raise` trailers git reads in
# the message. --no-divider: a `---` line does not end the message here, as
# it does not for a commit's %(trailers).
raises() {
  printf '%s\n' "$1" | tr -d '\r' | git interpret-trailers --parse --no-divider | grep -c '^Security-Floor: raise$' || true
}

# misplaced <name> <message>: an error unless every line starting with
# Security-Floor (any case, after blanks) is exactly the marker and each is
# read as a trailer.
misplaced() {
  text=$(printf '%s\n' "$2" | tr -d '\r')
  lines=$(printf '%s\n' "$text" | grep -ic '^[[:space:]]*security-floor' || true)
  exact=$(printf '%s\n' "$text" | grep -c '^Security-Floor: raise[[:space:]]*$' || true)
  if [ "$lines" -ne "$exact" ] || [ "$exact" -ne "$(raises "$text")" ]; then
    echo "error: $1 has a Security-Floor line that is not a git trailer 'Security-Floor: raise' (spelled exactly, in the last paragraph of the message); no floor decision is made" >&2
    return 1
  fi
}

# check_commits <range>: misplaced for each commit, and the trailers git
# reads with %(trailers) for the floor decision must be the ones raises
# counted, so the two readings can never silently disagree.
check_commits() {
  shas=$(git log --format=%H "$1") || { echo "error: cannot read the commits of '$1'" >&2; return 1; }
  bad=0
  for sha in $shas; do
    short=${sha%"${sha#????????}"}
    message=$(git log -1 --format=%B "$sha")
    misplaced "$short" "$message" || bad=1
    read_by_log=$(git log -1 --format='%(trailers:key=Security-Floor)' "$sha" | grep -c '^Security-Floor: raise$' || true)
    if [ "$read_by_log" -ne "$(raises "$message")" ]; then
      echo "error: git reads the Security-Floor trailer of $short in two ways; no floor decision is made" >&2
      bad=1
    fi
  done
  return "$bad"
}

case ${1:-} in
--check-commits)
  check_commits "${2:?usage: security-floor.sh --check-commits <git-range>}"
  exit
  ;;
--check-message)
  misplaced "the message" "$(cat)"
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
