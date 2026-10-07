#!/usr/bin/env bash
# Validate before mutation. --verify uses an isolated disposable PostgreSQL
# container; it verifies SQL import and archive integrity, not application UX.
set -euo pipefail
# shellcheck source=scripts/backup-common.sh
source "$(dirname "${BASH_SOURCE[0]}")/backup-common.sh"
src=${1:?usage: restore.sh <folder> [--yes|--verify] [--allow-legacy]}
shift
mode='' legacy=0
for arg in "$@"; do
  case "$arg" in
    --yes|--verify) [ -z "$mode" ] || backup_fail 'choose one restore mode'; mode=$arg ;;
    --allow-legacy) legacy=1 ;;
    *) backup_fail "unknown option: $arg" ;;
  esac
done
src=$(cd "$src" && pwd)
backup_validate "$src" "$legacy"
if [ "$mode" = --verify ]; then
  check_container=''
  # shellcheck disable=SC2329 # Invoked by the EXIT trap.
  cleanup_verify() {
    status=$?
    trap - EXIT INT TERM
    if [ -n "$check_container" ]; then docker rm -f -v "$check_container" >/dev/null || status=1; fi
    exit "$status"
  }
  trap cleanup_verify EXIT
  trap 'exit 130' INT
  trap 'exit 143' TERM
  # No published ports, network or live volumes. Trust auth is confined to
  # this throwaway container's Unix socket; all access uses docker exec.
  check_container=$(docker run --rm -d --network none --mount type=volume,dst=/var/lib/postgresql/data -e POSTGRES_HOST_AUTH_METHOD=trust -e POSTGRES_DB=mnema_verify postgres:17-alpine)
  ready=false
  for _ in $(seq 1 60); do
    if docker exec "$check_container" sh -c '[ "$(cat /proc/1/comm)" = postgres ] && pg_isready -U postgres -d mnema_verify' </dev/null >/dev/null 2>&1; then ready=true; break; fi
    sleep 1
  done
  [ "$ready" = true ] || backup_fail 'isolated verification database did not become ready'
  gunzip -c "$src/postgres.sql.gz" | docker exec -i "$check_container" psql -U postgres -d mnema_verify -v ON_ERROR_STOP=1 -q >/dev/null
  docker exec "$check_container" psql -U postgres -d mnema_verify -At -v ON_ERROR_STOP=1 -c "SELECT 'users ' || count(*) FROM users UNION ALL SELECT 'channels ' || count(*) FROM channels UNION ALL SELECT 'messages ' || count(*) FROM messages UNION ALL SELECT 'media ' || count(*) FROM media UNION ALL SELECT 'migrations ' || count(*) FROM schema_migrations" </dev/null
  echo 'structural verification passed; test login, object retrieval and device recovery on a separate restored deployment'
  exit 0
fi
if [ "$mode" != --yes ]; then
  read -r -p 'Replace live database and media? Type restore: ' answer
  [ "$answer" = restore ] || backup_fail 'aborted'
fi
maintenance_lock
app_resume=false seaweed_resume=false mutated=false
cleanup_restore() {
  status=$?
  trap - EXIT INT TERM
  storage_ready=true
  if [ "$mutated" = false ] || [ "$status" = 0 ]; then
    if [ "$seaweed_resume" = true ]; then
      if ! docker compose start --wait seaweedfs >/dev/null; then
        status=1 storage_ready=false
        echo 'seaweedfs did not resume; app remains stopped' >&2
      fi
    fi
    if [ "$app_resume" = true ] && [ "$storage_ready" = true ]; then docker compose start --wait app >/dev/null || status=1; fi
  else
    echo 'restore failed after mutation: services remain stopped; resolve or restore a complete backup before starting' >&2
  fi
  rmdir .mnema-maintenance.lock
  exit "$status"
}
trap cleanup_restore EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
app=$(service_id app)
seaweed=$(service_id seaweedfs)
postgres=$(service_id postgres)
[ "$(service_running "$postgres")" = true ] || backup_fail 'postgres must already be running'
app_resume=$(service_running "$app")
seaweed_resume=$(service_running "$seaweed")
if [ "$app_resume" = true ]; then docker compose stop -t 60 app >/dev/null; fi
if [ "$seaweed_resume" = true ]; then docker compose stop -t 60 seaweedfs >/dev/null; fi
[ "$(service_running "$app")" = false ] && [ "$(service_running "$seaweed")" = false ] || backup_fail 'writers did not stop'
mutated=true
docker compose exec -T postgres sh -c 'dropdb -U "$POSTGRES_USER" --if-exists --force "$POSTGRES_DB" && createdb -U "$POSTGRES_USER" "$POSTGRES_DB"' </dev/null
gunzip -c "$src/postgres.sql.gz" | docker compose exec -T postgres sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1 -q' >/dev/null
docker run --rm --network none --volumes-from "$seaweed" -v "$src":/in:ro alpine:3.24 sh -c 'find /data -mindepth 1 -delete && tar xzf /in/seaweedfs.tar.gz -C /data'
echo 'data restore complete; current .env and compose configuration were preserved. Review backed-up env/compose.yaml manually before changing secrets, endpoints or service definitions.'
