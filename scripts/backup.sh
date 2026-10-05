#!/usr/bin/env bash
# Backs up a running Mnema Talk compose deployment: a PostgreSQL dump, the
# SeaweedFS (S3) data volume and the .env file, into one timestamped folder.
# Old backups beyond KEEP_DAYS are removed.
#
# Run it in the deployment directory (where docker-compose.yml and .env are),
# e.g. nightly from cron:
#   0 3 * * *  cd /path/to/mnema-talk && ./scripts/backup.sh >> backups/backup.log 2>&1
#
# Environment: BACKUP_DIR (default ./backups), KEEP_DAYS (default 14).
set -euo pipefail

BACKUP_DIR=${BACKUP_DIR:-./backups}
KEEP_DAYS=${KEEP_DAYS:-14}
ts=$(date +%Y%m%d-%H%M%S)
dest="$BACKUP_DIR/$ts"
umask 077
mkdir -p "$dest"

log() { echo "$(date -Iseconds) $*"; }

log "backup $ts start"

# 1. Database: a consistent logical dump while the app keeps running.
docker compose exec -T postgres sh -c 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" --no-owner' \
  | gzip > "$dest/postgres.sql.gz"

# 2. Media: the SeaweedFS data volume. Objects are written once and never
# modified, so a copy taken while running is usable; a file still being
# written at that moment is simply missing from this backup.
seaweed=$(docker compose ps -q seaweedfs)
docker run --rm --volumes-from "$seaweed":ro -v "$(cd "$dest" && pwd)":/out alpine:3.24 \
  tar czf /out/seaweedfs.tar.gz -C /data .

# 3. Configuration (secrets).
cp -p .env "$dest/env"
# Everything in a backup is private (the media archive is written by root in
# a container, outside this shell's umask).
chmod 600 "$dest"/*

# Integrity check: both archives must be readable to the end.
gzip -t "$dest/postgres.sql.gz"
tar tzf "$dest/seaweedfs.tar.gz" > /dev/null
du -sh "$dest" | awk -v ts="$ts" '{print "'"$(date -Iseconds)"' backup " ts " done, " $1}'

# Retention: drop timestamped backup folders older than KEEP_DAYS.
find "$BACKUP_DIR" -mindepth 1 -maxdepth 1 -type d -name '20*' -mtime +"$KEEP_DAYS" -print -exec rm -rf {} + \
  | sed "s/^/$(date -Iseconds) removed old backup /"
