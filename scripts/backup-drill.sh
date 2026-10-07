#!/usr/bin/env bash
# Backup/restore drill against a real compose stack: seeds a message and an
# upload, runs scripts/backup.sh, changes data afterwards, verifies the backup
# (restore.sh --verify), restores it (restore.sh --yes) and checks that the
# seeded message and file are back and the later change is gone.
# Usage: scripts/backup-drill.sh [image]   (default mnema-talk:local; `make backup-drill`)
#   DRILL_PORT=58190  host port for the app
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
IMAGE="${1:-mnema-talk:local}"
PORT="${DRILL_PORT:-58190}"
BASE="http://127.0.0.1:$PORT"
ADMIN_PASSWORD="drill-$(openssl rand -hex 8)"

# A throwaway deployment directory, laid out like a real one: compose file,
# scripts and .env. backup.sh refuses BACKUP_DIR inside a git work tree.
DEPLOY="$(mktemp -d)"
# Preflight failures may remove only this run's temporary directory.
trap 'rm -rf "$DEPLOY"' EXIT
COMPOSE_PROJECT_NAME="mnema-drill-$(openssl rand -hex 8)"
export COMPOSE_PROJECT_NAME
export COMPOSE_PROFILES=""
export COMPOSE_FILE="docker-compose.yml:isolated.yml"
export BACKUP_DIR="$DEPLOY/backups"
cp "$ROOT/docker-compose.yml" "$DEPLOY/"
cp -R "$ROOT/scripts" "$DEPLOY/scripts"
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
WEBRTC_UDP_PORT_MIN=58200
WEBRTC_UDP_PORT_MAX=58210
WEBRTC_NAT_1TO1_IP=127.0.0.1
EOF
chmod 600 "$DEPLOY/.env"
# Only namespace and published test ports differ from the production stack.
cat >"$DEPLOY/isolated.yml" <<EOF
services:
  app:
    container_name: $COMPOSE_PROJECT_NAME-app
    ports: !override
      - "127.0.0.1:$PORT:8080"
      - "127.0.0.1:58200-58210:58200-58210/udp"
  postgres:
    container_name: $COMPOSE_PROJECT_NAME-postgres
  seaweedfs:
    container_name: $COMPOSE_PROJECT_NAME-seaweedfs
  coturn:
    container_name: $COMPOSE_PROJECT_NAME-coturn
  updater:
    container_name: $COMPOSE_PROJECT_NAME-updater
networks:
  mnema-network:
    name: $COMPOSE_PROJECT_NAME-network
  mnema-updater:
    name: $COMPOSE_PROJECT_NAME-updater
EOF
cd "$DEPLOY"

# Refuse collisions before any cleanup that could remove Docker resources.
docker compose config --quiet
for kind in container network volume; do
  case "$kind" in
    container) names="$COMPOSE_PROJECT_NAME-app $COMPOSE_PROJECT_NAME-postgres $COMPOSE_PROJECT_NAME-seaweedfs $COMPOSE_PROJECT_NAME-coturn $COMPOSE_PROJECT_NAME-updater" ;;
    network) names="$COMPOSE_PROJECT_NAME-network $COMPOSE_PROJECT_NAME-updater" ;;
    volume) names="${COMPOSE_PROJECT_NAME}_postgres18-data ${COMPOSE_PROJECT_NAME}_postgres-data ${COMPOSE_PROJECT_NAME}_seaweedfs-data" ;;
  esac
  if [ "$kind" = container ]; then
    existing=$(docker container ls -a --format '{{.Names}}')
    project_resources=$(docker container ls -aq --filter "label=com.docker.compose.project=$COMPOSE_PROJECT_NAME")
  else
    existing=$(docker "$kind" ls --format '{{.Name}}')
    project_resources=$(docker "$kind" ls -q --filter "label=com.docker.compose.project=$COMPOSE_PROJECT_NAME")
  fi
  if [ -n "$project_resources" ]; then
    echo "refusing existing resources of project $COMPOSE_PROJECT_NAME" >&2
    exit 1
  fi
  for name in $names; do
    if [[ $'\n'"$existing"$'\n' == *$'\n'"$name"$'\n'* ]]; then
      echo "refusing existing $kind: $name" >&2
      rm -rf "$DEPLOY"
      exit 1
    fi
  done
done

cleanup() {
  status=$?
  if [ "$status" -ne 0 ]; then
    echo "--- app log (last 60 lines) ---" >&2
    docker compose logs --no-color --tail 60 app >&2 || true
  fi
  docker compose down -v --remove-orphans >/dev/null 2>&1 || true
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
post_message() {
  api -H 'Content-Type: application/json' --data "{\"content\":\"$1\"}" \
    "$BASE/api/channels/$CHANNEL/messages" >/dev/null
}
messages() { api "$BASE/api/channels/$CHANNEL/messages?limit=100" | jq -r '.. | .content? // empty'; }

echo "==> starting $IMAGE"
docker compose up -d --no-build --wait --wait-timeout 180
login
CHANNEL="$(api "$BASE/api/channels" | jq -r '[.categories[].channels[], .uncategorized[]] | map(select(.type == "text")) | first | .id')"
[ -n "$CHANNEL" ] && [ "$CHANNEL" != null ] || { echo "no text channel found" >&2; exit 1; }

echo "==> seeding a message and an upload"
BEFORE="before-backup-$(openssl rand -hex 4)"
AFTER="after-backup-$(openssl rand -hex 4)"
post_message "$BEFORE"
head -c 4096 /dev/urandom >"$DEPLOY/payload.bin"
MEDIA_URL="$(api -F "file=@$DEPLOY/payload.bin;type=application/octet-stream" -F 'content=drill upload' \
  "$BASE/api/channels/$CHANNEL/upload" | jq -r '.attachments[0].url')"
[ -n "$MEDIA_URL" ] && [ "$MEDIA_URL" != null ] || { echo "upload returned no attachment" >&2; exit 1; }

echo "==> backup"
scripts/backup.sh
SNAPSHOT="$(find "$BACKUP_DIR" -mindepth 1 -maxdepth 1 -type d -name '20*' | head -n 1)"
[ -f "$SNAPSHOT/COMPLETE" ] || { echo "backup is incomplete" >&2; exit 1; }

echo "==> changing data after the backup"
login
post_message "$AFTER"

echo "==> restore.sh --verify"
scripts/restore.sh "$SNAPSHOT" --verify

echo "==> restore.sh --yes"
scripts/restore.sh "$SNAPSHOT" --yes
docker compose up -d --no-build --wait --wait-timeout 180

echo "==> checking the restored data"
login
current="$(messages)"
grep -qx "$BEFORE" <<<"$current" || { echo "seeded message missing after restore" >&2; exit 1; }
if grep -qx "$AFTER" <<<"$current"; then echo "message from after the backup survived the restore" >&2; exit 1; fi
api "$BASE$MEDIA_URL" -o "$DEPLOY/restored.bin"
cmp -s "$DEPLOY/payload.bin" "$DEPLOY/restored.bin" || { echo "restored upload differs" >&2; exit 1; }

echo "==> backup drill passed"
