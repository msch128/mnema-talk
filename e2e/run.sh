#!/usr/bin/env bash
# Browser smoke test: builds the real binary, starts throwaway Postgres and
# SeaweedFS, runs Playwright against it and tears everything down.
# Usage: e2e/run.sh   (or `make e2e`)
#   SKIP_BUILD=1          reuse web/dist and bin/mnema-talk-e2e
#   E2E_INSTALL_BROWSER=1 also run `playwright install --with-deps chromium`
set -euo pipefail

cd "$(dirname "$0")/.."
ROOT="$PWD"

PG_PORT="${E2E_PG_PORT:-55432}"
S3_PORT="${E2E_S3_PORT:-58333}"
APP_PORT="${E2E_APP_PORT:-58080}"
BIN="$ROOT/bin/mnema-talk-e2e"
LOG="$ROOT/e2e/server.log"
COMPOSE=(docker compose -p mnema-e2e -f "$ROOT/e2e/docker-compose.yml")

# Throwaway values, generated per run; nothing here is a real secret.
JWT_SECRET="$(openssl rand -hex 32)"
ADMIN_PASSWORD="e2e-$(openssl rand -hex 8)"
S3_KEY="e2e-$(openssl rand -hex 6)"
S3_SECRET="e2e-$(openssl rand -hex 12)"

export E2E_PG_PORT="$PG_PORT" E2E_S3_PORT="$S3_PORT"
SERVER_PID=""

cleanup() {
  status=$?
  if [ -n "$SERVER_PID" ]; then
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
  fi
  if [ "$status" -ne 0 ] && [ -f "$LOG" ]; then
    echo "--- server log (last 60 lines) ---" >&2
    tail -n 60 "$LOG" >&2 || true
  fi
  "${COMPOSE[@]}" down -v --remove-orphans >/dev/null 2>&1 || true
  exit "$status"
}
trap cleanup EXIT

if [ "${SKIP_BUILD:-0}" != "1" ]; then
  echo "==> building web app"
  (cd web && npm ci --no-audit --no-fund && npm run build)
  echo "==> building server binary"
  mkdir -p "$(dirname "$BIN")"
  CGO_ENABLED=0 go build -o "$BIN" ./cmd/server
fi

echo "==> installing e2e dependencies"
(cd e2e && npm ci --no-audit --no-fund)
if [ "${E2E_INSTALL_BROWSER:-0}" = "1" ]; then
  (cd e2e && npx playwright install --with-deps chromium)
fi

echo "==> starting postgres + seaweedfs"
"${COMPOSE[@]}" up -d --wait postgres seaweedfs

echo "==> starting server on 127.0.0.1:$APP_PORT"
# env -i keeps a developer's .env/shell secrets out of the test server.
env -i PATH="$PATH" HOME="$HOME" \
  APP_ENV=development \
  PORT="$APP_PORT" BIND_ADDR=127.0.0.1 \
  PUBLIC_URL="http://127.0.0.1:$APP_PORT" \
  JWT_SECRET="$JWT_SECRET" \
  ADMIN_USERNAME=Herzog ADMIN_INITIAL_PASSWORD="$ADMIN_PASSWORD" \
  DATABASE_URL="postgres://mnema:mnema@127.0.0.1:$PG_PORT/mnema_e2e?sslmode=disable" \
  S3_ENDPOINT="http://127.0.0.1:$S3_PORT" S3_BUCKET=mnema-e2e \
  S3_ACCESS_KEY="$S3_KEY" S3_SECRET_KEY="$S3_SECRET" S3_FORCE_PATH_STYLE=true \
  LINK_PREVIEWS_ENABLED=false LOG_LEVEL="${E2E_LOG_LEVEL:-}" \
  WEBRTC_UDP_PORT_MIN=50100 WEBRTC_UDP_PORT_MAX=50150 \
  WEBRTC_NAT_1TO1_IP=127.0.0.1 \
  "$BIN" >"$LOG" 2>&1 &
SERVER_PID=$!

for _ in $(seq 1 60); do
  if curl -fsS "http://127.0.0.1:$APP_PORT/api/health" >/dev/null 2>&1; then break; fi
  if ! kill -0 "$SERVER_PID" 2>/dev/null; then echo "server exited early" >&2; exit 1; fi
  sleep 1
done
curl -fsS "http://127.0.0.1:$APP_PORT/api/health" >/dev/null

echo "==> running playwright"
cd e2e
E2E_BASE_URL="http://127.0.0.1:$APP_PORT" E2E_ADMIN_USER=Herzog E2E_ADMIN_PASSWORD="$ADMIN_PASSWORD" \
  npx playwright test ${E2E_ONLY:-}
