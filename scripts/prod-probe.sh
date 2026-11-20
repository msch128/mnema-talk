#!/usr/bin/env bash
# Black-box probe of a public Mnema Talk server, run from outside by the
# scheduled workflow .github/workflows/prod-probe.yml. It looks only at what
# any visitor can see; it needs no token and no access to the host.
#
# Usage: scripts/prod-probe.sh BASE_URL        (e.g. https://talk.mnema.xyz)
#
# Checks (each failure is one line of the report, on stdout, as a list item):
#   1. GET /api/health answers 200 with {"status":"ok"} within 10 s. Up to
#      three tries (5 s and 10 s apart), so one dropped packet is no alert.
#   2. GET / serves the web client (200, HTML).
#   3. The TLS certificate is valid for at least 14 more days (https only).
#   4. The running version is not behind the newest release by more than
#      PROBE_MAX_LAG_HOURS (default 6). The self-updater applies a release
#      within about an hour, so a longer lag means it is stuck: a failed
#      signature check, a changed docker-compose.yml, an unhealthy start.
#
# Exit status: 0 all fine, 1 at least one check failed, 2 usage error.
# Environment (all optional):
#   PROBE_RELEASE_URL     API URL of the newest release
#                         (default https://api.github.com/repos/mnema-xyz/talk/releases/latest)
#   PROBE_MAX_LAG_HOURS   see 4 (default 6)
#   PROBE_RETRY_DELAY     base pause between health tries in seconds (default 5)
#   GH_TOKEN              sent to the GitHub API when set (higher rate limit)
set -uo pipefail

base="${1:-}"
[[ "$base" =~ ^https?://[^/[:space:]]+$ ]] || { echo "usage: prod-probe.sh BASE_URL (scheme and host, no path)" >&2; exit 2; }
release_url="${PROBE_RELEASE_URL:-https://api.github.com/repos/mnema-xyz/talk/releases/latest}"
max_lag_h="${PROBE_MAX_LAG_HOURS:-6}"
delay="${PROBE_RETRY_DELAY:-5}"
failed=0
fail() { printf -- '- %s\n' "$*"; failed=1; }

# --- 1. health ----------------------------------------------------------------
health=''
for attempt in 1 2 3; do
  if health="$(curl -fsS --max-time 10 --proto '=http,https' "$base/api/health" 2>&1)" &&
    [ "$(jq -r .status <<<"$health" 2>/dev/null)" = ok ]; then
    break
  fi
  [ "$attempt" -lt 3 ] && sleep "$((delay * attempt))"
  health_error="${health:-no answer}"
  health=''
done
if [ -z "$health" ]; then
  fail "\`/api/health\` did not report status ok after 3 tries: $(printf '%s' "${health_error:-no answer}" | head -c 200 | tr '\n' ' ')"
fi
running="$(jq -r '.version // empty' <<<"$health" 2>/dev/null)"

# --- 2. web client --------------------------------------------------------------
page="$(curl -fsS --max-time 10 --proto '=http,https' -H 'Accept: text/html' "$base/" 2>&1)" || page=''
grep -qi '<html' <<<"$page" || fail "\`/\` does not serve the web client"

# --- 3. certificate ---------------------------------------------------------------
if [[ "$base" == https://* ]]; then
  hostport="${base#https://}"
  host="${hostport%%:*}"
  port=443
  [[ "$hostport" == *:* ]] && port="${hostport##*:}"
  if ! echo | openssl s_client -servername "$host" -connect "$host:$port" 2>/dev/null |
    openssl x509 -noout -checkend $((14 * 86400)) >/dev/null 2>&1; then
    fail "the TLS certificate of $host expires within 14 days or cannot be read"
  fi
fi

# --- 4. version lag -----------------------------------------------------------------
auth=()
[ -n "${GH_TOKEN:-}" ] && auth=(-H "Authorization: Bearer $GH_TOKEN")
if release="$(curl -fsS --max-time 20 --retry 2 --retry-delay 3 ${auth[@]+"${auth[@]}"} -H 'Accept: application/vnd.github+json' "$release_url" 2>/dev/null)"; then
  latest="$(jq -r '.tag_name // empty | ltrimstr("v")' <<<"$release")"
  published="$(jq -r '.published_at // empty' <<<"$release")"
  if [ -n "$running" ] && [ -n "$latest" ] && [ "$running" != "$latest" ] &&
    [ "$(printf '%s\n%s\n' "$running" "$latest" | sort -V | tail -n 1)" = "$latest" ]; then
    # GNU date (runners) or BSD date (a laptop).
    then_s="$(date -u -d "$published" +%s 2>/dev/null || date -u -j -f '%Y-%m-%dT%H:%M:%SZ' "$published" +%s 2>/dev/null || echo '')"
    if [ -n "$then_s" ] && [ $(($(date -u +%s) - then_s)) -gt $((max_lag_h * 3600)) ]; then
      fail "the server runs $running, but $latest was released more than ${max_lag_h}h ago: the self-updater is stuck (signature, changed docker-compose.yml or an unhealthy start; see doc/updating.md)"
    fi
  fi
else
  echo "note: the release API did not answer; the version-lag check was skipped" >&2
fi

[ "$failed" = 0 ] && echo "ok: $base answers, running ${running:-unknown}"
exit "$failed"
