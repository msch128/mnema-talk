#!/usr/bin/env bash
# One tracking issue per alert, for scheduled workflows that watch something
# (the production probe, the restore drill). The alert is the issue: GitHub
# sends the notification, and there is no third-party service.
#
#   ops-issue.sh open  LABEL TITLE BODY_FILE   open the issue, or comment on the
#                                              one that is already open
#   ops-issue.sh close LABEL COMMENT           close it again with a comment
#
# Needs gh, GH_TOKEN and GITHUB_REPOSITORY, and `issues: write` on the token.
set -euo pipefail

action="${1:-}"
label="${2:-}"
[[ "$label" =~ ^[a-z0-9][a-z0-9-]*$ ]] || { echo "usage: ops-issue.sh open LABEL TITLE BODY_FILE | close LABEL COMMENT" >&2; exit 2; }

existing() {
  gh issue list --repo "$GITHUB_REPOSITORY" --label "$label" --state open --json number --jq '.[0].number // empty'
}

case "$action" in
  open)
    title="${3:?title}" body_file="${4:?body file}"
    [ -s "$body_file" ] || { echo "ops-issue.sh: empty body" >&2; exit 2; }
    gh label create "$label" --repo "$GITHUB_REPOSITORY" --color B60205 \
      --description "Opened and closed by a scheduled workflow" >/dev/null 2>&1 || true
    number="$(existing)"
    if [ -n "$number" ]; then
      gh issue comment "$number" --repo "$GITHUB_REPOSITORY" --body-file "$body_file"
    else
      gh issue create --repo "$GITHUB_REPOSITORY" --label "$label" --title "$title" --body-file "$body_file"
    fi
    ;;
  close)
    comment="${3:?comment}"
    number="$(existing)"
    [ -z "$number" ] || gh issue close "$number" --repo "$GITHUB_REPOSITORY" --reason completed --comment "$comment"
    ;;
  *)
    echo "usage: ops-issue.sh open LABEL TITLE BODY_FILE | close LABEL COMMENT" >&2
    exit 2
    ;;
esac
