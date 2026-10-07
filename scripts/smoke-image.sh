#!/usr/bin/env bash
# Image smoke test: starts the production docker-compose.yml (app, postgres,
# SeaweedFS) with a given image and throwaway secrets, then checks the
# healthcheck, /api/health, an admin login and the session, a 12 MB upload
# round trip (streamed to SeaweedFS as a multipart upload), and tears
# everything down again. Exercises the image, its entrypoint and the compose
# hardening (read-only root, cap_drop, tmpfs, memory limits) together.
# Usage: scripts/smoke-image.sh [image]   (default mnema-talk:local; `make smoke`)
#   SMOKE_PORT=58090  host port for the app
set -euo pipefail

cd "$(dirname "$0")/.."

IMAGE="${1:-mnema-talk:local}"
PORT="${SMOKE_PORT:-58090}"
PROJECT="mnema-smoke"
ENV_FILE="$(mktemp)"
COOKIES="$(mktemp)"
PAYLOAD="$(mktemp)"
RESTORED="$(mktemp)"
ADMIN_PASSWORD="smoke-$(openssl rand -hex 8)"
BASE="http://127.0.0.1:$PORT"

# Throwaway values, generated per run; nothing here is a real secret. The
# production compose file is used unchanged; only the env differs.
cat >"$ENV_FILE" <<EOF
MNEMA_IMAGE=$IMAGE
APP_ENV=production
PORT=$PORT
PUBLIC_URL=$BASE
JWT_SECRET=$(openssl rand -hex 32)
POSTGRES_PASSWORD=$(openssl rand -hex 16)
S3_ACCESS_KEY=smoke$(openssl rand -hex 4)
S3_SECRET_KEY=$(openssl rand -hex 16)
ADMIN_USERNAME=Herzog
ADMIN_INITIAL_PASSWORD=$ADMIN_PASSWORD
UPDATE_CHECK_ENABLED=false
LINK_PREVIEWS_ENABLED=false
WEBRTC_UDP_PORT_MIN=58100
WEBRTC_UDP_PORT_MAX=58110
WEBRTC_NAT_1TO1_IP=127.0.0.1
EOF

COMPOSE=(docker compose -p "$PROJECT" --env-file "$ENV_FILE" -f docker-compose.yml)

cleanup() {
  status=$?
  if [ "$status" -ne 0 ]; then
    echo "--- app log (last 60 lines) ---" >&2
    "${COMPOSE[@]}" logs --no-color --tail 60 app >&2 || true
  fi
  "${COMPOSE[@]}" down -v --remove-orphans >/dev/null 2>&1 || true
  rm -f "$ENV_FILE" "$COOKIES" "$PAYLOAD" "$RESTORED"
  exit "$status"
}
trap cleanup EXIT

echo "==> starting $IMAGE with postgres + seaweedfs"
# --wait returns once every service, the app included, reports healthy.
"${COMPOSE[@]}" up -d --no-build --wait --wait-timeout 180

echo "==> /api/health"
curl -fsS "$BASE/api/health"
echo

echo "==> admin login"
# Same-origin request: the CSRF check needs a matching Origin header.
curl -fsS -c "$COOKIES" -H "Origin: $BASE" -H 'Content-Type: application/json' \
  --data "{\"username\":\"Herzog\",\"password\":\"$ADMIN_PASSWORD\"}" \
  "$BASE/api/auth/login" >/dev/null
me="$(curl -fsS -b "$COOKIES" "$BASE/api/auth/me")"
case "$me" in
  *'"role":"admin"'*) ;;
  *) echo "unexpected /api/auth/me: $me" >&2; exit 1 ;;
esac

echo "==> 12 MB upload round trip"
CHANNEL="$(curl -fsS -b "$COOKIES" "$BASE/api/channels" | jq -r '[.categories[].channels[], .uncategorized[]] | map(select(.type == "text")) | first | .id')"
head -c $((12 << 20)) /dev/urandom >"$PAYLOAD"
MEDIA_URL="$(curl -fsS -b "$COOKIES" -H "Origin: $BASE" \
  -F "file=@$PAYLOAD;filename=big.bin;type=application/octet-stream" \
  "$BASE/api/channels/$CHANNEL/upload" | jq -r '.attachments[0].url')"
curl -fsS -b "$COOKIES" "$BASE$MEDIA_URL" -o "$RESTORED"
cmp -s "$PAYLOAD" "$RESTORED" || { echo "downloaded file differs from the upload" >&2; exit 1; }

echo "==> orphan scan lists the bucket"
orphans="$(curl -fsS -b "$COOKIES" "$BASE/api/admin/media/orphans" | jq -r '.count')"
[ "$orphans" = 0 ] || { echo "unexpected orphans: $orphans" >&2; exit 1; }

echo "==> smoke test passed"
