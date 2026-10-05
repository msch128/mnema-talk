#!/usr/bin/env bash
# Restores a backup made by scripts/backup.sh into this compose deployment.
# DESTRUCTIVE: replaces the current database and media with the backup.
#
#   ./scripts/restore.sh backups/20261005-030000          # asks for confirmation
#   ./scripts/restore.sh backups/20261005-030000 --yes    # no prompt
#   ./scripts/restore.sh backups/20261005-030000 --verify # dry run, see below
#
# --verify restores the database dump into a scratch database next to the live
# one, prints row counts and drops it again. Nothing live is touched; use it
# to prove a backup can be restored.
set -euo pipefail

src=${1:?usage: restore.sh <backup-folder> [--yes|--verify]}
mode=${2:-}
[ -f "$src/postgres.sql.gz" ] && [ -f "$src/seaweedfs.tar.gz" ] || { echo "not a backup folder: $src" >&2; exit 1; }

psql_in() { docker compose exec -T postgres sh -c "psql -U \"\$POSTGRES_USER\" -v ON_ERROR_STOP=1 -q $*"; }

if [ "$mode" = "--verify" ]; then
  psql_in "-d postgres -c 'DROP DATABASE IF EXISTS mnema_restore_check' -c 'CREATE DATABASE mnema_restore_check'"
  trap "psql_in \"-d postgres -c 'DROP DATABASE IF EXISTS mnema_restore_check'\"" EXIT
  gunzip -c "$src/postgres.sql.gz" | psql_in "-d mnema_restore_check" > /dev/null
  docker compose exec -T postgres sh -c 'psql -U "$POSTGRES_USER" -d mnema_restore_check -At -c "
    SELECT '\''users '\'' || count(*) FROM users UNION ALL
    SELECT '\''channels '\'' || count(*) FROM channels UNION ALL
    SELECT '\''messages '\'' || count(*) FROM messages UNION ALL
    SELECT '\''media '\'' || count(*) FROM media UNION ALL
    SELECT '\''migrations '\'' || count(*) FROM schema_migrations"'
  echo "media archive: $(tar tzf "$src/seaweedfs.tar.gz" | wc -l) entries"
  echo "verify ok: $src"
  exit 0
fi

if [ "$mode" != "--yes" ]; then
  read -r -p "Replace the live database and media with $src? Type 'restore': " answer
  [ "$answer" = "restore" ] || { echo "aborted"; exit 1; }
fi

echo "stopping app"
docker compose stop app

echo "restoring database"
docker compose exec -T postgres sh -c 'psql -U "$POSTGRES_USER" -d postgres -v ON_ERROR_STOP=1 -q \
  -c "DROP DATABASE IF EXISTS \"$POSTGRES_DB\" WITH (FORCE)" -c "CREATE DATABASE \"$POSTGRES_DB\""'
gunzip -c "$src/postgres.sql.gz" | docker compose exec -T postgres sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1 -q' > /dev/null

echo "restoring media"
docker compose stop seaweedfs
seaweed=$(docker compose ps -a -q seaweedfs)
docker run --rm --volumes-from "$seaweed" -v "$(cd "$src" && pwd)":/in:ro alpine:3.24 \
  sh -c 'find /data -mindepth 1 -delete && tar xzf /in/seaweedfs.tar.gz -C /data'
docker compose up -d --wait seaweedfs

echo "starting app"
docker compose up -d app
echo "restore done: $src"
