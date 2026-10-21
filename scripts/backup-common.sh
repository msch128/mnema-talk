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
validate_safe_file() {
  local file="$1/$2"
  if [ ! -f "$file" ] || [ -L "$file" ]; then
    backup_fail "missing/unsafe backup file: $2"
    return 1
  fi
}

validate_required_files() {
  local src="$1"
  local name
  for name in postgres.sql.gz seaweedfs.tar.gz env; do
    validate_safe_file "$src" "$name" || return 1
  done
}

validate_manifest_files() {
  local src="$1"
  local name
  for name in manifest schema.txt compose.yaml SHA256SUMS; do
    validate_safe_file "$src" "$name" || return 1
  done
}

validate_backup_completeness() {
  local src="$1"
  local preparing="$2"
  if [ "$preparing" = preparing ]; then
    return 0
  fi
  local complete="$src/COMPLETE"
  if [ ! -f "$complete" ] || [ -L "$complete" ]; then
    backup_fail 'incomplete backup'
    return 1
  fi
  if [ "$(< "$complete")" != mnema-backup-v1 ]; then
    backup_fail 'incomplete backup'
    return 1
  fi
}

validate_manifest_and_checksums() {
  local src="$1"
  if [ "$(head -n 1 "$src/manifest")" != format=mnema-backup-v1 ]; then
    backup_fail 'unsupported backup format'
    return 1
  fi
  local actual
  actual=$(cd "$src" && backup_hashes postgres.sql.gz seaweedfs.tar.gz env manifest schema.txt compose.yaml)
  if [ "$actual" != "$(< "$src/SHA256SUMS")" ]; then
    backup_fail 'backup checksum mismatch'
    return 1
  fi
}

is_v1_backup() {
  local src="$1"
  local legacy="$2"
  if [ "$legacy" != 1 ]; then
    return 0
  fi
  if [ -e "$src/COMPLETE" ] || [ -e "$src/manifest" ]; then
    return 0
  fi
  return 1
}

validate_media_archive() {
  local tarball="$1/seaweedfs.tar.gz"
  if ! tar tzf "$tarball" | awk '/^\// || /(^|\/)\.\.(\/|$)/ {bad=1} END {exit bad}'; then
    backup_fail 'unsafe media archive paths'
    return 1
  fi
  if ! tar tvzf "$tarball" | awk 'substr($0,1,1)!="-" && substr($0,1,1)!="d" {bad=1} END {exit bad}'; then
    backup_fail 'media archive contains links/special files'
    return 1
  fi
}

validate_compressed_archives() {
  local src="$1"
  gzip -t "$src/postgres.sql.gz" || return 1
  gzip -t "$src/seaweedfs.tar.gz" || return 1
  validate_media_archive "$src" || return 1
}

validate_v1_backup_if_needed() {
  local src="$1"
  local legacy="$2"
  local preparing="$3"
  if is_v1_backup "$src" "$legacy"; then
    validate_manifest_files "$src" || return 1
    validate_backup_completeness "$src" "$preparing" || return 1
    validate_manifest_and_checksums "$src" || return 1
  else
    echo 'WARNING: explicit legacy restore; no manifest/checksums or consistency guarantee' >&2
  fi
}

backup_validate() {
  local src=$1 legacy=$2 preparing=${3:-}
  validate_required_files "$src" || return 1
  validate_v1_backup_if_needed "$src" "$legacy" "$preparing" || return 1
  validate_compressed_archives "$src" || return 1
}
