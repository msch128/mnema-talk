#!/usr/bin/env bash
# Shared, non-secret backup validation and compose maintenance helpers.
backup_fail() { echo "$*" >&2; return 1; }
backup_hashes() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$@";
  else shasum -a 256 "$@"; fi
}
maintenance_lock() {
  mkdir .mnema-maintenance.lock 2>/dev/null || backup_fail 'another backup/restore holds .mnema-maintenance.lock; inspect before removing a stale lock'
}
service_id() {
  local id
  id=$(docker compose ps -a -q "$1")
  [ -n "$id" ] && [ "$(printf '%s\n' "$id" | wc -l | tr -d ' ')" = 1 ] || { backup_fail "expected exactly one $1 container"; return 1; }
  printf '%s\n' "$id"
}
service_running() { docker inspect -f '{{.State.Running}}' "$1"; }
# Start a stopped service and wait until it is healthy (or running, without a
# healthcheck). Unlike `compose start --wait` this works with every Compose v2.
service_start() {
  local id state _
  docker compose start "$1" >/dev/null || return 1
  id=$(service_id "$1") || return 1
  for _ in $(seq 1 "${SERVICE_START_TIMEOUT:-180}"); do
    state=$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Running}}{{end}}' "$id") || return 1
    case "$state" in healthy|true) return 0 ;; esac
    sleep 1
  done
  backup_fail "$1 did not become healthy"
}
backup_validate() {
  local src=$1 legacy=$2 preparing=${3:-} name actual
  for name in postgres.sql.gz seaweedfs.tar.gz env; do
    [ -f "$src/$name" ] && [ ! -L "$src/$name" ] || backup_fail "missing/unsafe backup file: $name" || return
  done
  if [ "$legacy" != 1 ] || [ -e "$src/COMPLETE" ] || [ -e "$src/manifest" ]; then
    for name in manifest schema.txt compose.yaml SHA256SUMS; do
      [ -f "$src/$name" ] && [ ! -L "$src/$name" ] || backup_fail "missing/unsafe backup file: $name" || return
    done
    if [ "$preparing" != preparing ]; then
      [ -f "$src/COMPLETE" ] && [ ! -L "$src/COMPLETE" ] && [ "$(cat "$src/COMPLETE")" = mnema-backup-v1 ] || backup_fail 'incomplete backup' || return
    fi
    [ "$(head -n 1 "$src/manifest")" = format=mnema-backup-v1 ] || backup_fail 'unsupported backup format' || return
    actual=$(cd "$src" && backup_hashes postgres.sql.gz seaweedfs.tar.gz env manifest schema.txt compose.yaml)
    [ "$actual" = "$(cat "$src/SHA256SUMS")" ] || backup_fail 'backup checksum mismatch' || return
  else
    echo 'WARNING: explicit legacy restore; no manifest/checksums or consistency guarantee' >&2
  fi
  gzip -t "$src/postgres.sql.gz" || return
  gzip -t "$src/seaweedfs.tar.gz" || return
  # Refuse traversal and non-file/directory members before any live mutation.
  tar tzf "$src/seaweedfs.tar.gz" | awk '/^\// || /(^|\/)\.\.(\/|$)/ {bad=1} END {exit bad}' || backup_fail 'unsafe media archive paths' || return
  tar tvzf "$src/seaweedfs.tar.gz" | awk 'substr($0,1,1)!="-" && substr($0,1,1)!="d" {bad=1} END {exit bad}' || backup_fail 'media archive contains links/special files' || return
}
