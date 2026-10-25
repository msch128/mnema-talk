#!/usr/bin/env python3
"""Fail when api/openapi.json breaks the API of the latest released version.

Compares the document in the working tree with the one at the latest stable
v* tag using oasdiff (version pinned by the Makefile) and fails on ERR-level
changes (doc/api-stability.md). A breaking change is waived only when a commit
of the change carries a `BREAKING CHANGE:` footer; CI passes the commit range
in OPENAPI_DIFF_RANGE, locally the range is origin/main..HEAD.

A change that is already present at the start of the range (the base branch)
was checked, and waived, when it landed there, so only the breaking changes
that this range adds need a footer. A second, new break still fails.

IGNORED_RULES lists oasdiff rule IDs that are known noise for the swag-generated
document. Every entry needs a reason; only justified entries go in.
"""
import json
import os
import re
import subprocess
import sys
import tempfile

SPEC = "api/openapi.json"
STABLE_TAG = re.compile(r"^v\d+\.\d+\.\d+$")
FOOTER = re.compile(r"^BREAKING[ -]CHANGE:\s*\S", re.MULTILINE)
ERR_LEVEL = 3

# rule id -> why it is not a real break of the public contract
IGNORED_RULES = {
    # A response value may now be larger than before (e.g. slow mode up to
    # 24 h). doc/api-stability.md only counts narrowing a range as breaking;
    # widening one is like a new enum value that clients must tolerate.
    "response-property-max-increased": "widening a documented response range is not breaking",
    # doc/api-stability.md: adding an enum value is not breaking; clients must
    # ignore values they do not know.
    "response-property-enum-value-added": "a new enum value in a response is not breaking",
}


def latest_tag(tags):
    """The highest stable vX.Y.Z tag, or None."""
    stable = [t for t in tags if STABLE_TAG.match(t)]
    return max(stable, key=lambda t: tuple(int(p) for p in t[1:].split("."))) if stable else None


def has_breaking_footer(messages):
    return any(FOOTER.search(m) for m in messages)


def errors(changes):
    """The ERR-level changes that are not filtered by rule id."""
    return [c for c in changes if c.get("level") == ERR_LEVEL and c.get("id") not in IGNORED_RULES]


def describe(change):
    return "%s (%s): %s" % (change.get("id", "?"), change.get("operation", "") + " " + change.get("path", ""), change.get("text", ""))


def run(*cmd):
    return subprocess.run(cmd, check=False, capture_output=True, text=True)


def change_key(change):
    return (change.get("id"), change.get("operation"), change.get("path"), change.get("text"))


def new_changes(broken, at_base):
    """The changes in broken that the base of the range did not have yet."""
    seen = {change_key(c) for c in at_base}
    return [c for c in broken if change_key(c) not in seen]


def breaking_against(tag_spec, head_path):
    """ERR-level changes from the tag's document to head_path, or None if oasdiff fails."""
    with tempfile.TemporaryDirectory() as tmp:
        base_path = os.path.join(tmp, "base.json")
        with open(base_path, "w") as f:
            f.write(tag_spec)
        version = os.environ.get("OASDIFF_VERSION", "v1.34.1")
        result = run("go", "run", "github.com/oasdiff/oasdiff@" + version, "breaking", base_path, head_path, "-f", "json")
    if result.returncode != 0:
        print("openapi-diff: oasdiff failed:\n" + result.stdout + result.stderr, file=sys.stderr)
        return None
    return errors(json.loads(result.stdout or "[]"))


def breaking_at_range_base(tag_spec, rev_range):
    """The breaking changes already present at the start of rev_range, or [] if unknown."""
    start = rev_range.split("..")[0]
    spec = run("git", "show", "%s:%s" % (start, SPEC)) if start else None
    if not spec or spec.returncode != 0:
        return []
    with tempfile.TemporaryDirectory() as tmp:
        path = os.path.join(tmp, "range-base.json")
        with open(path, "w") as f:
            f.write(spec.stdout)
        return breaking_against(tag_spec, path) or []


def commit_messages(rev_range):
    out = run("git", "log", "--format=%B%x00", rev_range)
    return [m for m in out.stdout.split("\0") if m.strip()] if out.returncode == 0 else []


def main():
    tags = run("git", "tag", "--list", "v*").stdout.split()
    tag = os.environ.get("OPENAPI_DIFF_BASE_TAG") or latest_tag(tags)
    if not tag:
        print("openapi-diff: no stable v* tag, nothing to compare with")
        return 0
    base = run("git", "show", "%s:%s" % (tag, SPEC))
    if base.returncode != 0:
        print("openapi-diff: %s has no %s, nothing to compare with" % (tag, SPEC))
        return 0
    broken = breaking_against(base.stdout, SPEC)
    if broken is None:
        return 2
    if not broken:
        print("openapi-diff: no breaking changes since %s" % tag)
        return 0
    print("openapi-diff: breaking changes since %s:" % tag)
    for change in broken:
        print("  - " + describe(change))
    rev_range = os.environ.get("OPENAPI_DIFF_RANGE", "origin/main..HEAD")
    if has_breaking_footer(commit_messages(rev_range)):
        print("openapi-diff: waived by a BREAKING CHANGE: footer in %s" % rev_range)
        return 0
    added = new_changes(broken, breaking_at_range_base(base.stdout, rev_range))
    if not added:
        print("openapi-diff: all of them are already on the base of %s, where they were waived" % rev_range)
        return 0
    print("openapi-diff: new in %s:" % rev_range)
    for change in added:
        print("  - " + describe(change))
    print("Removing or changing public API needs a major release (doc/api-stability.md)."
          " Deprecate it instead, or, if the break is intended, add a 'BREAKING CHANGE: <what>' footer to a commit.",
          file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main())
