#!/usr/bin/env bash
# Capacity test that runs ON the server host, against a docker compose
# deployment, with no browsers, no Node and no Python: static Go binaries
# (make load-test-host) and this script. See loadtest/README.md, "On the host".
#
# What it does, in order, and undoes whatever happens:
#   1. snapshot: pg_dump to a temp file, a count/md5 fingerprint of every table,
#      and the sequences
#   2. create the users loadtest-NNN (unusable password hash), a voice Talk and
#      a text channel
#   3. mint session tokens from JWT_SECRET into a 0600 temp file
#   4. run the scenarios one after the other with pauses, sampling the server
#      through /api/metrics and `docker stats`
#   5. clean up (also on Ctrl-C or an error): delete the channels and users, reset
#      the sequences, compare the fingerprint with the snapshot, remove the
#      tokens (and the dump, when the fingerprint is identical)
#
# Secrets (JWT_SECRET, METRICS_TOKEN, the tokens) are read into shell variables
# or a 0600 file, handed to the tools through their environment, and never
# printed. Do not run this script with `bash -x`.
#
# Usage:  loadtest/host-run.sh [--list]
#   SCENARIOS="chat100 chat300 chat1000 voice relay mixed"   (default; --list shows them;
#             chat<N> is a chat test with N sessions, chat50 or chat2000)
#   APP_CONTAINER=mnema-talk  DB_CONTAINER=mnema-postgres
#   BASE_URL=http://127.0.0.1:8080       default: the app container's published port
#   BIN_DIR=<dir with loadtest-mint, loadtest-chat, loadtest-voice>   default: ./bin
#   RESULT_DIR=./loadtest-results/<time>
#   PAUSE_S=30 (between scenarios)   ABORT_ON_FAIL=1 (stop at the first failed scenario)
#   JWT_SECRET, METRICS_TOKEN: read from the app container when unset
#   PGUSER, PGDATABASE: read from the app container's DATABASE_URL when unset
# Scenario knobs (defaults in brackets): CHAT_HOLD_S [120], CHAT_WRITER_PCT [10],
#   CHAT_RATE [0.2], CHAT_RAMP_PER_MIN [100], VOICE_HOLD_S [120], VOICE_MEMBERS [100],
#   RELAY_MEMBERS [100], RELAY_HOST [], MIXED_MEMBERS [100], MIXED_CAMERAS [10],
#   MIXED_SPEAKERS [3]; thresholds MAX_* / MIN_* are passed through to the tools.
# FINGERPRINT_SKIP="table1 table2": tables that change by themselves and are left
#   out of the comparison.
set -euo pipefail
umask 077

HERE="$(cd "$(dirname "$0")" && pwd)"
APP_CONTAINER="${APP_CONTAINER:-mnema-talk}"
DB_CONTAINER="${DB_CONTAINER:-mnema-postgres}"
BIN_DIR="${BIN_DIR:-$HERE/bin}"
SCENARIOS="${SCENARIOS:-chat100 chat300 chat1000 voice relay mixed}"
PAUSE_S="${PAUSE_S:-30}"
STATS_EVERY_S="${STATS_EVERY_S:-5}"

CHAT_HOLD_S="${CHAT_HOLD_S:-120}"
CHAT_WRITER_PCT="${CHAT_WRITER_PCT:-10}"
CHAT_RATE="${CHAT_RATE:-0.2}"
CHAT_RAMP_PER_MIN="${CHAT_RAMP_PER_MIN:-100}"
VOICE_HOLD_S="${VOICE_HOLD_S:-120}"
VOICE_MEMBERS="${VOICE_MEMBERS:-100}"
RELAY_MEMBERS="${RELAY_MEMBERS:-100}"
RELAY_HOST="${RELAY_HOST:-}"
MIXED_MEMBERS="${MIXED_MEMBERS:-100}"
MIXED_CAMERAS="${MIXED_CAMERAS:-10}"
MIXED_SPEAKERS="${MIXED_SPEAKERS:-3}"

say() { printf '%s\n' "$*"; }
die() { printf 'host-run: %s\n' "$*" >&2; exit 2; }

# -- scenarios ---------------------------------------------------------------------------
# scenario_kind and scenario_users give the tool and how many users it needs.
scenario_kind() {
  case "$1" in
    chat[1-9] | chat[1-9][0-9]*) echo chat ;; # chat<N>: N sessions
    voice | relay | mixed) echo voice ;;
    *) die "unknown scenario '$1' (chat<N> such as chat100, voice, relay, mixed)" ;;
  esac
}
scenario_users() {
  case "$1" in
    chat*) echo "${1#chat}" ;;
    voice) echo "$VOICE_MEMBERS" ;;
    relay) echo "$RELAY_MEMBERS" ;;
    mixed) echo "$MIXED_MEMBERS" ;;
  esac
}
# scenario_minutes estimates the run time: connecting, the hold, and the settle.
scenario_minutes() {
  case "$1" in
    chat*) local n; n="$(scenario_users "$1")"; echo $(( (n + CHAT_RAMP_PER_MIN - 1) / CHAT_RAMP_PER_MIN + (CHAT_HOLD_S + 30) / 60 + 1 )) ;;
    voice) echo $(( VOICE_MEMBERS * 3 / 600 + (VOICE_HOLD_S + 40) / 60 + 1 )) ;;
    relay) echo $(( RELAY_MEMBERS * 6 / 600 + (VOICE_HOLD_S + 40) / 60 + 1 )) ;;
    mixed) echo $(( MIXED_MEMBERS * 3 / 600 + (VOICE_HOLD_S + 40) / 60 + 1 )) ;;
  esac
}

if [ "${1:-}" = "--list" ]; then
  for s in chat100 chat300 chat1000 voice relay mixed; do
    printf '%-9s %-5s users %-5s about %s min\n' "$s" "$(scenario_kind "$s")" "$(scenario_users "$s")" "$(scenario_minutes "$s")"
  done
  exit 0
fi
[ "$#" -eq 0 ] || die "usage: $0 [--list]"

# Environment of the tool for one scenario (printed as KEY=VALUE lines).
scenario_env() {
  case "$1" in
    chat*)
      echo "SESSIONS=$(scenario_users "$1")"
      echo "WRITER_PCT=$CHAT_WRITER_PCT"
      echo "RATE=$CHAT_RATE"
      echo "HOLD_S=$CHAT_HOLD_S"
      echo "RAMP_PER_MIN=$CHAT_RAMP_PER_MIN"
      ;;
    voice)
      echo "TO=$VOICE_MEMBERS"
      echo "SCREEN_PUBLISHERS=1"
      echo "HOLD_S=$VOICE_HOLD_S"
      ;;
    relay)
      echo "TO=$RELAY_MEMBERS"
      echo "SCREEN_PUBLISHERS=1"
      echo "AUDIO_PUBLISHERS=3"
      echo "RELAY=1"
      echo "RELAY_HOST=$RELAY_HOST"
      echo "HOLD_S=$VOICE_HOLD_S"
      ;;
    mixed)
      echo "TO=$MIXED_MEMBERS"
      echo "SCREEN_PUBLISHERS=1"
      echo "CAMERA_PUBLISHERS=$MIXED_CAMERAS"
      echo "AUDIO_PUBLISHERS=$MIXED_SPEAKERS"
      echo "WATCH_CAMERAS=1"
      echo "HOLD_S=$VOICE_HOLD_S"
      ;;
  esac
}

# -- preflight ------------------------------------------------------------------------------
command -v docker >/dev/null || die "docker is required"
command -v curl >/dev/null || die "curl is required"
for b in loadtest-mint loadtest-chat loadtest-voice; do
  [ -x "$BIN_DIR/$b" ] || die "$BIN_DIR/$b not found (build with make load-test-host, see the README)"
done
for c in "$APP_CONTAINER" "$DB_CONTAINER"; do
  [ "$(docker inspect -f '{{.State.Running}}' "$c" 2>/dev/null || true)" = true ] || die "container '$c' is not running"
done

USERS=2
for s in $SCENARIOS; do
  scenario_kind "$s" >/dev/null
  n="$(scenario_users "$s")"
  [ "$n" -gt "$USERS" ] && USERS="$n"
done

# container_env prints one variable of a container's configuration (a value,
# never the whole environment).
container_env() {
  docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' "$1" | sed -n "s/^$2=//p" | head -n1
}

if [ -z "${BASE_URL:-}" ]; then
  mapping="$(docker port "$APP_CONTAINER" 8080/tcp 2>/dev/null | head -n1 || true)"
  [ -n "$mapping" ] || die "cannot find the published port of $APP_CONTAINER; set BASE_URL"
  host="${mapping%:*}"
  case "$host" in 0.0.0.0 | '::' | '[::]') host=127.0.0.1 ;; esac
  BASE_URL="http://$host:${mapping##*:}"
fi
curl -fsS --max-time 10 "$BASE_URL/api/health" >/dev/null || die "$BASE_URL/api/health does not answer"

[ -n "${JWT_SECRET:-}" ] || JWT_SECRET="$(container_env "$APP_CONTAINER" JWT_SECRET)"
[ -n "$JWT_SECRET" ] || die "JWT_SECRET is not in the container's environment; export it"
[ -n "${METRICS_TOKEN:-}" ] || METRICS_TOKEN="$(container_env "$APP_CONTAINER" METRICS_TOKEN)"
# ORIGIN is what the tools send as the page origin; the server compares it with
# its PUBLIC_URL (or CORS_ALLOWED_ORIGINS), also when BASE_URL is a loopback address.
if [ -z "${ORIGIN:-}" ]; then
  pub="$(container_env "$APP_CONTAINER" PUBLIC_URL)"
  [ -n "$pub" ] || pub="$(container_env "$APP_CONTAINER" CORS_ALLOWED_ORIGINS | cut -d, -f1)"
  ORIGIN="$(printf '%s' "$pub" | sed -E 's#^([a-z]+://[^/]+).*#\1#')"
  unset pub
fi
SESSION_COOKIE="${SESSION_COOKIE:-}"
if [ -z "$SESSION_COOKIE" ]; then
  case "$(container_env "$APP_CONTAINER" PUBLIC_URL)" in
    https://*) SESSION_COOKIE="__Host-mnema_session" ;;
    *) SESSION_COOKIE="mnema_session" ;;
  esac
fi
if [ -z "${PGUSER:-}" ] || [ -z "${PGDATABASE:-}" ]; then
  dburl="$(container_env "$APP_CONTAINER" DATABASE_URL)"
  [ -n "$dburl" ] || die "DATABASE_URL is not in the container's environment; set PGUSER and PGDATABASE"
  PGUSER="${PGUSER:-$(printf '%s' "$dburl" | sed -E 's#^[a-z]+://([^:@/]+).*#\1#')}"
  PGDATABASE="${PGDATABASE:-$(printf '%s' "$dburl" | sed -E 's#^[^@]*@[^/]*/([^?]*).*#\1#')}"
  unset dburl
fi

psql_db() { docker exec -i "$DB_CONTAINER" psql -U "$PGUSER" -d "$PGDATABASE" -v ON_ERROR_STOP=1 -X -At -F, "$@"; }
psql_db -c 'select 1' >/dev/null || die "cannot run psql in $DB_CONTAINER as $PGUSER on $PGDATABASE"
if [ "$(psql_db -c "select count(*) from users where username like 'loadtest-%'")" != 0 ]; then
  die "users named loadtest-* exist already (a run that was killed?). Delete them with: delete from users where username like 'loadtest-%'; and the channels named 'Loadtest%', then compare the fingerprint yourself"
fi

if [ "$(psql_db -c "select count(*) from channels where name in ('Loadtest', 'Loadtest Chat')")" != 0 ]; then
  die "channels named 'Loadtest' or 'Loadtest Chat' exist already; rename or delete them first (the script deletes its own channels by id)"
fi
# Outgoing webhooks and bots would receive the events of the test channels.
if [ "${ALLOW_WEBHOOKS:-0}" != 1 ] && [ "$(psql_db -c "select (select count(*) from outgoing_webhooks where enabled) + (select count(*) from bots)")" != 0 ]; then
  die "enabled outgoing webhooks or bots exist and would receive the test channels' events (real requests to their URLs); disable them or set ALLOW_WEBHOOKS=1"
fi

RESULT_DIR="${RESULT_DIR:-$PWD/loadtest-results/$(date +%Y%m%d-%H%M%S)}"
mkdir -p "$RESULT_DIR"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/mnema-loadtest.XXXXXX")"
chmod 700 "$WORK"
TOKENS="$WORK/tokens.json"

# -- cleanup (runs on every exit) ----------------------------------------------------------
CHILD_PIDS=()
SEEDED=0
VOICE_CHANNEL=""
TEXT_CHANNEL=""
CLEANED=0
INTERRUPTED=0
FINGERPRINT_OK=unknown

fingerprint() { # writes "table|count|md5" lines, sorted, to $1
  local skip="${FINGERPRINT_SKIP:-}" q
  q="$(psql_db -c "select string_agg(format('select %L as t, count(*)::text as n, md5(coalesce(string_agg(md5(x::text), %L order by md5(x::text)), %L)) as h from %I.%I x', tablename, '', '', schemaname, tablename), ' union all ' order by tablename) from pg_tables where schemaname = 'public'")"
  printf '%s\n' "$q" | psql_db -F'|' | sort >"$1"
  if [ -n "$skip" ]; then
    local t
    for t in $skip; do grep -v "^$t|" "$1" >"$1.tmp" || true; mv "$1.tmp" "$1"; done
  fi
}

sequences() { # writes "name|last_value" lines to $1
  psql_db -F'|' -c "select schemaname || '.' || sequencename, coalesce(last_value::text, '') from pg_sequences where schemaname = 'public' order by 1" >"$1"
}

reset_sequences() {
  local before="$WORK/seq.before" after="$WORK/seq.after" sql="$WORK/seq.sql" name old
  sequences "$after"
  : >"$sql"
  while IFS='|' read -r name old; do
    if [ "$(grep -F "$name|" "$after" | head -n1 | cut -d'|' -f2)" != "$old" ]; then
      if [ -z "$old" ]; then
        printf 'alter sequence %s restart;\n' "$name" >>"$sql"
      else
        printf "select setval('%s', %s, true);\n" "$name" "$old" >>"$sql"
      fi
    fi
  done <"$before"
  [ -s "$sql" ] && psql_db -q <"$sql" >/dev/null
  return 0
}

delete_loadtest_data() {
  # The channels are deleted by the ids they were created with.
  local CHANNEL_IDS="'00000000-0000-0000-0000-000000000000'"
  [ -z "$VOICE_CHANNEL" ] || CHANNEL_IDS="$CHANNEL_IDS, '$VOICE_CHANNEL'"
  [ -z "$TEXT_CHANNEL" ] || CHANNEL_IDS="$CHANNEL_IDS, '$TEXT_CHANNEL'"
  psql_db -q <<SQL >/dev/null
begin;
-- The audit log and the outgoing queue keep no foreign keys to the channels:
-- remove what the test users and channels caused.
delete from audit_log
 where actor_id in (select id from users where username like 'loadtest-%')
    or channel_id in ($CHANNEL_IDS);
delete from outgoing_queue where channel_id in ($CHANNEL_IDS);
delete from channels where id in ($CHANNEL_IDS);
delete from users where username like 'loadtest-%';
commit;
SQL
}

# stop_children asks the tools to stop (TERM), and kills what is still running
# after 5 s.
stop_children() {
  local p i
  for p in "${CHILD_PIDS[@]:-}"; do [ -n "$p" ] && kill "$p" 2>/dev/null; done
  for _ in 1 2 3 4 5; do
    local alive=0
    for p in "${CHILD_PIDS[@]:-}"; do [ -n "$p" ] && kill -0 "$p" 2>/dev/null && alive=1; done
    [ "$alive" = 0 ] && break
    sleep 1
  done
  for p in "${CHILD_PIDS[@]:-}"; do [ -n "$p" ] && kill -9 "$p" 2>/dev/null; done
  for p in "${CHILD_PIDS[@]:-}"; do [ -n "$p" ] && wait "$p" 2>/dev/null; done
  return 0
}

cleanup() {
  local status=$?
  trap - EXIT
  trap '' INT TERM # a second Ctrl-C must not leave users, channels and tokens behind
  set +e # every step below must run, whatever the one before did
  [ "$CLEANED" = 0 ] || exit "$status"
  CLEANED=1
  say "==> stopping the tools and cleaning up; please wait (Ctrl-C is ignored from here)"
  stop_children
  rm -f "$TOKENS"
  if [ "$SEEDED" = 1 ]; then
    say "==> cleaning up"
    sleep 5 # the server notices the closed connections
    if delete_loadtest_data && reset_sequences; then
      fingerprint "$WORK/fingerprint.after"
      if diff -q "$WORK/fingerprint.before" "$WORK/fingerprint.after" >/dev/null; then
        FINGERPRINT_OK=identical
        say "fingerprint of every table identical to the snapshot"
      else
        FINGERPRINT_OK=DIFFERENT
        say "FINGERPRINT DIFFERS. Tables that differ:"
        diff "$WORK/fingerprint.before" "$WORK/fingerprint.after" | sed -n 's/^[<>] \([^|]*\)|.*/  \1/p' | sort -u
        say "(a table that changes by itself, such as sessions or rate-limit rows, can differ legitimately; the dump is kept)"
      fi
    else
      FINGERPRINT_OK=CLEANUP-FAILED
      say "CLEANUP FAILED: delete the users loadtest-* and the channels 'Loadtest' and 'Loadtest Chat' by hand"
    fi
    cp "$WORK/fingerprint.before" "$RESULT_DIR/fingerprint.before" 2>/dev/null || true
    cp "$WORK/fingerprint.after" "$RESULT_DIR/fingerprint.after" 2>/dev/null || true
  fi
  if [ "$FINGERPRINT_OK" = identical ] || [ "$SEEDED" = 0 ]; then
    rm -rf "$WORK"
  else
    say "kept for you to inspect: $WORK (the dump db.dump restores with pg_restore; delete the directory afterwards)"
  fi
  write_summary "$status"
  [ "$FINGERPRINT_OK" = identical ] || [ "$SEEDED" = 0 ] || status=3
  [ "$INTERRUPTED" = 0 ] || status=130
  say "results: $RESULT_DIR"
  exit "$status"
}
interrupt() { INTERRUPTED=1; say "==> interrupted"; exit 130; }
trap cleanup EXIT
trap interrupt INT TERM

# -- summary file (no jq on the host: plain printf) ---------------------------------------
SCENARIO_ROWS=()
write_summary() {
  local status="$1" out="$RESULT_DIR/summary.json" first=1 row
  {
    printf '{"fingerprint":"%s","exit":%s,"scenarios":[' "$FINGERPRINT_OK" "$status"
    for row in "${SCENARIO_ROWS[@]:-}"; do
      [ -n "$row" ] || continue
      [ "$first" = 1 ] || printf ','
      first=0
      printf '%s' "$row"
    done
    printf ']}\n'
  } >"$out"
}

# -- 1. snapshot ----------------------------------------------------------------------------
say "==> snapshot (pg_dump, fingerprint, sequences)"
docker exec "$DB_CONTAINER" pg_dump -U "$PGUSER" -d "$PGDATABASE" -Fc >"$WORK/db.dump"
[ -s "$WORK/db.dump" ] || die "pg_dump produced nothing"
fingerprint "$WORK/fingerprint.before"
sequences "$WORK/seq.before"
say "    $(wc -l <"$WORK/fingerprint.before" | tr -d ' ') tables fingerprinted, dump $(wc -c <"$WORK/db.dump" | tr -d ' ') bytes"

# -- 2. users and channels --------------------------------------------------------------------
say "==> creating $USERS users and two channels"
SEEDED=1
psql_db -q <<SQL >/dev/null
insert into users (username, display_name, password_hash, rules_accepted_version)
select format('loadtest-%s', lpad(i::text, 4, '0')), format('Load %s', lpad(i::text, 4, '0')), '!loadtest-no-login',
       coalesce((select case when rules_text <> '' then rules_version else 0 end from instance_settings limit 1), 0)
from generate_series(0, $USERS - 1) i;
SQL
VOICE_CHANNEL="$(psql_db -q -c "insert into channels (category_id, name, type, sort_order) select id, 'Loadtest', 'voice', 99 from categories order by sort_order, id limit 1 returning id" | head -n1)"
TEXT_CHANNEL="$(psql_db -q -c "insert into channels (category_id, name, type, sort_order) select id, 'Loadtest Chat', 'text', 99 from categories order by sort_order, id limit 1 returning id" | head -n1)"
[ -n "$VOICE_CHANNEL" ] && [ -n "$TEXT_CHANNEL" ] || die "no category to put the channels in"

# -- 3. tokens ----------------------------------------------------------------------------------
psql_db -c "select id, token_version from users where username like 'loadtest-%' order by username" \
  | JWT_SECRET="$JWT_SECRET" OUT="$TOKENS" "$BIN_DIR/loadtest-mint"
[ -s "$TOKENS" ] || die "no tokens were minted"
chmod 600 "$TOKENS"

# -- 4. scenarios -------------------------------------------------------------------------------
sample_docker_stats() { # outfile
  while true; do
    docker stats --no-stream --format "$(date +%s)	{{.Name}}	{{.CPUPerc}}	{{.MemUsage}}" "$APP_CONTAINER" "$DB_CONTAINER" >>"$1" 2>/dev/null || true
    sleep "$STATS_EVERY_S"
  done
}

# docker_peak prints the highest CPU % and memory of a container in a stats file.
docker_peak() { # file container
  awk -F'\t' -v c="$2" '$2 == c {
      cpu = $3; sub(/%/, "", cpu); if (cpu + 0 > mc) mc = cpu + 0
      mem = $4; sub(/ \/.*/, "", mem); v = mem + 0
      if (mem ~ /GiB/) v *= 1024; else if (mem ~ /KiB/) v /= 1024; else if (mem ~ /B$/ && mem !~ /MiB/) v /= 1048576
      if (v > mm) mm = v
    } END { printf "%.0f%% CPU, %.0f MiB", mc, mm }' "$1"
}

run_scenario() {
  local name="$1" kind out log stats pid spid status line
  kind="$(scenario_kind "$name")"
  out="$RESULT_DIR/$name.json"
  log="$RESULT_DIR/$name.log"
  stats="$RESULT_DIR/$name.docker-stats.tsv"
  : >"$stats"
  say "==> scenario $name (about $(scenario_minutes "$name") min)"
  sample_docker_stats "$stats" &
  spid=$!
  CHILD_PIDS+=("$spid")
  local vars=(BASE_URL="$BASE_URL" TOKENS="$TOKENS" METRICS_TOKEN="$METRICS_TOKEN" SESSION_COOKIE="$SESSION_COOKIE" ORIGIN="$ORIGIN" OUT="$out" EVALUATE=1)
  while IFS= read -r line; do vars+=("$line"); done < <(scenario_env "$name")
  if [ "$kind" = chat ]; then
    vars+=(CHANNEL_ID="$TEXT_CHANNEL")
    env "${vars[@]}" "$BIN_DIR/loadtest-chat" >"$log" 2>&1 &
  else
    vars+=(CHANNEL_ID="$VOICE_CHANNEL")
    env "${vars[@]}" "$BIN_DIR/loadtest-voice" >"$log" 2>&1 &
  fi
  pid=$!
  CHILD_PIDS+=("$pid")
  status=0
  wait "$pid" || status=$?
  kill "$spid" 2>/dev/null || true
  wait "$spid" 2>/dev/null || true
  # The tool printed its own summary; show the part from the headline on.
  awk '/^=== /{p=1} /^viewers=/{p=1} p {print "    " $0}' "$log"
  say "    docker stats: $APP_CONTAINER $(docker_peak "$stats" "$APP_CONTAINER"); $DB_CONTAINER $(docker_peak "$stats" "$DB_CONTAINER")"
  local verdict=ok
  [ "$status" = 0 ] || verdict="failed($status)"
  SCENARIO_ROWS+=("{\"name\":\"$name\",\"exit\":$status,\"result\":\"$name.json\",\"log\":\"$name.log\",\"docker_stats\":\"$name.docker-stats.tsv\"}")
  say "    $name: $verdict"
  return "$status"
}

WORST=0
n=0
for s in $SCENARIOS; do
  n=$((n + 1))
  if [ "$n" -gt 1 ]; then
    say "==> pause ${PAUSE_S}s"
    sleep "$PAUSE_S"
  fi
  rc=0
  run_scenario "$s" || rc=$?
  [ "$rc" -le "$WORST" ] || WORST="$rc"
  if [ "$rc" -ne 0 ] && [ "${ABORT_ON_FAIL:-0}" = 1 ]; then
    say "==> stopping after the failed scenario $s"
    break
  fi
done

if [ "$WORST" -eq 0 ]; then say "==> all scenarios met their thresholds"; else say "==> at least one scenario failed its thresholds"; fi
exit "$WORST"
