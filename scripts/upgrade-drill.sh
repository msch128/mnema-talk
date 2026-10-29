#!/usr/bin/env bash
# Upgrade drill: install a released image from GHCR, fill it with realistic
# data through the API (users, channels, messages, an upload, roles and
# overwrites, a webhook, a bot, notification levels and, from 0.9.0 on,
# onboarding and role mentions), then switch the stack to the freshly built
# image like an operator running `docker compose pull app && docker compose up
# -d app`, and check that the migrations apply, row counts match, every seeded
# object reads back through the API, sessions and logins still work and health
# is ok.
#
# Usage: scripts/upgrade-drill.sh [new-image]     (default mnema-talk:local; `make upgrade-drill`)
#   FROM_VERSIONS="0.8.16 0.9.0"   released versions to upgrade from, one leg each,
#                                  run one after the other (default: that list).
#                                  CI runs one leg per job with FROM_VERSIONS=<one>.
#   PREVIOUS_IMAGE=ghcr.io/msch128/mnema-talk:0.9.0   one leg from exactly this
#                                  image; FROM_VERSION names its version (default:
#                                  the tag after the last colon)
#   DRILL_PORT=<port>              host port for the app (default: a free one)
#   DRILL_PULL_ATTEMPTS / DRILL_PULL_DELAY   bounded registry retry (4 / 15s)
#
# A registry outage fails the drill after the retries; it is never skipped.
# Nothing in here is a real secret: every credential is generated per run.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
# shellcheck source=scripts/drill-common.sh
source "$ROOT/scripts/drill-common.sh"
# shellcheck source=scripts/free-port.sh
source "$ROOT/scripts/free-port.sh"
# shellcheck source=scripts/upgrade-drill-data.sh
source "$ROOT/scripts/upgrade-drill-data.sh"
NEW_IMAGE="${1:-mnema-talk:local}"

# --- several versions: one leg each, in a child process, then a summary --------
if [ -z "${PREVIOUS_IMAGE:-}" ] && [ -z "${DRILL_LEG:-}" ]; then
  VERSIONS="$(drill_resolve_versions "${FROM_VERSIONS:-}")" || exit 1
  failed='' summary=''
  for v in $VERSIONS; do
    started=$SECONDS
    if DRILL_LEG=1 FROM_VERSION="$v" "$0" "$NEW_IMAGE"; then result=passed; else result=FAILED; failed="$failed $v"; fi
    summary="$summary$v $result in $((SECONDS - started))s"$'\n'
  done
  printf 'upgrade drill:\n%s' "$summary"
  [ -z "$failed" ] || { echo "upgrade drill failed from:$failed" >&2; exit 1; }
  exit 0
fi

PORT="${DRILL_PORT:-$(free_port)}"
UDP_MIN="${DRILL_UDP_PORT:-$(free_port_range 11)}"
UDP_MAX=$((UDP_MIN + 10))
BASE="http://127.0.0.1:$PORT"
ADMIN_PASSWORD="drill-$(openssl rand -hex 8)"
ALICE_PASSWORD="alice-$(openssl rand -hex 8)"
BOB_PASSWORD="bob-$(openssl rand -hex 8)"

# --- which release to upgrade from -------------------------------------------
if [ -n "${PREVIOUS_IMAGE:-}" ]; then
  OLD_IMAGE="$PREVIOUS_IMAGE"
  FROM_VERSION="${FROM_VERSION:-${PREVIOUS_IMAGE##*:}}"
else
  FROM_VERSION="$(drill_resolve_versions "${FROM_VERSION:-}")" || exit 1
  case "$FROM_VERSION" in *$'\n'*) echo "a leg takes exactly one FROM_VERSION" >&2; exit 1 ;; esac
  OLD_IMAGE="$REGISTRY_IMAGE:$FROM_VERSION"
fi
drill_semver_ok "$FROM_VERSION" || { echo "cannot tell the version of $OLD_IMAGE; set FROM_VERSION" >&2; exit 1; }
drill_pull_image "$OLD_IMAGE"
echo "==> upgrading from $OLD_IMAGE to $NEW_IMAGE"
LEG_STARTED=$SECONDS

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
ADMIN_USERNAME=cool_guy
ADMIN_INITIAL_PASSWORD=$ADMIN_PASSWORD
UPDATE_CHECK_ENABLED=false
LINK_PREVIEWS_ENABLED=false
WEBRTC_UDP_PORT_MIN=$UDP_MIN
WEBRTC_UDP_PORT_MAX=$UDP_MAX
WEBRTC_NAT_1TO1_IP=127.0.0.1
EOF
chmod 600 "$DEPLOY/.env"
cat >"$DEPLOY/isolated.yml" <<EOF
services:
  app:
    container_name: $COMPOSE_PROJECT_NAME-app
    ports: !override
      - "127.0.0.1:$PORT:8080"
      - "127.0.0.1:$UDP_MIN-$UDP_MAX:$UDP_MIN-$UDP_MAX/udp"
  postgres:
    container_name: $COMPOSE_PROJECT_NAME-postgres
  seaweedfs:
    container_name: $COMPOSE_PROJECT_NAME-seaweedfs
  coturn:
    container_name: $COMPOSE_PROJECT_NAME-coturn
  updater:
    container_name: $COMPOSE_PROJECT_NAME-updater
  recreator:
    container_name: $COMPOSE_PROJECT_NAME-recreator
networks:
  mnema-network:
    name: $COMPOSE_PROJECT_NAME-network
  mnema-updater:
    name: $COMPOSE_PROJECT_NAME-updater
  mnema-recreator:
    name: $COMPOSE_PROJECT_NAME-recreator
  mnema-updater-egress:
    name: $COMPOSE_PROJECT_NAME-updater-egress
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
put_json() { local who="$1" path="$2" body="$3"; api "$who" -X PUT -H 'Content-Type: application/json' --data "$body" "$BASE$path"; }
post_message() { post_json "$1" "/api/channels/$2/messages" "{\"content\":\"$3\"}" >/dev/null; }
messages() { api "$1" "$BASE/api/channels/$2/messages?limit=100" | jq -r '.. | .content? // empty'; }
health_version() { curl -fsS "$BASE/api/health" | jq -r '.version'; }

echo "==> starting the previous release"
docker compose up -d --no-build --wait --wait-timeout 240
OLD_VERSION="$(health_version)"
echo "previous release reports version $OLD_VERSION"

echo "==> seeding users, channels, messages and an upload"
login cool_guy "$ADMIN_PASSWORD"
for person in alice bob; do
  code="$(post_json cool_guy /api/admin/invites '{"max_uses":1}' | jq -r '.code')"
  [ -n "$code" ] && [ "$code" != null ] || { echo "no invite code for $person" >&2; exit 1; }
  pw="ALICE_PASSWORD"; [ "$person" = bob ] && pw="BOB_PASSWORD"
  post_json cool_guy /api/auth/register "{\"username\":\"$person\",\"password\":\"${!pw}\",\"invite_code\":\"$code\"}" >/dev/null
done
login alice "$ALICE_PASSWORD"
login bob "$BOB_PASSWORD"

CATEGORY="$(post_json cool_guy /api/admin/categories '{"name":"Projects"}' | jq -r '.id')"
GENERAL="$(api cool_guy "$BASE/api/channels" | jq -r '[.categories[].channels[], .uncategorized[]] | map(select(.type == "text")) | first | .id')"
PROJECTS="$(post_json cool_guy /api/admin/channels "{\"name\":\"upgrade-notes\",\"type\":\"text\",\"category_id\":\"$CATEGORY\"}" | jq -r '.id')"
VOICE="$(post_json cool_guy /api/admin/channels '{"name":"hangout","type":"voice"}' | jq -r '.id')"
for id in "$CATEGORY" "$GENERAL" "$PROJECTS" "$VOICE"; do
  [ -n "$id" ] && [ "$id" != null ] || { echo "seeding a channel or category failed" >&2; exit 1; }
done

TAG="$(openssl rand -hex 4)"
post_message cool_guy "$GENERAL" "welcome-$TAG"
post_message alice "$GENERAL" "alice-hello-$TAG"
post_message bob "$GENERAL" "bob-hello-$TAG"
post_message alice "$PROJECTS" "alice-plan-$TAG"
post_message bob "$PROJECTS" "bob-plan-$TAG"
for i in 1 2 3 4 5 6 7 8; do post_message alice "$GENERAL" "bulk-$i-$TAG"; done
# Deterministic binary content (sniffs as application/octet-stream); random
# bytes occasionally match a disallowed signature and the upload got a 415.
{ printf 'MNEMA-DRILL\000'; head -c 262132 /dev/zero | LC_ALL=C tr '\000' '\001'; } >"$DEPLOY/payload.bin"
MEDIA_URL="$(api alice -F "file=@$DEPLOY/payload.bin;filename=notes.bin;type=application/octet-stream" -F "content=upload-$TAG" \
  "$BASE/api/channels/$PROJECTS/upload" | jq -r '.attachments[0].url')"
[ -n "$MEDIA_URL" ] && [ "$MEDIA_URL" != null ] || { echo "upload returned no attachment" >&2; exit 1; }
echo "==> seeding roles, overwrites, a webhook, a bot and notification settings (from $FROM_VERSION)"
seed_integration_data
GENERAL_BEFORE="$(messages cool_guy "$GENERAL" | sort)"
PROJECTS_BEFORE="$(messages cool_guy "$PROJECTS" | sort)"
CHANNELS_BEFORE="$(api cool_guy "$BASE/api/channels" | jq -S '[.categories[].channels[], .uncategorized[]] | map({id, name, type}) | sort_by(.id)')"

echo "==> switching the stack to $NEW_IMAGE"
sed -i.bak "s|^MNEMA_IMAGE=.*|MNEMA_IMAGE=$NEW_IMAGE|" .env && rm -f .env.bak
docker compose up -d --no-build --wait --wait-timeout 240

echo "==> health and migrations"
NEW_VERSION="$(health_version)"
[ -n "$NEW_VERSION" ] && [ "$NEW_VERSION" != null ] || { echo "health reports no version" >&2; exit 1; }
echo "now running $NEW_VERSION (was $OLD_VERSION)"
LATEST_MIGRATION="$(find "$ROOT/internal/db/migrations" -name '*.sql' -exec basename {} \; | sort | tail -n 1)"
# The old sessions still work: the secrets and the database are the same.
SYSTEM="$(api cool_guy "$BASE/api/admin/system")"
[ "$(jq -r '.health.database.latest_migration' <<<"$SYSTEM")" = "$LATEST_MIGRATION" ] ||
  { echo "latest migration is not $LATEST_MIGRATION: $(jq -c '.health.database' <<<"$SYSTEM")" >&2; exit 1; }
[ "$(jq -r '.health.database.pending_migrations' <<<"$SYSTEM")" = 0 ] || { echo "migrations are pending" >&2; exit 1; }
[ "$(jq -r '.health.storage.reachable' <<<"$SYSTEM")" = true ] || { echo "storage is not reachable" >&2; exit 1; }
[ "$(jq -r '.version.current' <<<"$SYSTEM")" = "$NEW_VERSION" ] || { echo "admin and health disagree on the version" >&2; exit 1; }

verify_counts

echo "==> sessions from before the upgrade, and fresh logins"
for person in alice bob; do
  [ "$(api "$person" "$BASE/api/auth/me" | jq -r '.username // .user.username')" = "$person" ] ||
    { echo "$person's session did not survive the upgrade" >&2; exit 1; }
done
login cool_guy "$ADMIN_PASSWORD"
login alice "$ALICE_PASSWORD"
login bob "$BOB_PASSWORD"

echo "==> data intact"
[ "$(messages alice "$GENERAL" | sort)" = "$GENERAL_BEFORE" ] || { echo "messages of #general changed" >&2; exit 1; }
[ "$(messages bob "$PROJECTS" | sort)" = "$PROJECTS_BEFORE" ] || { echo "messages of the project channel changed" >&2; exit 1; }
[ "$(api cool_guy "$BASE/api/channels" | jq -S '[.categories[].channels[], .uncategorized[]] | map({id, name, type}) | sort_by(.id)')" = "$CHANNELS_BEFORE" ] ||
  { echo "the channel list changed" >&2; exit 1; }
api bob "$BASE$MEDIA_URL" -o "$DEPLOY/after.bin"
cmp -s "$DEPLOY/payload.bin" "$DEPLOY/after.bin" || { echo "the upload differs after the upgrade" >&2; exit 1; }

echo "==> roles, overwrites, webhook, bot, notification levels, onboarding and role mentions"
verify_integration_data

echo "==> the upgraded server takes new writes"
post_message bob "$GENERAL" "after-upgrade-$TAG"
messages alice "$GENERAL" | grep -qx "after-upgrade-$TAG" || { echo "a new message was not stored" >&2; exit 1; }

echo "==> direct messages, blocks, saved messages and 1.1 features"
exercise_110_features

echo "==> the new tables take writes after the upgrade"
exercise_new_tables

echo "==> upgrade drill passed ($OLD_VERSION -> $NEW_VERSION) in $((SECONDS - LEG_STARTED))s"
