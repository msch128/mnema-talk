#!/usr/bin/env bash
# One-time move of the database from PostgreSQL 17 (Mnema Talk before 0.5,
# volume postgres-data) to PostgreSQL 18 (volume postgres18-data). Run from
# the deployment directory after updating docker-compose.yml, instead of
# `docker compose up -d`. Media (SeaweedFS) is not touched.
#
# 1. Stops the app and its existing PostgreSQL service.
# 2. Cold-copies the stopped PostgreSQL 17 volume into a disposable volume,
#    starts a throwaway PostgreSQL 17 on that copy (no network), and dumps
#    the database to BACKUP_DIR.
# 3. Starts PostgreSQL 18 on its new, empty volume and imports the dump.
# 4. Starts the stack.
# The old volume is never modified by Mnema Talk and stays as the rollback;
# remove it once you are happy (the script prints the command).
set -euo pipefail
# shellcheck source=scripts/backup-common.sh
source "$(dirname "${BASH_SOURCE[0]}")/backup-common.sh"

# The image that wrote the old volume (Mnema Talk 0.4.x).
PG17_IMAGE=postgres:17-alpine@sha256:b0f9560a2de083e2cc7382e75f808c7381a32852a7ec49117deedb300e552b24
BACKUP_DIR=${BACKUP_DIR:-$HOME/mnema-talk-backups}

[ -f .env ] && [ ! -L .env ] || backup_fail 'run this in the deployment directory (a regular .env is required)'
# Dumps contain private community data; use the same work-tree safeguard as
# backup.sh before creating any directory or changing a running service.
if [ "${BACKUP_ALLOW_IN_REPO:-0}" != 1 ] && command -v git >/dev/null 2>&1; then
  probe=$BACKUP_DIR
  while [ ! -d "$probe" ]; do probe=$(dirname "$probe"); done
  if git -C "$probe" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    backup_fail 'BACKUP_DIR is inside a git work tree; database dumps contain private data'
  fi
fi
env_value() { sed -n "s/^$1=//p" .env | tail -n 1; }
pg_user=$(env_value POSTGRES_USER); pg_user=${pg_user:-mnema}
pg_db=$(env_value POSTGRES_DB); pg_db=${pg_db:-mnema_talk}

project=$(docker compose config </dev/null | sed -n 's/^name: //p' | head -n 1)
[ -n "$project" ] || backup_fail 'could not read the compose project name'
volume_of() {
  docker volume ls -q --filter "label=com.docker.compose.project=$project" --filter "label=com.docker.compose.volume=$1"
}
has_cluster() { # volume, path of PG_VERSION inside it
  docker run --rm --network none -v "$1":/v:ro "$PG17_IMAGE" test -s "/v/$2"
}

old=$(volume_of postgres-data)
if [ -z "$old" ] || ! has_cluster "$old" PG_VERSION; then
  backup_fail 'no PostgreSQL 17 data (volume postgres-data) found: nothing to upgrade'
fi
new=$(volume_of postgres18-data)
if [ -n "$new" ] && has_cluster "$new" 18/docker/PG_VERSION; then
  backup_fail "PostgreSQL 18 already has a database (volume $new): already upgraded? To retry, run docker compose down and docker volume rm $new first"
fi

umask 077
mkdir -p "$BACKUP_DIR"
maintenance_lock
old_pg=''
old_copy=''
cleanup() {
  status=$?
  trap - EXIT INT TERM
  if [ -n "$old_pg" ]; then docker rm -f -v "$old_pg" >/dev/null 2>&1 || true; fi
  if [ -n "$old_copy" ]; then docker volume rm "$old_copy" >/dev/null 2>&1 || true; fi
  rmdir .mnema-maintenance.lock
  if [ "$status" != 0 ]; then
    echo 'upgrade failed; the PostgreSQL 17 volume is unchanged.' >&2
    echo 'To roll back, run the previous docker-compose.yml/MNEMA_IMAGE again; to retry, remove the new volume first:' >&2
    echo "  docker compose down && docker volume rm ${project}_postgres18-data" >&2
  fi
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

echo '==> stopping the app and existing PostgreSQL service'
# Do not open PGDATA in a second server while the old server still runs.
# A failed stop must abort before cloning or starting any database.
docker compose stop app postgres >/dev/null
# The container-list snapshot can lag behind a successful stop. List all
# matching containers, then inspect their live state before cold-copying.
# Running includes paused and restarting containers; uncertain state is unsafe.
mounted=$(docker ps -aq --filter "volume=$old") || backup_fail 'could not list containers mounting the PostgreSQL 17 volume'
for id in $mounted; do
  running=$(docker inspect -f '{{.State.Running}}' "$id") || backup_fail "could not inspect a container mounting the PostgreSQL 17 volume: $id"
  case "$running" in
    false) ;;
    true) backup_fail "the PostgreSQL 17 volume is still mounted by a running container ($id); stop it before retrying" ;;
    *) backup_fail "could not establish whether a container mounting the PostgreSQL 17 volume is stopped: $id" ;;
  esac
done

echo '==> copying the stopped PostgreSQL 17 volume for a read-only rollback'
old_copy=$(docker volume create --label "mnema-talk.pg-upgrade-copy=$project")
[ -n "$old_copy" ] || backup_fail 'could not create the disposable PostgreSQL 17 copy'
docker run --rm --network none -v "$old":/source:ro -v "$old_copy":/copy "$PG17_IMAGE" sh -c 'cp -a /source/. /copy/'

dump="$BACKUP_DIR/postgres17-upgrade-$(date -u +%Y%m%d-%H%M%S).sql.gz"
echo "==> dumping PostgreSQL 17 to $dump"
# Throwaway server writes only the disposable copy; the original remains
# read-only and has no new postmaster/WAL/control-file writes.
old_pg=$(docker run -d --network none -v "$old_copy":/var/lib/postgresql/data "$PG17_IMAGE")
ready=false
for _ in $(seq 1 60); do
  if docker exec "$old_pg" pg_isready -U "$pg_user" -d "$pg_db" >/dev/null 2>&1; then ready=true; break; fi
  sleep 1
done
[ "$ready" = true ] || backup_fail 'PostgreSQL 17 did not start on the disposable copy'
docker exec "$old_pg" pg_dump -U "$pg_user" -d "$pg_db" --no-owner | gzip > "$dump"
gzip -t "$dump"
docker stop "$old_pg" >/dev/null
docker rm -v "$old_pg" >/dev/null
old_pg=''
docker volume rm "$old_copy" >/dev/null
old_copy=''

echo '==> starting PostgreSQL 18 on its new volume'
MNEMA_PG_UPGRADE=1 docker compose up -d --no-deps --wait postgres
echo '==> importing the dump'
gunzip -c "$dump" | docker compose exec -T postgres psql -U "$pg_user" -d "$pg_db" -v ON_ERROR_STOP=1 -q >/dev/null
docker compose exec -T postgres psql -U "$pg_user" -d "$pg_db" -At -c "SELECT 'users ' || count(*) FROM users UNION ALL SELECT 'messages ' || count(*) FROM messages UNION ALL SELECT 'media ' || count(*) FROM media" </dev/null

# PG_VERSION exists as soon as PostgreSQL initializes, before the import has
# succeeded. Only this marker authorizes normal startup beside old 17 data.
# Publish it atomically after both the import and the verification above.
docker compose exec -T postgres sh -c 'umask 077; printf "%s\n" mnema-postgres17-upgrade-v1 > /var/lib/postgresql/.mnema-pg17-upgrade-complete.tmp && mv /var/lib/postgresql/.mnema-pg17-upgrade-complete.tmp /var/lib/postgresql/.mnema-pg17-upgrade-complete' </dev/null

echo '==> starting the stack'
# Recreates postgres without the upgrade flag, then starts everything else.
docker compose up -d --wait
echo "upgrade complete. The dump stays at $dump."
echo "The old PostgreSQL 17 volume is kept as a rollback; once everything works, remove it with:"
echo "  docker volume rm $old"
