#!/usr/bin/env bash
# Cross-version steps of scripts/backup-drill.sh (sourced, not run): what the
# schema looks like after the new image migrated restored data, and that the
# old image then refuses to start on it, as doc/operations.md "Downgrading"
# says, without touching the data. Needs set_image and login from backup-drill.sh.

schema_state() {
  docker compose exec -T postgres sh -c "psql -U \"\$POSTGRES_USER\" -d \"\$POSTGRES_DB\" -Atc \"SELECT count(*) || ' ' || max(filename) FROM schema_migrations\"" </dev/null
}

user_count() {
  docker compose exec -T postgres sh -c "psql -U \"\$POSTGRES_USER\" -d \"\$POSTGRES_DB\" -Atc 'SELECT count(*) FROM users'" </dev/null
}

# check_migrated ROOT: the restored old data was migrated to the newest file.
check_migrated() {
  local latest state
  latest="$(find "$1/internal/db/migrations" -name '*.sql' -exec basename {} \; | sort | tail -n 1)"
  state="$(schema_state)"
  [ "${state#* }" = "$latest" ] || { echo "restored data is at ${state#* }, not $latest" >&2; return 1; }
}

# build_has_newer_migrations ROOT SOURCE_LATEST: the build ships a migration
# the source version lacks (filenames sort by their number). Only then is the
# data newer than the source image knows and must it refuse to start.
build_has_newer_migrations() {
  local latest
  latest="$(find "$1/internal/db/migrations" -name '*.sql' -exec basename {} \; | sort | tail -n 1)"
  [ "$latest" != "$2" ] && [ "$(printf '%s\n%s\n' "$latest" "$2" | sort | tail -n 1)" = "$latest" ]
}

# wait_for_refusal: the app container of the old image must exit with an error
# (the restart policy then starts it again, so a restart counts too).
wait_for_refusal() {
  local id status code restarts
  id="$(docker compose ps -aq app)"
  for _ in $(seq 1 90); do
    read -r status code restarts < <(docker inspect -f '{{.State.Status}} {{.State.ExitCode}} {{.RestartCount}}' "$id")
    if [ "$restarts" -ge 1 ] || { [ "$status" = exited ] && [ "$code" != 0 ]; }; then return 0; fi
    sleep 1
  done
  echo "the old image kept running on newer data (state: $status)" >&2
  return 1
}

# check_downgrade_refused OLD_IMAGE NEW_IMAGE
check_downgrade_refused() {
  local old="$1" new="$2" before after users logs
  before="$(schema_state)"
  users="$(user_count)"
  echo "==> downgrade check: the build has newer migrations than the source, so the old image must refuse"
  echo "==> downgrade: $old on data migrated by $new must refuse to start"
  set_image "$old"
  docker compose up -d --no-build app
  wait_for_refusal || { docker compose logs --no-color --tail 40 app >&2 || true; return 1; }
  logs="$(docker compose logs --no-color app 2>&1)"
  grep -qF 'migrated by a newer version' <<<"$logs" ||
    { echo "the old image failed, but not with the documented message:" >&2; tail -n 20 <<<"$logs" >&2; return 1; }
  if grep -qE '^panic:|goroutine [0-9]+ \[' <<<"$logs"; then echo "the old image panicked instead of refusing cleanly" >&2; return 1; fi
  docker compose stop -t 30 app >/dev/null
  after="$(schema_state)"
  [ "$after" = "$before" ] || { echo "the refused downgrade changed the schema: $before -> $after" >&2; return 1; }
  [ "$(user_count)" = "$users" ] || { echo "the refused downgrade changed the data" >&2; return 1; }
  echo "the old image refused with the documented message and left the data alone"
  set_image "$new"
  docker compose up -d --no-build --wait --wait-timeout 180
  login
}

# check_same_schema_runs OLD_IMAGE NEW_IMAGE: the build adds no migration, so
# the restored data is not newer and the old image must start healthy on it.
check_same_schema_runs() {
  local old="$1" new="$2" before after users
  before="$(schema_state)"
  users="$(user_count)"
  echo "==> downgrade check skipped: the build has no migration beyond the source, so $old must run on the data migrated by $new"
  set_image "$old"
  docker compose up -d --no-build --wait --wait-timeout 180 app
  login
  docker compose stop -t 30 app >/dev/null
  after="$(schema_state)"
  [ "$after" = "$before" ] || { echo "the old image changed the schema: $before -> $after" >&2; return 1; }
  [ "$(user_count)" = "$users" ] || { echo "the old image changed the data" >&2; return 1; }
  echo "the old image started healthy on the same schema and left the data alone"
  set_image "$new"
  docker compose up -d --no-build --wait --wait-timeout 180
  login
}
