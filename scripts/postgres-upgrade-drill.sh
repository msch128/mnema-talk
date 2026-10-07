#!/usr/bin/env bash
# Disposable PostgreSQL 17 -> 18 regressions: upgrade directly from running
# 17, exercise the normal-start guard, and prove failed stop/import fail closed.
# Usage: scripts/postgres-upgrade-drill.sh [image]
# DRILL_SCENARIOS="direct guard import-failure stop-failure" (default all)
# DRILL_PORT=58290; each scenario gets independent random containers/volumes.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
IMAGE="${1:-mnema-talk:local}"
PORT="${DRILL_PORT:-58290}"
BASE="http://127.0.0.1:$PORT"
SCENARIOS="${DRILL_SCENARIOS:-direct guard import-failure stop-failure}"
REAL_DOCKER="$(command -v docker)"
PG17_IMAGE=postgres:17-alpine@sha256:b0f9560a2de083e2cc7382e75f808c7381a32852a7ec49117deedb300e552b24
export COMPOSE_PROFILES=''
export MNEMA_PG_UPGRADE=''
DEPLOY=''
cleanup() {
  status=$?
  trap - EXIT INT TERM
  if [ -n "$DEPLOY" ]; then
    if [ "$status" -ne 0 ]; then
      docker compose logs --no-color --tail 30 postgres app >&2 || true
    fi
    docker compose down -v --remove-orphans >/dev/null 2>&1 || true
    # Only this collision-checked random project's disposable fixture volumes.
    while IFS= read -r volume; do
      [ -z "$volume" ] || docker volume rm "$volume" >/dev/null 2>&1 || true
    done < <(docker volume ls -q --filter "label=com.docker.compose.project=$COMPOSE_PROJECT_NAME")
    rm -rf "$DEPLOY"
  fi
  exit "$status"
}

for scenario in $SCENARIOS; do
  case "$scenario" in direct|guard|import-failure|stop-failure) ;; *) echo "unknown drill scenario: $scenario" >&2; exit 1 ;; esac
  project="mnema-pgdrill-$(openssl rand -hex 8)"
  # Check ownership BEFORE installing destructive cleanup; refuse collisions.
  for kind in container network volume; do
    case "$kind" in container) existing=$(docker ps -aq --filter "label=com.docker.compose.project=$project") ;;
      *) existing=$(docker "$kind" ls -q --filter "label=com.docker.compose.project=$project") ;; esac
    [ -z "$existing" ] || { echo "project $project already has $kind resources; refusing without cleanup" >&2; exit 1; }
  done
  # Also refuse exact names whose owners did not attach Compose labels.
  existing=$(docker ps -aq --filter "name=^/${project}-(app|postgres|seaweedfs|coturn|updater)$")
  [ -z "$existing" ] || { echo 'drill container names already exist; refusing without cleanup' >&2; exit 1; }
  existing=$(docker network ls -q --filter "name=^${project}-(network|updater-network)$")
  [ -z "$existing" ] || { echo 'drill network names already exist; refusing without cleanup' >&2; exit 1; }
  existing=$(docker volume ls -q --filter "name=^${project}_(postgres-data|postgres18-data|seaweedfs-data)$")
  [ -z "$existing" ] || { echo 'drill volume names already exist; refusing without cleanup' >&2; exit 1; }
  DEPLOY="$(mktemp -d)"
  export COMPOSE_PROJECT_NAME="$project"
  export COMPOSE_FILE="$DEPLOY/docker-compose.yml:$DEPLOY/drill-isolation.yml"
  export BACKUP_DIR="$DEPLOY/backups"
  cp "$ROOT/docker-compose.yml" "$DEPLOY/"
  cp -R "$ROOT/scripts" "$DEPLOY/scripts"
  # Project selection alone does not override production's fixed names.
  cat >"$DEPLOY/drill-isolation.yml" <<YAML
services:
  app:
    container_name: $project-app
    ports: !override
      - "127.0.0.1:$PORT:8080"
      - "127.0.0.1:58300-58310:58300-58310/udp"
  postgres:
    container_name: $project-postgres
  seaweedfs:
    container_name: $project-seaweedfs
  coturn:
    container_name: $project-coturn
  updater:
    container_name: $project-updater
networks:
  mnema-network:
    name: $project-network
  mnema-updater:
    name: $project-updater-network
YAML
  cat >"$DEPLOY/postgres17.yml" <<YAML
services:
  postgres:
    image: $PG17_IMAGE
    entrypoint: !reset null
    volumes: !override
      - postgres-data:/var/lib/postgresql/data
YAML
  ADMIN_PASSWORD="drill-$(openssl rand -hex 8)"
  cat >"$DEPLOY/.env" <<ENV
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
ENV
  chmod 600 "$DEPLOY/.env"
  cd "$DEPLOY"
  trap cleanup EXIT
  trap 'exit 130' INT
  trap 'exit 143' TERM
  COOKIES="$DEPLOY/cookies"
  login() {
    rm -f "$COOKIES"
    curl -fsS -c "$COOKIES" -H "Origin: $BASE" -H 'Content-Type: application/json' \
      --data "{\"username\":\"Herzog\",\"password\":\"$ADMIN_PASSWORD\"}" "$BASE/api/auth/login" >/dev/null
  }
  api() { curl -fsS -b "$COOKIES" -H "Origin: $BASE" "$@"; }
  guard_refuses() {
    # This expected refusal is the behavior being tested; never start the app.
    docker compose up -d --no-build --no-deps postgres >/dev/null 2>&1 || true
    hint=false
    for _ in $(seq 1 20); do
      id=$(docker compose ps -a -q postgres)
      health=$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{end}}' "$id")
      [ "$health" != healthy ] || { echo 'unguarded PostgreSQL 18 became healthy' >&2; return 1; }
      if docker compose logs --no-color postgres 2>&1 | grep -q 'upgrade-postgres.sh'; then hint=true; break; fi
      sleep 1
    done
    [ "$hint" = true ] || { echo 'no upgrade refusal in PostgreSQL log' >&2; return 1; }
    docker compose stop postgres >/dev/null
  }
  make_fault_wrapper() {
    mkdir "$DEPLOY/fault-bin"
    cat >"$DEPLOY/fault-bin/docker" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
if [ "$DRILL_FAULT" = stop ] && [ "${1:-}" = compose ] && [ "${2:-}" = stop ] && [ "${3:-}" = app ] && [ "${4:-}" = postgres ]; then
  echo 'injected stop refusal' >&2
  exit 42
fi
if [ "$DRILL_FAULT" = import ] && [ "${1:-}" = compose ] && [ "${2:-}" = exec ]; then
  psql=false quiet=false
  for arg in "$@"; do [ "$arg" != psql ] || psql=true; [ "$arg" != -q ] || quiet=true; done
  if [ "$psql" = true ] && [ "$quiet" = true ]; then
    # Consume the real dump but restore only its beginning, then fail through
    # the real psql process. PG_VERSION exists despite this failed import.
    awk 'NR <= 100 {print} END {print "\nSELECT mnema_drill_missing_function();"}' | "$DRILL_REAL_DOCKER" "$@"
    exit 0
  fi
fi
exec "$DRILL_REAL_DOCKER" "$@"
SH
    chmod +x "$DEPLOY/fault-bin/docker"
  }

  echo "==> $scenario: installing PostgreSQL 17 with a message and upload"
  COMPOSE_FILE="$COMPOSE_FILE:$DEPLOY/postgres17.yml" docker compose up -d --no-build --wait --wait-timeout 180
  login
  CHANNEL="$(api "$BASE/api/channels" | jq -r '[.categories[].channels[], .uncategorized[]] | map(select(.type == "text")) | first | .id')"
  MESSAGE="pg17-$(openssl rand -hex 4)"
  api -H 'Content-Type: application/json' --data "{\"content\":\"$MESSAGE\"}" "$BASE/api/channels/$CHANNEL/messages" >/dev/null
  head -c 4096 /dev/urandom >"$DEPLOY/payload.bin"
  MEDIA_URL="$(api -F "file=@$DEPLOY/payload.bin;type=application/octet-stream" "$BASE/api/channels/$CHANNEL/upload" | jq -r '.attachments[0].url')"
  old=$(docker volume ls -q --filter "label=com.docker.compose.project=$project" --filter 'label=com.docker.compose.volume=postgres-data')
  [ -n "$old" ] || { echo 'missing own PostgreSQL 17 fixture volume' >&2; exit 1; }

  if [ "$scenario" = direct ]; then
    echo '==> refusing a dump destination inside the checkout before any service changes'
    forbidden_backup="$ROOT/.codex/forbidden-upgrade-backup-$project"
    if BACKUP_ALLOW_IN_REPO=0 BACKUP_DIR="$forbidden_backup" scripts/upgrade-postgres.sh >"$DEPLOY/forbidden-backup.log" 2>&1; then
      echo 'upgrade accepted a backup directory inside a git work tree' >&2; exit 1
    fi
    grep -q 'inside a git work tree' "$DEPLOY/forbidden-backup.log"
    [ ! -e "$forbidden_backup" ] || { echo 'refused backup directory was created' >&2; exit 1; }
    id=$(docker compose ps -a -q postgres)
    [ "$(docker inspect -f '{{.State.Running}}' "$id")" = true ] || { echo 'backup refusal stopped the old database' >&2; exit 1; }
  fi

  case "$scenario" in
    guard) echo '==> normal startup refuses unmigrated data'; guard_refuses ;;
    direct) echo '==> upgrading directly while the original PostgreSQL 17 is running'
      id=$(docker compose ps -a -q postgres)
      [ "$(docker inspect -f '{{.State.Running}}' "$id")" = true ] || { echo 'direct-upgrade fixture is not running' >&2; exit 1; } ;;
  esac
  if [ "$scenario" = stop-failure ] || [ "$scenario" = import-failure ]; then
    make_fault_wrapper
    fault=stop; [ "$scenario" != import-failure ] || fault=import
    if PATH="$DEPLOY/fault-bin:$PATH" DRILL_FAULT="$fault" DRILL_REAL_DOCKER="$REAL_DOCKER" scripts/upgrade-postgres.sh; then
      echo "$scenario unexpectedly succeeded" >&2; exit 1
    fi
    if [ "$scenario" = stop-failure ]; then
      id=$(docker compose ps -a -q postgres)
      [ "$(docker inspect -f '{{.State.Running}}' "$id")" = true ] || { echo 'failed stop altered the running PostgreSQL' >&2; exit 1; }
      [ -z "$(docker volume ls -q --filter "label=mnema-talk.pg-upgrade-copy=$project")" ] || { echo 'failed stop created a cluster copy' >&2; exit 1; }
      [ -z "$(find "$BACKUP_DIR" -type f -name '*.sql.gz')" ] || { echo 'failed stop produced a database dump' >&2; exit 1; }
    else
      docker compose exec -T postgres test -s /var/lib/postgresql/18/docker/PG_VERSION </dev/null
      if docker compose exec -T postgres test -e /var/lib/postgresql/.mnema-pg17-upgrade-complete </dev/null; then
        echo 'failed import published the completion marker' >&2; exit 1
      fi
      echo '==> failed import must still block normal startup with PG_VERSION present'
      guard_refuses
      id=$(docker compose ps -a -q app)
      [ "$(docker inspect -f '{{.State.Running}}' "$id")" = false ] || { echo 'app resumed after failed import' >&2; exit 1; }
    fi
  else
    scripts/upgrade-postgres.sh
    docker compose exec -T postgres psql -U mnema -d mnema_talk -At -c 'SHOW server_version' </dev/null | grep -q '^18\.'
    [ "$(docker compose exec -T postgres cat /var/lib/postgresql/.mnema-pg17-upgrade-complete </dev/null)" = mnema-postgres17-upgrade-v1 ]
    login
    api "$BASE/api/channels/$CHANNEL/messages?limit=100" | jq -r '.. | .content? // empty' | grep -qx "$MESSAGE" || { echo 'message missing after upgrade' >&2; exit 1; }
    api "$BASE$MEDIA_URL" -o "$DEPLOY/restored.bin"
    cmp -s "$DEPLOY/payload.bin" "$DEPLOY/restored.bin" || { echo 'upload differs after upgrade' >&2; exit 1; }
    if scripts/upgrade-postgres.sh >/dev/null 2>&1; then echo 'upgrade ran twice' >&2; exit 1; fi
  fi
  # Original cluster survives every path and is mounted only read-only by 18.
  docker run --rm --network none -v "$old":/v:ro "$PG17_IMAGE" test -s /v/PG_VERSION
  echo "==> $scenario passed"
  docker compose down -v --remove-orphans >/dev/null 2>&1
  while IFS= read -r volume; do [ -z "$volume" ] || docker volume rm "$volume" >/dev/null; done < <(docker volume ls -q --filter "label=com.docker.compose.project=$project")
  cd "$ROOT"
  rm -rf "$DEPLOY"
  DEPLOY=''
  trap - EXIT INT TERM
done
echo '==> postgres upgrade drill passed'
