#!/usr/bin/env bash
# Upgrade drill: install the previous release from GHCR, fill it with realistic
# data through the API (users, channels, messages, an upload), then switch the
# stack to the freshly built image like an operator running
# `docker compose pull app && docker compose up -d app`, and check that the
# migrations apply, nothing is lost, sessions and logins still work and health
# is ok.
#
# Usage: scripts/upgrade-drill.sh [new-image]     (default mnema-talk:local; `make upgrade-drill`)
#   PREVIOUS_IMAGE=ghcr.io/msch128/mnema-talk:0.7.16   skip the lookup and use this image
#   DRILL_PORT=58192                                   host port for the app
#
# Which release is "previous": the newest released tag (git tags on origin, so a
# shallow checkout works) that is not newer than version.txt and does not point
# at the commit being tested, whose image can be pulled. Nothing in here is a
# real secret: every credential is generated per run.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
NEW_IMAGE="${1:-mnema-talk:local}"
PORT="${DRILL_PORT:-58192}"
BASE="http://127.0.0.1:$PORT"
REGISTRY_IMAGE="ghcr.io/msch128/mnema-talk"
ADMIN_PASSWORD="drill-$(openssl rand -hex 8)"
ALICE_PASSWORD="alice-$(openssl rand -hex 8)"
BOB_PASSWORD="bob-$(openssl rand -hex 8)"

# --- which release to upgrade from -------------------------------------------
resolve_previous() {
  local current head tags tag ver
  current="$(tr -d '[:space:]' <"$ROOT/version.txt")"
  head="$(git -C "$ROOT" rev-parse HEAD 2>/dev/null || true)"
  # "<sha> refs/tags/vX.Y.Z" and, for annotated tags, the peeled "<sha> refs/tags/vX.Y.Z^{}".
  tags="$(git -C "$ROOT" ls-remote --tags origin 'refs/tags/v*' 2>/dev/null || true)"
  if [ -z "$tags" ]; then
    tags="$(git -C "$ROOT" for-each-ref --format='%(objectname) %(refname)' 'refs/tags/v*' 2>/dev/null || true)"
  fi
  # Newest first, at most current, skipping the tag of the tested commit.
  while read -r ver; do
    [ -n "$ver" ] || continue
    if [ -n "$head" ] && grep -Eq "^$head[[:space:]]+refs/tags/v$ver(\^\{\})?\$" <<<"$tags"; then continue; fi
    if [ "$(printf '%s\n%s\n' "$ver" "$current" | sort -V | tail -n 1)" != "$current" ]; then continue; fi
    if docker pull --quiet "$REGISTRY_IMAGE:$ver" >/dev/null 2>&1; then
      echo "$REGISTRY_IMAGE:$ver"
      return 0
    fi
    echo "no image for $REGISTRY_IMAGE:$ver, trying the one before" >&2
  done < <(awk '{sub("refs/tags/v", "", $2); sub(/\^\{\}$/, "", $2); print $2}' <<<"$tags" |
    grep -E '^[0-9]+\.[0-9]+\.[0-9]+$' | sort -uV -r | head -n 8)
  return 1
}

if [ -n "${PREVIOUS_IMAGE:-}" ]; then
  OLD_IMAGE="$PREVIOUS_IMAGE"
else
  OLD_IMAGE="$(resolve_previous)" || { echo "could not find a released image to upgrade from" >&2; exit 1; }
fi
echo "==> upgrading from $OLD_IMAGE to $NEW_IMAGE"

# --- a throwaway deployment directory, laid out like a real one ---------------
DEPLOY="$(mktemp -d)"
trap 'rm -rf "$DEPLOY"' EXIT
COMPOSE_PROJECT_NAME="mnema-upgrade-$(openssl rand -hex 8)"
export COMPOSE_PROJECT_NAME
export COMPOSE_PROFILES=""
export COMPOSE_FILE="docker-compose.yml:isolated.yml"
cp "$ROOT/docker-compose.yml" "$DEPLOY/"
cat >"$DEPLOY/.env" <<EOF
MNEMA_IMAGE=$OLD_IMAGE
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
WEBRTC_UDP_PORT_MIN=58220
WEBRTC_UDP_PORT_MAX=58230
WEBRTC_NAT_1TO1_IP=127.0.0.1
EOF
chmod 600 "$DEPLOY/.env"
cat >"$DEPLOY/isolated.yml" <<EOF
services:
  app:
    container_name: $COMPOSE_PROJECT_NAME-app
    ports: !override
      - "127.0.0.1:$PORT:8080"
      - "127.0.0.1:58220-58230:58220-58230/udp"
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
if [ -n "$(docker container ls -aq --filter "label=com.docker.compose.project=$COMPOSE_PROJECT_NAME")" ]; then
  echo "refusing existing resources of project $COMPOSE_PROJECT_NAME" >&2
  exit 1
fi

cleanup() {
  status=$?
  if [ "$status" -ne 0 ]; then
    echo "--- app log (last 80 lines) ---" >&2
    docker compose logs --no-color --tail 80 app >&2 || true
  fi
  docker compose down -v --remove-orphans >/dev/null 2>&1 || true
  rm -rf "$DEPLOY"
  exit "$status"
}
trap cleanup EXIT

# --- API helpers: one cookie jar per person ------------------------------------
jar() { echo "$DEPLOY/cookies-$1"; }
login() { # name password
  rm -f "$(jar "$1")"
  curl -fsS -c "$(jar "$1")" -H "Origin: $BASE" -H 'Content-Type: application/json' \
    --data "{\"username\":\"$1\",\"password\":\"$2\"}" "$BASE/api/auth/login" >/dev/null
}
api() { local who="$1"; shift; curl -fsS -b "$(jar "$who")" -H "Origin: $BASE" "$@"; }
post_json() { local who="$1" path="$2" body="$3"; api "$who" -H 'Content-Type: application/json' --data "$body" "$BASE$path"; }
post_message() { post_json "$1" "/api/channels/$2/messages" "{\"content\":\"$3\"}" >/dev/null; }
messages() { api "$1" "$BASE/api/channels/$2/messages?limit=100" | jq -r '.. | .content? // empty'; }
health_version() { curl -fsS "$BASE/api/health" | jq -r '.version'; }

echo "==> starting the previous release"
docker compose up -d --no-build --wait --wait-timeout 240
OLD_VERSION="$(health_version)"
echo "previous release reports version $OLD_VERSION"

echo "==> seeding users, channels, messages and an upload"
login Herzog "$ADMIN_PASSWORD"
for person in alice bob; do
  code="$(post_json Herzog /api/admin/invites '{"max_uses":1}' | jq -r '.code')"
  [ -n "$code" ] && [ "$code" != null ] || { echo "no invite code for $person" >&2; exit 1; }
  pw="ALICE_PASSWORD"; [ "$person" = bob ] && pw="BOB_PASSWORD"
  post_json Herzog /api/auth/register "{\"username\":\"$person\",\"password\":\"${!pw}\",\"invite_code\":\"$code\"}" >/dev/null
done
login alice "$ALICE_PASSWORD"
login bob "$BOB_PASSWORD"

CATEGORY="$(post_json Herzog /api/admin/categories '{"name":"Projects"}' | jq -r '.id')"
GENERAL="$(api Herzog "$BASE/api/channels" | jq -r '[.categories[].channels[], .uncategorized[]] | map(select(.type == "text")) | first | .id')"
PROJECTS="$(post_json Herzog /api/admin/channels "{\"name\":\"upgrade-notes\",\"type\":\"text\",\"category_id\":\"$CATEGORY\"}" | jq -r '.id')"
VOICE="$(post_json Herzog /api/admin/channels '{"name":"hangout","type":"voice"}' | jq -r '.id')"
for id in "$CATEGORY" "$GENERAL" "$PROJECTS" "$VOICE"; do
  [ -n "$id" ] && [ "$id" != null ] || { echo "seeding a channel or category failed" >&2; exit 1; }
done

TAG="$(openssl rand -hex 4)"
post_message Herzog "$GENERAL" "welcome-$TAG"
post_message alice "$GENERAL" "alice-hello-$TAG"
post_message bob "$GENERAL" "bob-hello-$TAG"
post_message alice "$PROJECTS" "alice-plan-$TAG"
post_message bob "$PROJECTS" "bob-plan-$TAG"
for i in 1 2 3 4 5 6 7 8; do post_message alice "$GENERAL" "bulk-$i-$TAG"; done
head -c 262144 /dev/urandom >"$DEPLOY/payload.bin"
MEDIA_URL="$(api alice -F "file=@$DEPLOY/payload.bin;filename=notes.bin;type=application/octet-stream" -F "content=upload-$TAG" \
  "$BASE/api/channels/$PROJECTS/upload" | jq -r '.attachments[0].url')"
[ -n "$MEDIA_URL" ] && [ "$MEDIA_URL" != null ] || { echo "upload returned no attachment" >&2; exit 1; }
GENERAL_BEFORE="$(messages Herzog "$GENERAL" | sort)"
PROJECTS_BEFORE="$(messages Herzog "$PROJECTS" | sort)"
CHANNELS_BEFORE="$(api Herzog "$BASE/api/channels" | jq -S '[.categories[].channels[], .uncategorized[]] | map({id, name, type}) | sort_by(.id)')"

echo "==> switching the stack to $NEW_IMAGE"
sed -i.bak "s|^MNEMA_IMAGE=.*|MNEMA_IMAGE=$NEW_IMAGE|" .env && rm -f .env.bak
docker compose up -d --no-build --wait --wait-timeout 240

echo "==> health and migrations"
NEW_VERSION="$(health_version)"
[ -n "$NEW_VERSION" ] && [ "$NEW_VERSION" != null ] || { echo "health reports no version" >&2; exit 1; }
echo "now running $NEW_VERSION (was $OLD_VERSION)"
LATEST_MIGRATION="$(find "$ROOT/internal/db/migrations" -name '*.sql' -exec basename {} \; | sort | tail -n 1)"
# The old sessions still work: the secrets and the database are the same.
SYSTEM="$(api Herzog "$BASE/api/admin/system")"
[ "$(jq -r '.health.database.latest_migration' <<<"$SYSTEM")" = "$LATEST_MIGRATION" ] ||
  { echo "latest migration is not $LATEST_MIGRATION: $(jq -c '.health.database' <<<"$SYSTEM")" >&2; exit 1; }
[ "$(jq -r '.health.database.pending_migrations' <<<"$SYSTEM")" = 0 ] || { echo "migrations are pending" >&2; exit 1; }
[ "$(jq -r '.health.storage.reachable' <<<"$SYSTEM")" = true ] || { echo "storage is not reachable" >&2; exit 1; }
[ "$(jq -r '.version.current' <<<"$SYSTEM")" = "$NEW_VERSION" ] || { echo "admin and health disagree on the version" >&2; exit 1; }

echo "==> sessions from before the upgrade, and fresh logins"
for person in alice bob; do
  [ "$(api "$person" "$BASE/api/auth/me" | jq -r '.username // .user.username')" = "$person" ] ||
    { echo "$person's session did not survive the upgrade" >&2; exit 1; }
done
login Herzog "$ADMIN_PASSWORD"
login alice "$ALICE_PASSWORD"
login bob "$BOB_PASSWORD"

echo "==> data intact"
[ "$(messages alice "$GENERAL" | sort)" = "$GENERAL_BEFORE" ] || { echo "messages of #general changed" >&2; exit 1; }
[ "$(messages bob "$PROJECTS" | sort)" = "$PROJECTS_BEFORE" ] || { echo "messages of the project channel changed" >&2; exit 1; }
[ "$(api Herzog "$BASE/api/channels" | jq -S '[.categories[].channels[], .uncategorized[]] | map({id, name, type}) | sort_by(.id)')" = "$CHANNELS_BEFORE" ] ||
  { echo "the channel list changed" >&2; exit 1; }
api bob "$BASE$MEDIA_URL" -o "$DEPLOY/after.bin"
cmp -s "$DEPLOY/payload.bin" "$DEPLOY/after.bin" || { echo "the upload differs after the upgrade" >&2; exit 1; }

echo "==> the upgraded server takes new writes"
post_message bob "$GENERAL" "after-upgrade-$TAG"
messages alice "$GENERAL" | grep -qx "after-upgrade-$TAG" || { echo "a new message was not stored" >&2; exit 1; }

echo "==> upgrade drill passed ($OLD_VERSION -> $NEW_VERSION)"
