#!/usr/bin/env bash
# Online backup (BACKUP_MODE=online): the app keeps running. Sourced by
# backup.sh and restore.sh after backup-common.sh.
#
# Why the copy is consistent: the object storage is frozen (docker pause)
# before the database snapshot is taken and stays frozen until its volume is
# copied. An upload stores its object before its row is committed and a
# delete removes the row first, so every object a row of the snapshot points
# at is in the copy. restore.sh --verify proves that for each backup.

# The advisory lock the server's object-deleting jobs respect (auto-delete,
# media pruning, staged and orphan sweeps): internal/db/backuplock.go.
# shellcheck disable=SC2154 # dest, seaweed_running and archive_media come from backup.sh.
BACKUP_LOCK_ID=120320915497314
online_session='' online_pid='' seaweed_paused=false

# Opens one psql session that lives until online_release, and takes the lock
# in it. The session later exports the snapshot pg_dump reads.
online_lock() {
  local wait=${BACKUP_LOCK_WAIT:-600}
  [[ "$wait" =~ ^[0-9]+$ ]] || { backup_fail 'BACKUP_LOCK_WAIT must be a number of seconds'; return 1; }
  online_session=$(mktemp -d "${TMPDIR:-/tmp}/mnema-backup.XXXXXX") || return 1
  mkfifo "$online_session/in" "$online_session/out" || return 1
  # shellcheck disable=SC2016 # Expanded inside the container.
  docker compose exec -T postgres sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -AtqX -v ON_ERROR_STOP=1' \
    <"$online_session/in" >"$online_session/out" &
  online_pid=$!
  exec 3>"$online_session/in" 4<"$online_session/out"
  # A running sweep finishes first; lock_timeout bounds that wait.
  online_ask "SET lock_timeout = '${wait}s'; SELECT 'locked' FROM pg_advisory_lock($BACKUP_LOCK_ID);"
  [ "$online_reply" = locked ] || { backup_fail "no maintenance lock within ${wait}s: a server job is still deleting files"; return 1; }
}

# Sends one statement to the session and reads its one-line answer.
online_ask() {
  online_reply=''
  printf '%s\n' "$1" >&3 || return 1
  IFS= read -r -t "$((${BACKUP_LOCK_WAIT:-600} + 60))" online_reply <&4 || true
}

# Freezes the storage, fixes the database snapshot, copies the volume and
# lets the storage continue. The snapshot id is left in online_snapshot.
online_copy_media() {
  if [ "$seaweed_running" = true ]; then
    docker compose pause seaweedfs >/dev/null || return 1
    seaweed_paused=true
  fi
  online_ask 'BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY; SELECT pg_export_snapshot();'
  online_snapshot=$online_reply
  [[ "$online_snapshot" =~ ^[0-9A-Fa-f-]+$ ]] || { backup_fail 'the database did not export a snapshot'; return 1; }
  archive_media || return 1
  online_resume_storage
}

online_resume_storage() {
  [ "$seaweed_paused" = true ] || return 0
  docker compose unpause seaweedfs >/dev/null || return 1
  seaweed_paused=false
}

# Dumps exactly the snapshot taken while the storage was frozen, and lists
# the objects its rows point at (bucket/key), for restore.sh --verify.
online_dump() {
  # shellcheck disable=SC2016 # Expanded inside the container.
  docker compose exec -T postgres sh -c 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" --no-owner --snapshot="$1"' sh "$online_snapshot" </dev/null | gzip > "$dest/postgres.sql.gz" || return 1
  # shellcheck disable=SC2016
  docker compose exec -T postgres sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -AtqX -v ON_ERROR_STOP=1 -c "$1"' sh \
    "BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY; SET TRANSACTION SNAPSHOT '$online_snapshot'; SELECT s3_bucket || '/' || s3_key FROM media WHERE NOT is_deleted ORDER BY 1; COMMIT;" \
    </dev/null > "$dest/media-keys.txt"
}

# Ends the session (which gives the lock back) and unfreezes the storage.
# Safe to call twice; the EXIT trap of backup.sh calls it on every path.
online_release() {
  local failed=0
  online_resume_storage || { failed=1; echo 'seaweedfs is still paused: run "docker compose unpause seaweedfs"' >&2; }
  if [ -n "$online_session" ]; then
    # End of input ends psql; a session that is still waiting is stopped.
    exec 3>&- 4<&-
    local _
    for _ in 1 2 3 4 5 6 7 8 9 10; do kill -0 "$online_pid" 2>/dev/null || break; sleep 1; done
    kill "$online_pid" 2>/dev/null || true
    wait "$online_pid" 2>/dev/null || true
    rm -rf -- "$online_session"
    online_session=''
  fi
  return "$failed"
}

# restore.sh --verify, for a backup with media-keys.txt: starts a throwaway,
# network-isolated SeaweedFS on a copy of the media archive and reads every
# listed object. Needs disk space for the extracted media.
online_verify_media() {
  local src=$1 keys="$1/media-keys.txt" image total
  total=$(grep -c . "$keys" || true)
  if [ "$total" = 0 ]; then echo 'objects 0 (the database refers to none)'; return 0; fi
  image=${SEAWEEDFS_VERIFY_IMAGE:-$(sed -n 's/^seaweedfs_image_id=//p' "$src/manifest")}
  docker image inspect "$image" >/dev/null 2>&1 \
    || { backup_fail 'the media check needs the SeaweedFS image of this backup; pull it and set SEAWEEDFS_VERIFY_IMAGE=<image>'; return 1; }
  media_container=$(docker run --rm -d --network none --mount type=volume,dst=/data --entrypoint sleep "$image" 86400) || return 1
  gunzip -c "$src/seaweedfs.tar.gz" | docker exec -i "$media_container" tar xf - -C /data || return 1
  docker exec -d "$media_container" weed server -filer -dir=/data || return 1
  # Reads through the filer, so both its index and the volume data are checked.
  # shellcheck disable=SC2016 # Runs inside the container.
  docker exec -i "$media_container" sh -c '
    open() { wget -q -O /dev/null "http://127.0.0.1:8888/buckets/$1"; }
    n=0; until open "" 2>/dev/null; do n=$((n+1)); [ "$n" -lt 120 ] || { echo "the storage copy did not start" >&2; exit 1; }; sleep 1; done
    grace=30 missing=0
    while IFS= read -r key; do
      [ -n "$key" ] || continue
      case "$key" in *[!A-Za-z0-9._/-]*) echo "unreadable key: $key" >&2; missing=$((missing+1)); continue ;; esac
      # The first reads may come before the volumes are mounted.
      until open "$key"; do
        [ "$grace" -gt 0 ] || { echo "missing object: $key" >&2; missing=$((missing+1)); break; }
        grace=$((grace-1)); sleep 1
      done
    done
    [ "$missing" = 0 ]' < "$keys" || { backup_fail 'the media copy lacks objects the database refers to'; return 1; }
  echo "objects $total (every one opens from the media copy)"
}
