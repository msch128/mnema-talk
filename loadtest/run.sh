#!/usr/bin/env bash
# Load test: one member shares a screen in a Talk, real browsers and
# lightweight Pion viewers watch it, the server's memory and goroutines are
# sampled, and a report checks thresholds. See loadtest/README.md.
#
# Usage: loadtest/run.sh   (or `make load-test`)
#   PROFILE=smoke|full|join|reload|long   (default smoke)
#   SKIP_BUILD=1               reuse web/dist and bin/mnema-talk-loadtest
#   LOADTEST_INSTALL_BROWSER=1 also run `playwright install --with-deps chromium`
#   KEEP=1                     leave the throwaway stack running afterwards
#   LOADTEST_UDP_PORT=50200    the server's media port (+50 allowed); LOADTEST_APP_PORT,
#                              LOADTEST_PG_PORT and LOADTEST_S3_PORT move the other ports
# Remote mode (no infrastructure, no seeding; see the README for how to prepare
# an instance): BASE_URL, TOKENS (json file [{id, token}]), CHANNEL_ID,
# optionally CHANNEL_NAME (default Loadtest) and METRICS_TOKEN.
# Knobs: BROWSERS, PION, HOLD_S, MEMBERS, LOSS, JITTER_MS, BW_KBPS, BW_QUEUE_MS, QUALITY, SPACING_MS, WIDTH,
# FPS, BATCH, BATCH_MS, PER_BROWSER, SCENARIOS (watch join reload).
set -euo pipefail

cd "$(dirname "$0")/.."
ROOT="$PWD"

PROFILE="${PROFILE:-smoke}"
case "$PROFILE" in
  smoke)  : "${SCENARIOS:=watch}"; : "${BROWSERS:=3}";  : "${PION:=10}"; : "${HOLD_S:=30}" ;;
  full)   : "${SCENARIOS:=watch}"; : "${BROWSERS:=15}"; : "${PION:=84}"; : "${HOLD_S:=120}" ;;
  long)   : "${SCENARIOS:=watch}"; : "${BROWSERS:=15}"; : "${PION:=84}"; : "${HOLD_S:=7200}" ;;
  join)   : "${SCENARIOS:=join}";   : "${BROWSERS:=0}";  : "${PION:=0}";  : "${HOLD_S:=30}" ;;
  reload) : "${SCENARIOS:=reload}"; : "${BROWSERS:=0}";  : "${PION:=0}";  : "${HOLD_S:=30}" ;;
  *) echo "unknown PROFILE '$PROFILE' (smoke, full, join, reload, long)" >&2; exit 2 ;;
esac
MEMBERS="${MEMBERS:-6}"
LOSS="${LOSS:-0}"
QUALITY="${QUALITY:-auto}"
JITTER_MS="${JITTER_MS:-0}"
SPACING_MS="${SPACING_MS:-300}"
BATCH="${BATCH:-10}"
BATCH_MS="${BATCH_MS:-5000}"

RESULT_DIR="${RESULT_DIR:-$ROOT/loadtest/results/$(date +%Y%m%d-%H%M%S)-$PROFILE}"
mkdir -p "$RESULT_DIR"
export RESULT_DIR

REMOTE=0
[ -n "${BASE_URL:-}" ] && REMOTE=1

PG_PORT="${LOADTEST_PG_PORT:-56432}"
S3_PORT="${LOADTEST_S3_PORT:-59333}"
APP_PORT="${LOADTEST_APP_PORT:-58090}"
# The server's media port (a range of 50 above it is allowed); move it when another stack uses 50200.
UDP_PORT="${LOADTEST_UDP_PORT:-50200}"
BIN="$ROOT/bin/mnema-talk-loadtest"
VIEWER_BIN="$ROOT/bin/loadtest-viewer"
SERVER_LOG="$RESULT_DIR/server.log"
COMPOSE=(docker compose -p mnema-loadtest -f "$ROOT/e2e/docker-compose.yml")
export E2E_PG_PORT="$PG_PORT" E2E_S3_PORT="$S3_PORT"

SERVER_PID=""
SCRAPER_PID=""
SCENARIO_PIDS=()

cleanup() {
  status=$?
  trap - EXIT
  [ -n "$SCRAPER_PID" ] && kill "$SCRAPER_PID" 2>/dev/null || true
  for p in "${SCENARIO_PIDS[@]:-}"; do [ -n "$p" ] && kill "$p" 2>/dev/null || true; done
  if [ "$REMOTE" = 0 ] && [ "${KEEP:-0}" != 1 ]; then
    [ -n "$SERVER_PID" ] && { kill "$SERVER_PID" 2>/dev/null || true; wait "$SERVER_PID" 2>/dev/null || true; }
    "${COMPOSE[@]}" down -v --remove-orphans >/dev/null 2>&1 || true
    rm -f "$RESULT_DIR/tokens.json"
  fi
  if [ "$status" -ne 0 ] && [ -f "$SERVER_LOG" ]; then
    echo "--- server log (last 40 lines) ---" >&2
    tail -n 40 "$SERVER_LOG" >&2 || true
  fi
  echo "results: $RESULT_DIR"
  exit "$status"
}
trap cleanup EXIT

# -- how many users the scenarios need ----------------------------------------
USERS=2
for s in $SCENARIOS; do
  case "$s" in
    watch) n=$((1 + BROWSERS + PION)) ;;
    join) n="$MEMBERS" ;;
    reload) n=2 ;;
    *) echo "unknown scenario '$s'" >&2; exit 2 ;;
  esac
  [ "$n" -gt "$USERS" ] && USERS="$n"
done

# -- build --------------------------------------------------------------------
echo "==> building the load-test viewer"
mkdir -p "$ROOT/bin"
(cd loadtest/viewer && CGO_ENABLED=0 go build -o "$VIEWER_BIN" .)

echo "==> installing browser dependencies"
(cd e2e && npm ci --no-audit --no-fund)
if [ "${LOADTEST_INSTALL_BROWSER:-0}" = "1" ]; then
  (cd e2e && npx playwright install --with-deps chromium)
fi

if [ "$REMOTE" = 0 ]; then
  if [ "${SKIP_BUILD:-0}" != "1" ]; then
    echo "==> building web app and server binary"
    (cd web && npm ci --no-audit --no-fund && npm run build)
    CGO_ENABLED=0 go build -o "$BIN" ./cmd/server
  fi

  # Throwaway values, generated per run; nothing here is a real secret.
  JWT_SECRET="$(openssl rand -hex 32)"
  METRICS_TOKEN="$(openssl rand -hex 16)"
  ADMIN_PASSWORD="lt-$(openssl rand -hex 8)"
  S3_KEY="lt-$(openssl rand -hex 6)"
  S3_SECRET="lt-$(openssl rand -hex 12)"
  BASE_URL="http://127.0.0.1:$APP_PORT"
  CHANNEL_NAME="Loadtest"

  echo "==> starting postgres + seaweedfs"
  "${COMPOSE[@]}" up -d --wait postgres seaweedfs

  echo "==> starting server on $BASE_URL"
  # env -i keeps a developer's .env/shell secrets out of the test server.
  env -i PATH="$PATH" HOME="$HOME" \
    APP_ENV=development \
    PORT="$APP_PORT" BIND_ADDR=127.0.0.1 PUBLIC_URL="$BASE_URL" \
    JWT_SECRET="$JWT_SECRET" METRICS_TOKEN="$METRICS_TOKEN" \
    ADMIN_USERNAME=Herzog ADMIN_INITIAL_PASSWORD="$ADMIN_PASSWORD" \
    DATABASE_URL="postgres://mnema:mnema@127.0.0.1:$PG_PORT/mnema_e2e?sslmode=disable" \
    S3_ENDPOINT="http://127.0.0.1:$S3_PORT" S3_BUCKET=mnema-loadtest \
    S3_ACCESS_KEY="$S3_KEY" S3_SECRET_KEY="$S3_SECRET" S3_FORCE_PATH_STYLE=true \
    LINK_PREVIEWS_ENABLED=false UPDATE_CHECK_ENABLED=false LOG_LEVEL="${LOADTEST_LOG_LEVEL:-info}" \
    WEBRTC_UDP_PORT_MIN="$UDP_PORT" WEBRTC_UDP_PORT_MAX="$((UDP_PORT + 50))" WEBRTC_UDP_MUX_PORT="$UDP_PORT" WEBRTC_NAT_1TO1_IP=127.0.0.1 \
    "$BIN" >"$SERVER_LOG" 2>&1 &
  SERVER_PID=$!
  for _ in $(seq 1 60); do
    curl -fsS "$BASE_URL/api/health" >/dev/null 2>&1 && break
    kill -0 "$SERVER_PID" 2>/dev/null || { echo "server exited early" >&2; exit 1; }
    sleep 1
  done
  curl -fsS "$BASE_URL/api/health" >/dev/null

  echo "==> seeding $USERS users and a Talk"
  psql() { "${COMPOSE[@]}" exec -T postgres psql -U mnema -d mnema_e2e -v ON_ERROR_STOP=1 -At -F, "$@"; }
  psql -q <<SQL >/dev/null
insert into users (username, display_name, password_hash)
select format('loadtest-%s', lpad(i::text, 3, '0')), format('Load %s', lpad(i::text, 3, '0')), '!loadtest-no-login'
from generate_series(0, $USERS - 1) i;
insert into channels (category_id, name, type, sort_order)
select id, '$CHANNEL_NAME', 'voice', 99 from categories limit 1;
SQL
  CHANNEL_ID="$(psql -c "select id from channels where name = '$CHANNEL_NAME'" | head -n1)"
  TOKENS="$RESULT_DIR/tokens.json"
  psql -c "select id, token_version from users where username like 'loadtest-%' order by username" \
    | JWT_SECRET="$JWT_SECRET" node loadtest/mint.mjs >"$TOKENS"
else
  : "${TOKENS:?remote mode needs TOKENS}" "${CHANNEL_ID:?remote mode needs CHANNEL_ID}"
  CHANNEL_NAME="${CHANNEL_NAME:-Loadtest}"
  have="$(node -e 'console.log(JSON.parse(require("fs").readFileSync(process.argv[1], "utf8")).length)' "$TOKENS")"
  if [ "$have" -lt "$USERS" ]; then echo "TOKENS has $have entries, the profile needs $USERS" >&2; exit 2; fi
fi
export BASE_URL TOKENS CHANNEL_NAME CHANNEL_ID
SHARER_ID="$(node -e 'console.log(JSON.parse(require("fs").readFileSync(process.argv[1], "utf8"))[0].id)' "$TOKENS")"

# -- server metrics every 10 s (phase: pre, run, hold, post) ---------------------
HOLD_FILE="$RESULT_DIR/.hold"
rm -f "$HOLD_FILE" "$RESULT_DIR/.ready" "$RESULT_DIR/.stop"
METRICS_OUT="$RESULT_DIR/metrics.tsv"
sample() { # phase
  [ -n "${METRICS_TOKEN:-}" ] || return 0
  curl -fsS --max-time 5 -H "Authorization: Bearer $METRICS_TOKEN" "$BASE_URL/api/metrics" 2>/dev/null \
    | awk -v t="$(date +%s)" -v ph="$1" '/^go_goroutines /{g=$2} /^go_memstats_alloc_bytes /{a=$2} /^go_memstats_sys_bytes /{s=$2} END{if (g != "") printf "%s\t%s\t%s\t%s\t%s\n", t, ph, g, a, s}' \
    >>"$METRICS_OUT" || true
}
if [ -n "${METRICS_TOKEN:-}" ]; then
  : >"$METRICS_OUT"
  sample pre
  (
    while true; do
      sleep 10
      if [ -f "$HOLD_FILE" ]; then sample hold; else sample run; fi
    done
  ) &
  SCRAPER_PID=$!
fi

# -- scenarios ------------------------------------------------------------------
run_watch() {
  local vpid="" bpid=""
  BROWSERS="$BROWSERS" HOLD_S="$HOLD_S" BATCH="$BATCH" BATCH_MS="$BATCH_MS" \
    READY_FILE="$RESULT_DIR/.ready" HOLD_FILE="$HOLD_FILE" STOP_FILE="$RESULT_DIR/.stop" OUT="$RESULT_DIR/browsers.json" \
    node loadtest/browsers.mjs > >(tee "$RESULT_DIR/browsers.log") 2>&1 &
  bpid=$!
  SCENARIO_PIDS+=("$bpid")
  for _ in $(seq 1 180); do
    [ -f "$RESULT_DIR/.ready" ] && break
    kill -0 "$bpid" 2>/dev/null || break
    sleep 1
  done
  if [ "$PION" -gt 0 ] && [ -f "$RESULT_DIR/.ready" ]; then
    # The viewers outlast the browsers' joins so both overlap for HOLD_S.
    local join_s=$(( (BROWSERS / BATCH + 1) * (BATCH_MS / 1000 + 8) + 20 ))
    FROM=$((BROWSERS + 1)) TO=$((BROWSERS + 1 + PION)) HOLD_S=$((HOLD_S + join_s)) \
      SHARER_ID="$SHARER_ID" STOP_FILE="$RESULT_DIR/.stop" LOSS="$LOSS" QUALITY="$QUALITY" JITTER_MS="$JITTER_MS" BW_KBPS="${BW_KBPS:-0}" BW_QUEUE_MS="${BW_QUEUE_MS:-1500}" SPACING_MS="$SPACING_MS" \
      OUT="$RESULT_DIR/viewers.json" "$VIEWER_BIN" > >(tee "$RESULT_DIR/viewers.log") 2>&1 &
    vpid=$!
    SCENARIO_PIDS+=("$vpid")
  fi
  wait "$bpid" || echo "browser scenario failed" >&2
  [ -n "$vpid" ] && { wait "$vpid" || echo "viewer failed" >&2; }
  return 0
}
run_join() {
  for mode in seq parallel desc; do
    echo "==> join scenario: $mode"
    local m="$mode" d=0
    [ "$mode" = desc ] && { m=seq; d=1; }
    MODE="$m" DESC="$d" MEMBERS="$MEMBERS" HOLD_S="$HOLD_S" GAP_MS="${GAP_MS:-8000}" \
      OUT="$RESULT_DIR/join-$mode.json" node loadtest/join.mjs 2>&1 | tee "$RESULT_DIR/join-$mode.log" || true
    sleep 3
  done
}
run_reload() {
  MEMBERS=2 OUT="$RESULT_DIR/reload.json" node loadtest/reload.mjs 2>&1 | tee "$RESULT_DIR/reload.log" || true
}

for s in $SCENARIOS; do
  echo "==> scenario: $s (profile $PROFILE)"
  "run_$s"
done

# -- report -----------------------------------------------------------------------
if [ -n "$SCRAPER_PID" ]; then kill "$SCRAPER_PID" 2>/dev/null || true; SCRAPER_PID=""; fi
sleep 5 # let the server finish tearing down the leavers
sample post
node loadtest/report.mjs
