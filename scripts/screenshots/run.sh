#!/usr/bin/env bash
# Regenerates the README screenshots (doc/images/*.png) on a throwaway stack
# with invented demo data: builds the app, starts PostgreSQL and SeaweedFS on
# loopback ports, seeds users/channels/messages, captures with Playwright and
# tears everything down. Same pattern as e2e/run.sh.
#   SKIP_BUILD=1   reuse web/dist and bin/mnema-talk-shots
#   SHOTS_ONLY="chat roles"   capture only these (chat thread unread roles webhooks pins poll emoji-picker two-factor phone talk safety-code operations)
set -euo pipefail

cd "$(dirname "$0")/../.."
ROOT="$PWD"
PG_PORT="${SHOTS_PG_PORT:-55932}" S3_PORT="${SHOTS_S3_PORT:-58933}" APP_PORT="${SHOTS_APP_PORT:-58980}"
BIN="$ROOT/bin/mnema-talk-shots"
LOG="$(mktemp)"
COMPOSE=(docker compose -p mnema-shots -f "$ROOT/e2e/docker-compose.yml")
export E2E_PG_PORT="$PG_PORT" E2E_S3_PORT="$S3_PORT"
PASSWORD="shots-$(openssl rand -hex 8)"
SERVER_PID=""

cleanup() {
  status=$?
  if [ -n "$SERVER_PID" ]; then kill "$SERVER_PID" 2>/dev/null || true; wait "$SERVER_PID" 2>/dev/null || true; fi
  if [ "$status" -ne 0 ]; then tail -n 40 "$LOG" >&2 || true; fi
  "${COMPOSE[@]}" down -v --remove-orphans >/dev/null 2>&1 || true
  rm -f "$LOG"
  exit "$status"
}
trap cleanup EXIT

if [ "${SKIP_BUILD:-0}" != "1" ]; then
  (cd web && npm ci --no-audit --no-fund && npm run build)
  mkdir -p bin
  CGO_ENABLED=0 go build -ldflags="-X github.com/msch128/mnema-talk/internal/version.Version=$(cat version.txt)" -o "$BIN" ./cmd/server
fi
(cd e2e && npm ci --no-audit --no-fund)
"${COMPOSE[@]}" up -d --wait postgres seaweedfs

# env -i keeps a developer's .env/shell secrets out of the demo server.
env -i PATH="$PATH" HOME="$HOME" APP_ENV=development PORT="$APP_PORT" BIND_ADDR=127.0.0.1 \
  PUBLIC_URL="http://127.0.0.1:$APP_PORT" JWT_SECRET="$(openssl rand -hex 32)" \
  ADMIN_USERNAME=maren ADMIN_INITIAL_PASSWORD="$PASSWORD" \
  DATABASE_URL="postgres://mnema:mnema@127.0.0.1:$PG_PORT/mnema_e2e?sslmode=disable" \
  S3_ENDPOINT="http://127.0.0.1:$S3_PORT" S3_BUCKET=mnema-shots S3_ACCESS_KEY=shotskey1 S3_SECRET_KEY=shotssecret12 S3_FORCE_PATH_STYLE=true \
  WEBHOOK_ALLOW_PRIVATE_TARGETS=127.0.0.0/8 LINK_PREVIEWS_ENABLED=false UPDATE_CHECK_ENABLED=false \
  WEBRTC_UDP_PORT_MIN=50300 WEBRTC_UDP_PORT_MAX=50350 WEBRTC_NAT_1TO1_IP=127.0.0.1 \
  "$BIN" >"$LOG" 2>&1 &
SERVER_PID=$!
for _ in $(seq 1 60); do curl -fsS "http://127.0.0.1:$APP_PORT/api/health" >/dev/null 2>&1 && break; sleep 1; done

export SHOTS_BASE_URL="http://127.0.0.1:$APP_PORT" SHOTS_ADMIN_PASSWORD="$PASSWORD"
node scripts/screenshots/seed.mjs
# shellcheck disable=SC2086
node scripts/screenshots/capture.mjs ${SHOTS_ONLY:-}
