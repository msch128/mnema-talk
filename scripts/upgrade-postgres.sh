#!/usr/bin/env bash
# One-time move of the database from PostgreSQL 17 (Mnema Talk before 0.5,
# volume postgres-data) to PostgreSQL 18 (volume postgres18-data). Run from
# the deployment directory after updating docker-compose.yml, instead of
# `docker compose up -d`. Media (SeaweedFS) is not touched.
#
# 1. Stops the app.
# 2. Starts a throwaway PostgreSQL 17 on the old volume (no network) and
#    dumps the database to BACKUP_DIR.
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
cleanup() {
  status=$?
  trap - EXIT INT TERM
  if [ -n "$old_pg" ]; then docker rm -f -v "$old_pg" >/dev/null 2>&1 || true; fi
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

echo '==> stopping the app'
docker compose stop app >/dev/null 2>&1 || true

dump="$BACKUP_DIR/postgres17-upgrade-$(date -u +%Y%m%d-%H%M%S).sql.gz"
echo "==> dumping PostgreSQL 17 to $dump"
# Throwaway container on the old volume: no network, socket access only.
old_pg=$(docker run -d --network none -v "$old":/var/lib/postgresql/data "$PG17_IMAGE")
ready=false
for _ in $(seq 1 60); do
  if docker exec "$old_pg" pg_isready -U "$pg_user" -d "$pg_db" >/dev/null 2>&1; then ready=true; break; fi
  sleep 1
done
[ "$ready" = true ] || backup_fail 'PostgreSQL 17 did not start on the old volume'
docker exec "$old_pg" pg_dump -U "$pg_user" -d "$pg_db" --no-owner | gzip > "$dump"
gzip -t "$dump"
docker stop "$old_pg" >/dev/null
docker rm -v "$old_pg" >/dev/null
old_pg=''

echo '==> starting PostgreSQL 18 on its new volume'
MNEMA_PG_UPGRADE=1 docker compose up -d --no-deps --wait postgres
echo '==> importing the dump'
gunzip -c "$dump" | docker compose exec -T postgres psql -U "$pg_user" -d "$pg_db" -v ON_ERROR_STOP=1 -q >/dev/null
docker compose exec -T postgres psql -U "$pg_user" -d "$pg_db" -At -c "SELECT 'users ' || count(*) FROM users UNION ALL SELECT 'messages ' || count(*) FROM messages UNION ALL SELECT 'media ' || count(*) FROM media" </dev/null

echo '==> starting the stack'
# Recreates postgres without the upgrade flag, then starts everything else.
docker compose up -d --wait
echo "upgrade complete. The dump stays at $dump."
echo "The old PostgreSQL 17 volume is kept as a rollback; once everything works, remove it with:"
echo "  docker volume rm $old"
