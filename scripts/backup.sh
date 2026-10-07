#!/usr/bin/env bash
# Consistent compose backup; app/retention and SeaweedFS pause temporarily.
# Run from the deployment directory. BACKUP_DIR defaults outside the checkout;
# KEEP_DAYS=14 prunes only complete validated older backups after success.
set -euo pipefail
# shellcheck source=scripts/backup-common.sh
source "$(dirname "${BASH_SOURCE[0]}")/backup-common.sh"
BACKUP_DIR=${BACKUP_DIR:-$HOME/mnema-talk-backups}
KEEP_DAYS=${KEEP_DAYS:-14}
[[ "$KEEP_DAYS" =~ ^[0-9]+$ ]] || backup_fail 'KEEP_DAYS must be a nonnegative integer'
[ "${SEAWEED_LIVE:-0}" = 0 ] || backup_fail 'SEAWEED_LIVE is incompatible with consistent backups; unset it'
[ -f .env ] && [ ! -L .env ] || backup_fail 'a regular .env is required'
if [ "${BACKUP_ALLOW_IN_REPO:-0}" != 1 ] && command -v git >/dev/null 2>&1; then
  probe=$BACKUP_DIR
  while [ ! -d "$probe" ]; do probe=$(dirname "$probe"); done
  if git -C "$probe" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    backup_fail 'BACKUP_DIR is inside a git work tree; backups contain secrets (.env)'
  fi
fi
umask 077
mkdir -p "$BACKUP_DIR"
maintenance_lock
app_resume=false seaweed_resume=false dest=''
cleanup() {
  status=$?
  trap - EXIT INT TERM
  storage_ready=true
  # A failed copy/dump must restore precisely the previous running services.
  if [ "$seaweed_resume" = true ]; then
    if ! service_start seaweedfs; then
      status=1 storage_ready=false
      echo 'seaweedfs did not resume; app remains stopped' >&2
    fi
  fi
  if [ "$app_resume" = true ] && [ "$storage_ready" = true ]; then
    if ! service_start app; then status=1; fi
  fi
  rmdir .mnema-maintenance.lock
  [ "$status" = 0 ] || echo 'backup failed; incomplete directories are not restore points' >&2
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
app=$(service_id app)
seaweed=$(service_id seaweedfs)
postgres=$(service_id postgres)
[ "$(service_running "$postgres")" = true ] || backup_fail 'postgres must already be running'
app_resume=$(service_running "$app")
seaweed_resume=$(service_running "$seaweed")
ts=$(date -u +%Y%m%d-%H%M%S)
dest=$(mktemp -d "$BACKUP_DIR/$ts-XXXXXX")
echo "backup $ts: pausing writes and retention (active calls disconnect)"
if [ "$app_resume" = true ]; then docker compose stop -t 60 app >/dev/null; fi
[ "$(service_running "$app")" = false ] || backup_fail 'app did not stop; refusing inconsistent backup'
if [ "$seaweed_resume" = true ]; then docker compose stop -t 60 seaweedfs >/dev/null; fi
[ "$(service_running "$seaweed")" = false ] || backup_fail 'seaweedfs did not stop'
docker compose exec -T postgres sh -c 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" --no-owner' </dev/null | gzip > "$dest/postgres.sql.gz"
docker compose exec -T postgres sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -At -v ON_ERROR_STOP=1 -c "SELECT filename, checksum FROM schema_migrations ORDER BY filename"' </dev/null > "$dest/schema.txt"
docker run --rm --network none --volumes-from "$seaweed":ro -v "$(cd "$dest" && pwd)":/out alpine:3.24 sh -c 'tar czf /out/seaweedfs.tar.gz -C /data . && chmod 600 /out/seaweedfs.tar.gz && chown "$1:$2" /out/seaweedfs.tar.gz' sh "$(id -u)" "$(id -g)"
cp .env "$dest/env"
docker compose config --no-interpolate </dev/null > "$dest/compose.yaml"
{
  echo format=mnema-backup-v1
  printf 'created_utc=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  printf 'app_image_id=%s\n' "$(docker inspect -f '{{.Image}}' "$app")"
  printf 'postgres_image_id=%s\n' "$(docker inspect -f '{{.Image}}' "$postgres")"
  printf 'seaweedfs_image_id=%s\n' "$(docker inspect -f '{{.Image}}' "$seaweed")"
} > "$dest/manifest"
(cd "$dest" && backup_hashes postgres.sql.gz seaweedfs.tar.gz env manifest schema.txt compose.yaml > SHA256SUMS)
chmod 600 "$dest"/*
backup_validate "$dest" 0 preparing
printf 'mnema-backup-v1\n' > "$dest/COMPLETE"
# Recover service availability before pruning; failure leaves older backups intact.
if [ "$seaweed_resume" = true ]; then service_start seaweedfs; seaweed_resume=false; fi
if [ "$app_resume" = true ]; then service_start app; app_resume=false; fi
while IFS= read -r -d '' old; do
  [ "$old" != "$dest" ] || continue
  if backup_validate "$old" 0 >/dev/null 2>&1; then rm -rf -- "$old"; fi
done < <(find "$BACKUP_DIR" -mindepth 1 -maxdepth 1 -type d -name '20*' -mtime +"$KEEP_DAYS" -print0)
echo "backup complete: $dest"
