#!/usr/bin/env bash
# Consistent compose backup; app/retention and SeaweedFS pause temporarily.
# BACKUP_MODE=online keeps the app running instead (calls stay connected):
# only the object storage is frozen while its volume is copied, see
# scripts/backup-online.sh. BACKUP_AGE_RECIPIENT=<age public key> encrypts the
# finished backup into <timestamp>.tar.age; unset (default) = not encrypted.
# Run from the deployment directory. BACKUP_DIR defaults outside the checkout;
# KEEP_DAYS=14 prunes only complete validated older backups after success.
# RESTORE_TEST=1 (default) then imports the dump into a throwaway database
# (restore.sh --verify, never the live one); 0 skips it. The result goes to
# BACKUP_STATUS_DIR/status.json (default ./backup-status), which the app reads
# read-only for Admin > Status (BACKUP_STATUS_FILE).
set -euo pipefail
# shellcheck source=scripts/backup-common.sh
source "$(dirname "${BASH_SOURCE[0]}")/backup-common.sh"
# shellcheck source=scripts/backup-online.sh
source "$(dirname "${BASH_SOURCE[0]}")/backup-online.sh"
BACKUP_DIR=${BACKUP_DIR:-$HOME/mnema-talk-backups}
KEEP_DAYS=${KEEP_DAYS:-14}
[[ "$KEEP_DAYS" =~ ^[0-9]+$ ]] || backup_fail 'KEEP_DAYS must be a nonnegative integer'
RESTORE_TEST=${RESTORE_TEST:-1}
[[ "$RESTORE_TEST" =~ ^[01]$ ]] || backup_fail 'RESTORE_TEST must be 1 or 0'
BACKUP_STATUS_DIR=${BACKUP_STATUS_DIR:-./backup-status}
BACKUP_MODE=${BACKUP_MODE:-cold}
[[ "$BACKUP_MODE" =~ ^(cold|online)$ ]] || backup_fail 'BACKUP_MODE must be cold or online'
BACKUP_AGE_RECIPIENT=${BACKUP_AGE_RECIPIENT:-}
[[ "$BACKUP_AGE_RECIPIENT" =~ [^[:space:]] ]] || BACKUP_AGE_RECIPIENT=''
[ -z "$BACKUP_AGE_RECIPIENT" ] || backup_recipients_ok "$BACKUP_AGE_RECIPIENT"
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
app_resume=false seaweed_resume=false dest='' complete=false
alpine=alpine:3.24.4@sha256:93b41ef40b1677fce8fcc64ff4af09daddfacf5040afde618bc46c1e2d1cc7d4
# The status file holds no secrets: times, size and the restore test result.
# It is written next to the old one and renamed, so the app never reads half.
write_backup_status() {
  local size=0 file json tmp
  for file in "$stored" "$stored"/*; do [ ! -f "$file" ] || size=$((size + $(wc -c < "$file"))); done
  json=$(printf '{"format":1,"last_backup_at":"%s","size_bytes":%s,"restore_test":{"result":"%s"%s}}' \
    "$created" "$size" "$1" "${2:+,\"at\":\"$2\"}")
  # The app (another uid) reads it: a new directory is world-readable.
  if [ ! -e "$BACKUP_STATUS_DIR" ]; then (umask 022 && mkdir -p "$BACKUP_STATUS_DIR") || return 1; fi
  [ -d "$BACKUP_STATUS_DIR" ] || return 1
  if [ -w "$BACKUP_STATUS_DIR" ]; then
    tmp=$(mktemp "$BACKUP_STATUS_DIR/.status.XXXXXX") || return 1
    if ! printf '%s\n' "$json" > "$tmp" || ! chmod 644 "$tmp" || ! mv -f "$tmp" "$BACKUP_STATUS_DIR/status.json"; then
      rm -f "$tmp"
      return 1
    fi
    return 0
  fi
  # Docker created the folder as root for the app's mount: write through a container.
  printf '%s\n' "$json" | docker run --rm -i --network none -v "$(cd "$BACKUP_STATUS_DIR" && pwd)":/out "$alpine" \
    sh -c 'cat > /out/.status.new && chmod 644 /out/.status.new && mv -f /out/.status.new /out/status.json'
}
cleanup() {
  status=$?
  trap - EXIT INT TERM
  storage_ready=true
  online_release || status=1
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
  [ "$status" = 0 ] || [ "$complete" = true ] || echo 'backup failed; incomplete directories are not restore points' >&2
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
archive_media() {
  docker run --rm --network none --volumes-from "$seaweed":ro -v "$(cd "$dest" && pwd)":/out "$alpine" sh -c 'tar czf /out/seaweedfs.tar.gz -C /data . && chmod 600 /out/seaweedfs.tar.gz && chown "$1:$2" /out/seaweedfs.tar.gz' sh "$(id -u)" "$(id -g)"
}
if [ "$BACKUP_MODE" = online ]; then
  echo "backup $ts (online): the app keeps running; file storage pauses while it is copied"
  # Nothing is stopped, so nothing is to resume: the storage is only paused.
  seaweed_running=$seaweed_resume app_resume=false seaweed_resume=false
  online_lock
  online_copy_media
  online_dump
  online_release
else
  echo "backup $ts: pausing writes and retention (active calls disconnect)"
  if [ "$app_resume" = true ]; then docker compose stop -t 60 app >/dev/null; fi
  [ "$(service_running "$app")" = false ] || backup_fail 'app did not stop; refusing inconsistent backup'
  if [ "$seaweed_resume" = true ]; then docker compose stop -t 60 seaweedfs >/dev/null; fi
  [ "$(service_running "$seaweed")" = false ] || backup_fail 'seaweedfs did not stop'
  docker compose exec -T postgres sh -c 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" --no-owner' </dev/null | gzip > "$dest/postgres.sql.gz"
fi
docker compose exec -T postgres sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -At -v ON_ERROR_STOP=1 -c "SELECT filename, checksum FROM schema_migrations ORDER BY filename"' </dev/null > "$dest/schema.txt"
[ "$BACKUP_MODE" = online ] || archive_media
cp .env "$dest/env"
docker compose config --no-interpolate </dev/null > "$dest/compose.yaml"
created=$(date -u +%Y-%m-%dT%H:%M:%SZ)
{
  echo format=mnema-backup-v1
  printf 'created_utc=%s\n' "$created"
  printf 'app_image_id=%s\n' "$(docker inspect -f '{{.Image}}' "$app")"
  printf 'postgres_image_id=%s\n' "$(docker inspect -f '{{.Image}}' "$postgres")"
  printf 'seaweedfs_image_id=%s\n' "$(docker inspect -f '{{.Image}}' "$seaweed")"
  if [ "$BACKUP_MODE" = online ]; then
    echo mode=online
    printf 'media_keys_sha256=%s\n' "$(cd "$dest" && backup_hashes media-keys.txt | cut -d' ' -f1)"
  fi
} > "$dest/manifest"
(cd "$dest" && backup_hashes postgres.sql.gz seaweedfs.tar.gz env manifest schema.txt compose.yaml > SHA256SUMS)
chmod 600 "$dest"/*
backup_validate "$dest" 0 preparing
printf 'mnema-backup-v1\n' > "$dest/COMPLETE"
# Recover service availability before pruning; failure leaves older backups intact.
if [ "$seaweed_resume" = true ]; then service_start seaweedfs; seaweed_resume=false; fi
if [ "$app_resume" = true ]; then service_start app; app_resume=false; fi
complete=true restore_test=skipped restore_at=''
if [ "$RESTORE_TEST" = 1 ]; then
  echo "restore test: importing $dest into a throwaway database"
  if bash "$(dirname "${BASH_SOURCE[0]}")/restore.sh" "$dest" --verify; then restore_test=passed; else restore_test=failed; fi
  restore_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
fi
# The restore test read the plain files; only now do they become one age file.
stored=$dest
if [ -n "$BACKUP_AGE_RECIPIENT" ]; then
  backup_encrypt "$dest" "$BACKUP_AGE_RECIPIENT"
  stored=$dest.tar.age
fi
status_written=true
if ! write_backup_status "$restore_test" "$restore_at"; then
  status_written=false
  echo "WARNING: could not write $BACKUP_STATUS_DIR/status.json; Admin > Status will not show this backup" >&2
fi
# A backup that failed its restore test is no reason to drop the older ones.
if [ "$restore_test" != failed ]; then
  while IFS= read -r -d '' old; do
    [ "$old" != "$dest" ] || continue
    if backup_validate "$old" 0 >/dev/null 2>&1; then rm -rf -- "$old"; fi
  done < <(find "$BACKUP_DIR" -mindepth 1 -maxdepth 1 -type d -name '20*' -mtime +"$KEEP_DAYS" -print0)
  # Encrypted backups cannot be opened here; each was validated before it was encrypted.
  find "$BACKUP_DIR" -mindepth 1 -maxdepth 1 -type f -name '20*.tar.age' ! -path "$stored" -mtime +"$KEEP_DAYS" -exec rm -f -- {} +
fi
echo "backup complete: $stored (restore test: $restore_test)"
[ "$restore_test" != failed ] || backup_fail 'restore test FAILED: this backup may not be restorable; older backups were kept'
[ "$status_written" = true ] || exit 1
