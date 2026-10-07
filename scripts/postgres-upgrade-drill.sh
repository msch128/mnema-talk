#!/usr/bin/env bash
# PostgreSQL 17 -> 18 upgrade drill: installs the stack the way versions
# before 0.5 ran it (PostgreSQL 17 on volume postgres-data), seeds a message
# and an upload, then switches to the current docker-compose.yml and checks
# that PostgreSQL refuses to start on an empty new database, that
# scripts/upgrade-postgres.sh moves the data over, and that a second run is
# refused.
# Usage: scripts/postgres-upgrade-drill.sh [image]   (default mnema-talk:local; `make postgres-upgrade-drill`)
#   DRILL_PORT=58290  host port for the app
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
IMAGE="${1:-mnema-talk:local}"
PORT="${DRILL_PORT:-58290}"
BASE="http://127.0.0.1:$PORT"
ADMIN_PASSWORD="drill-$(openssl rand -hex 8)"

DEPLOY="$(mktemp -d)"
export COMPOSE_PROJECT_NAME="mnema-pgdrill"
export BACKUP_DIR="$DEPLOY/backups"
cp "$ROOT/docker-compose.yml" "$DEPLOY/"
cp -R "$ROOT/scripts" "$DEPLOY/scripts"
# The PostgreSQL service as Mnema Talk 0.4 defined it.
cat >"$DEPLOY/postgres17.yml" <<'EOF'
services:
  postgres:
    image: postgres:17-alpine@sha256:b0f9560a2de083e2cc7382e75f808c7381a32852a7ec49117deedb300e552b24
    entrypoint: !reset null
    volumes: !override
      - postgres-data:/var/lib/postgresql/data
EOF
cat >"$DEPLOY/.env" <<EOF
MNEMA_IMAGE=$IMAGE
APP_ENV=production
PORT=$PORT
PUBLIC_URL=$BASE
JWT_SECRET=$(openssl rand -hex 32)
POSTGRES_PASSWORD=$(openssl rand -hex 16)
S3_ACCESS_KEY=drill$(openssl rand -hex 4)
S3_SECRET_KEY=$(openssl rand -hex 16)
ADMIN_USERNAME=Herzog
ADMIN_INITIAL_PASSWORD=$ADMIN_PASSWORD
UPDATE_CHECK_ENABLED=false
LINK_PREVIEWS_ENABLED=false
WEBRTC_UDP_PORT_MIN=58300
WEBRTC_UDP_PORT_MAX=58310
WEBRTC_NAT_1TO1_IP=127.0.0.1
EOF
chmod 600 "$DEPLOY/.env"
cd "$DEPLOY"

cleanup() {
  status=$?
  if [ "$status" -ne 0 ]; then
    echo "--- postgres log (last 30 lines) ---" >&2
    docker compose logs --no-color --tail 30 postgres >&2 || true
    echo "--- app log (last 30 lines) ---" >&2
    docker compose logs --no-color --tail 30 app >&2 || true
  fi
  docker compose down -v --remove-orphans >/dev/null 2>&1 || true
  # down -v skips volumes no service of the current file uses; the drill's
  # volumes all carry the project label.
  docker volume ls -q --filter "label=com.docker.compose.project=$COMPOSE_PROJECT_NAME" | xargs -r docker volume rm >/dev/null 2>&1 || true
  rm -rf "$DEPLOY"
  exit "$status"
}
trap cleanup EXIT

COOKIES="$DEPLOY/cookies"
login() {
  rm -f "$COOKIES"
  curl -fsS -c "$COOKIES" -H "Origin: $BASE" -H 'Content-Type: application/json' \
    --data "{\"username\":\"Herzog\",\"password\":\"$ADMIN_PASSWORD\"}" \
    "$BASE/api/auth/login" >/dev/null
}
api() { curl -fsS -b "$COOKIES" -H "Origin: $BASE" "$@"; }

if [ -n "$(docker volume ls -q --filter "label=com.docker.compose.project=$COMPOSE_PROJECT_NAME")" ]; then
  echo "volumes of project $COMPOSE_PROJECT_NAME already exist; remove them first" >&2
  exit 1
fi

echo "==> installing like 0.4: PostgreSQL 17 on volume postgres-data"
COMPOSE_FILE=docker-compose.yml:postgres17.yml docker compose up -d --no-build --wait --wait-timeout 180
login
CHANNEL="$(api "$BASE/api/channels" | jq -r '[.categories[].channels[], .uncategorized[]] | map(select(.type == "text")) | first | .id')"
MESSAGE="pg17-$(openssl rand -hex 4)"
api -H 'Content-Type: application/json' --data "{\"content\":\"$MESSAGE\"}" "$BASE/api/channels/$CHANNEL/messages" >/dev/null
head -c 4096 /dev/urandom >"$DEPLOY/payload.bin"
MEDIA_URL="$(api -F "file=@$DEPLOY/payload.bin;type=application/octet-stream" "$BASE/api/channels/$CHANNEL/upload" | jq -r '.attachments[0].url')"

echo "==> current compose without the upgrade: PostgreSQL must refuse to start"
# The container keeps exiting (and restarting); it must never become healthy
# and must say why. `up --wait` cannot judge a restart loop reliably.
docker compose up -d --no-build --no-deps postgres >/dev/null 2>&1 || true
hint=false
for _ in $(seq 1 20); do
  health=$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{end}}' mnema-postgres 2>/dev/null || true)
  [ "$health" != healthy ] || { echo "PostgreSQL 18 became healthy next to un-upgraded 17 data" >&2; exit 1; }
  if docker compose logs --no-color postgres 2>&1 | grep -q 'upgrade-postgres.sh'; then hint=true; fi
  sleep 1
done
[ "$hint" = true ] || { echo "no upgrade hint in the postgres log" >&2; exit 1; }

echo "==> scripts/upgrade-postgres.sh"
scripts/upgrade-postgres.sh

echo "==> checking the upgraded data"
docker compose exec -T postgres psql -U mnema -d mnema_talk -At -c 'SHOW server_version' </dev/null | grep -q '^18\.' || { echo "not on PostgreSQL 18" >&2; exit 1; }
login
api "$BASE/api/channels/$CHANNEL/messages?limit=100" | jq -r '.. | .content? // empty' | grep -qx "$MESSAGE" || { echo "message missing after the upgrade" >&2; exit 1; }
api "$BASE$MEDIA_URL" -o "$DEPLOY/restored.bin"
cmp -s "$DEPLOY/payload.bin" "$DEPLOY/restored.bin" || { echo "upload differs after the upgrade" >&2; exit 1; }

echo "==> a second run is refused"
if scripts/upgrade-postgres.sh >/dev/null 2>&1; then
  echo "upgrade ran twice" >&2
  exit 1
fi

echo "==> postgres upgrade drill passed"
