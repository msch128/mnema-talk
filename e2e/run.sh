#!/usr/bin/env bash
# Browser smoke test: builds the real binary, starts throwaway Postgres and
# SeaweedFS, runs Playwright against it and tears everything down.
# Usage: e2e/run.sh   (or `make e2e`)
#   SKIP_BUILD=1          reuse web/dist and bin/mnema-talk-e2e
#   E2E_INSTALL_BROWSER=1 also run `playwright install` for E2E_BROWSERS
#                         (with --with-deps, except when CI is set: the workflow installs those)
#   E2E_BROWSERS=chromium browser(s) to install: chromium, webkit, firefox
#   E2E_ONLY=...          arguments for `playwright test`, e.g. "--project=webkit"
#   E2E_WEBRTC_ALL_INTERFACES=1 include VM/VPN interfaces in the browser ICE test
#   E2E_WEBRTC_UDP_MUX_PORT=0    test legacy per-peer ports instead of the shared UDP port
# Runs do not collide: the compose project is unique per run (mnema-e2e-<pid>) and
# the Postgres, S3, app and WebRTC UDP ports are free ports picked at start, so
# several runs (other worktrees, other agents) can go at the same time. Pin any
# of them for debugging: E2E_PROJECT, E2E_PG_PORT, E2E_S3_PORT, E2E_APP_PORT,
# E2E_UDP_PORT (first of a range of 51 UDP ports), E2E_LOG.
set -euo pipefail

cd "$(dirname "$0")/.."
ROOT="$PWD"

# A free port, asked from the kernel (TCP or UDP). Two calls in a row can in
# theory return the same port, so ports already chosen are skipped.
CHOSEN=" "
free_port() {
  local kind="$1" port
  for _ in $(seq 1 20); do
    port="$(python3 -c "import socket,sys; s=socket.socket(socket.AF_INET, socket.SOCK_STREAM if sys.argv[1]=='tcp' else socket.SOCK_DGRAM); s.bind(('127.0.0.1',0)); print(s.getsockname()[1])" "$kind")"
    case "$CHOSEN" in *" $port "*) continue ;; esac
    CHOSEN="$CHOSEN$port "
    echo "$port"
    return
  done
  echo "no free port" >&2
  exit 1
}

PG_PORT="${E2E_PG_PORT:-$(free_port tcp)}"
S3_PORT="${E2E_S3_PORT:-$(free_port tcp)}"
APP_PORT="${E2E_APP_PORT:-$(free_port tcp)}"
# WebRTC media: one shared UDP port plus a small range, kept below 65535.
UDP_MIN="${E2E_UDP_PORT:-$(free_port udp)}"
if [ "$UDP_MIN" -gt 65480 ]; then UDP_MIN=50100; fi
UDP_MAX=$((UDP_MIN + 50))
PROJECT="${E2E_PROJECT:-mnema-e2e-$$}"
BIN="$ROOT/bin/mnema-talk-e2e"
LOG="${E2E_LOG:-$ROOT/e2e/server.log}"
COMPOSE=(docker compose -p "$PROJECT" -f "$ROOT/e2e/docker-compose.yml")

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
  # MNEMA_E2E=1 enables the media key-lifetime test hook (web/src/lib/media/lifetime.ts); never set for releases.
  (cd web && npm ci --no-audit --no-fund && MNEMA_E2E=1 npm run build)
  echo "==> building server binary"
  mkdir -p "$(dirname "$BIN")"
  CGO_ENABLED=0 go build -o "$BIN" ./cmd/server
fi

echo "==> installing e2e dependencies"
(cd e2e && npm ci --no-audit --no-fund)
if [ "${E2E_INSTALL_BROWSER:-0}" = "1" ]; then
  # In CI the system packages are installed by a bounded workflow step first
  # (a slow apt mirror must not hang this script), so only fetch browsers.
  DEPS=(--with-deps)
  if [ -n "${CI:-}" ]; then DEPS=(); fi
  (cd e2e && npx playwright install ${DEPS[@]+"${DEPS[@]}"} ${E2E_BROWSERS:-chromium})
fi

echo "==> project $PROJECT: postgres :$PG_PORT, s3 :$S3_PORT, app :$APP_PORT, udp :$UDP_MIN-$UDP_MAX"
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
  WEBHOOK_ALLOW_PRIVATE_TARGETS=127.0.0.0/8 \
  LINK_PREVIEWS_ENABLED=false UPDATE_CHECK_ENABLED=false LOG_LEVEL="${E2E_LOG_LEVEL:-}" \
  WEBRTC_UDP_PORT_MIN="$UDP_MIN" WEBRTC_UDP_PORT_MAX="$UDP_MAX" \
  WEBRTC_UDP_MUX_PORT="${E2E_WEBRTC_UDP_MUX_PORT:-}" \
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
  npx playwright test ${E2E_ONLY:---project=chromium}
