#!/usr/bin/env bash
# Backs up a running Mnema Talk compose deployment: a PostgreSQL dump, the
# SeaweedFS (S3) data volume and the .env file, into one timestamped folder.
# Old backups beyond KEEP_DAYS are removed.
#
# Run it in the deployment directory (where docker-compose.yml and .env are),
# e.g. nightly from cron:
#   0 3 * * *  cd /path/to/mnema-talk && ./scripts/backup.sh >> "$HOME/mnema-talk-backups/backup.log" 2>&1
#
# Environment:
#   BACKUP_DIR      where to write (default: $HOME/mnema-talk-backups). A backup
#                   holds .env with every secret, so it is refused inside a git
#                   work tree (where it could be committed) unless
#                   BACKUP_ALLOW_IN_REPO=1.
#   KEEP_DAYS       retention in days (default 14).
#   SEAWEED_LIVE=1  copy the SeaweedFS volume while it runs instead of
#                   stopping it for the copy (no media downtime, but a file
#                   written during the copy may be missing or torn).
set -euo pipefail

BACKUP_DIR=${BACKUP_DIR:-$HOME/mnema-talk-backups}
KEEP_DAYS=${KEEP_DAYS:-14}
ts=$(date +%Y%m%d-%H%M%S)

log() { echo "$(date -Iseconds) $*"; }

# Refuse a destination inside a git checkout: the backup contains .env.
if [ "${BACKUP_ALLOW_IN_REPO:-0}" != "1" ] && command -v git >/dev/null 2>&1; then
  probe=$BACKUP_DIR
  while [ ! -d "$probe" ]; do probe=$(dirname "$probe"); done
  if git -C "$probe" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    echo "BACKUP_DIR=$BACKUP_DIR is inside a git work tree; backups contain secrets (.env)." >&2
    echo "Choose a directory outside the checkout, or set BACKUP_ALLOW_IN_REPO=1 if it is git-ignored." >&2
    exit 1
  fi
fi

dest="$BACKUP_DIR/$ts"
umask 077
mkdir -p "$dest"

log "backup $ts start"

# 1. Database: a consistent logical dump while the app keeps running.
docker compose exec -T postgres sh -c 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" --no-owner' \
  | gzip > "$dest/postgres.sql.gz"

# 2. Media: the SeaweedFS data volume. SeaweedFS keeps volume files and filer
# metadata open, so by default it is stopped for the copy, which gives a
# consistent snapshot. The app keeps running: chat works, uploads and media
# loads fail for the few moments of the copy and recover by themselves.
seaweed=$(docker compose ps -a -q seaweedfs)
[ -n "$seaweed" ] || { echo "seaweedfs container not found" >&2; exit 1; }
restart_seaweed() { docker compose start seaweedfs >/dev/null; }
if [ "${SEAWEED_LIVE:-0}" != "1" ]; then
  log "stopping seaweedfs for a consistent copy"
  trap restart_seaweed EXIT
  docker compose stop seaweedfs >/dev/null
fi
docker run --rm --volumes-from "$seaweed":ro -v "$(cd "$dest" && pwd)":/out alpine:3.24 \
  tar czf /out/seaweedfs.tar.gz -C /data .
if [ "${SEAWEED_LIVE:-0}" != "1" ]; then
  restart_seaweed
  trap - EXIT
  log "seaweedfs started again"
fi

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
