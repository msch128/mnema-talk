#!/usr/bin/env bash
# Online leg of scripts/backup-drill.sh (sourced): BACKUP_MODE=online runs
# while a member keeps posting messages with uploads. The app must never
# stop, every request must succeed, and after restoring that backup every
# attachment of every restored message must open with the bytes that were
# sent. With `age` installed the backup is encrypted and restored from the
# encrypted file as well.

# One upload per round until the stop file appears; uploads.log lists the
# rounds the server acknowledged, failures the requests that failed.
drill_writer() {
  local dir=$1 i=0 url
  while [ ! -e "$dir/stop" ]; do
    i=$((i + 1))
    { printf 'MNEMA-ONLINE-%s\000' "$i"; head -c 2048 /dev/zero | LC_ALL=C tr '\000' '\002'; } >"$dir/$i.bin"
    url="$(api -F "file=@$dir/$i.bin;type=application/octet-stream" -F "content=online-$i" \
      "$BASE/api/channels/$CHANNEL/upload" | jq -r '.attachments[0].url // empty')" || url=''
    if [ -n "$url" ]; then echo "$i" >>"$dir/uploads.log"; else echo "upload $i" >>"$dir/failures"; fi
    post_message "online-text-$i" || echo "message $i" >>"$dir/failures"
    # Uploads are limited to 60 a minute per member.
    sleep 1.5
  done
}

# Every top-level message of the channel as one JSON array, oldest page last.
drill_all_messages() {
  local page before='' out='[]'
  while :; do
    page="$(api "$BASE/api/channels/$CHANNEL/messages?limit=100${before:+&before=$before}" | jq -c '[.. | objects | select(has("content") and has("attachments"))]')"
    [ "$(jq length <<<"$page")" -gt 0 ] || break
    out="$(jq -c -n --argjson a "$out" --argjson b "$page" '$a + $b | unique_by(.id)')"
    before="$(jq -r 'min_by(.created_at) | .id' <<<"$page")"
    [ "$(jq length <<<"$page")" -ge 100 ] || break
  done
  printf '%s\n' "$out"
}

# Opens every attachment of every message and compares the ones this drill
# uploaded with what it sent. Prints how many attachments it opened.
drill_check_attachments() {
  local dir=$1 all opened=0 content url sent
  all="$(drill_all_messages)"
  while IFS=$'\t' read -r content url; do
    [ -n "$url" ] || continue
    api "$BASE$url" -o "$dir/restored.bin" || { echo "attachment of \"$content\" does not open after the restore" >&2; return 1; }
    case "$content" in
      online-[0-9]*) sent="$dir/${content#online-}.bin" ;;
      'drill upload') sent="$DEPLOY/payload.bin" ;;
      *) sent='' ;;
    esac
    if [ -n "$sent" ] && ! cmp -s "$sent" "$dir/restored.bin"; then
      echo "attachment of \"$content\" differs after the restore" >&2
      return 1
    fi
    opened=$((opened + 1))
  done < <(jq -r '.[] | .content as $c | .attachments[]? | [$c, .url] | @tsv' <<<"$all")
  echo "$opened"
}

drill_online() {
  local dir="$DEPLOY/writer" writer started sent_before during snapshot opened messages_all i
  local app="$COMPOSE_PROJECT_NAME-app" seaweed="$COMPOSE_PROJECT_NAME-seaweedfs"
  mkdir "$dir"
  login
  started="$(docker inspect -f '{{.State.StartedAt}} {{.State.Pid}}' "$app")"

  echo "==> online backup while a member posts messages and uploads"
  drill_writer "$dir" &
  writer=$!
  # Writes are under way before the backup starts.
  # The first upload after a restart can take a few seconds.
  for _ in $(seq 1 300); do [ "$(wc -l 2>/dev/null <"$dir/uploads.log" || echo 0)" -ge 3 ] && break; sleep 0.2; done
  sent_before="$(wc -l <"$dir/uploads.log" | tr -d ' ')"
  [ "$sent_before" -ge 3 ] || { echo "the writer did not get three uploads through before the backup" >&2; return 1; }
  rm -rf "$BACKUP_DIR"
  if command -v age >/dev/null 2>&1 && command -v age-keygen >/dev/null 2>&1; then
    age-keygen -o "$DEPLOY/age-identity" 2>/dev/null
    BACKUP_AGE_RECIPIENT="$(age-keygen -y "$DEPLOY/age-identity")" BACKUP_MODE=online scripts/backup.sh
    snapshot="$(find "$BACKUP_DIR" -mindepth 1 -maxdepth 1 -type f -name '20*.tar.age' | head -n 1)"
    [ -n "$snapshot" ] || { echo "no encrypted backup was written" >&2; return 1; }
    [ -z "$(find "$BACKUP_DIR" -mindepth 1 -maxdepth 1 -type d)" ] || { echo "the unencrypted backup was left behind" >&2; return 1; }
    if grep -aq "MNEMA-DRILL\|JWT_SECRET" "$snapshot"; then echo "the encrypted backup holds readable data" >&2; return 1; fi
    export BACKUP_AGE_IDENTITY="$DEPLOY/age-identity"
    echo "==> the backup is encrypted: $(basename "$snapshot")"
  else
    echo "==> age is not installed: the encrypted leg is skipped"
    BACKUP_MODE=online scripts/backup.sh
    snapshot="$(find "$BACKUP_DIR" -mindepth 1 -maxdepth 1 -type d -name '20*' | head -n 1)"
    grep -qx 'mode=online' "$snapshot/manifest" || { echo "the manifest does not say mode=online" >&2; return 1; }
  fi
  during=$(($(wc -l <"$dir/uploads.log" | tr -d ' ') - sent_before))
  touch "$dir/stop"
  wait "$writer"

  [ ! -s "$dir/failures" ] || { echo "requests failed during the online backup:" >&2; cat "$dir/failures" >&2; return 1; }
  [ "$during" -gt 0 ] || { echo "no upload was acknowledged while the backup ran" >&2; return 1; }
  [ "$(docker inspect -f '{{.State.StartedAt}} {{.State.Pid}}' "$app")" = "$started" ] \
    || { echo "the app was stopped or restarted by the online backup" >&2; return 1; }
  [ "$(docker inspect -f '{{.State.Paused}}' "$seaweed")" = false ] || { echo "seaweedfs was left paused" >&2; return 1; }
  jq -e '.restore_test.result == "passed"' backup-status/status.json >/dev/null \
    || { echo "the online backup did not report a passed restore test" >&2; return 1; }
  echo "==> the app never stopped; uploads acknowledged: $sent_before before the backup, $during while it ran, none failed"

  echo "==> restore.sh --verify and --yes on the online backup"
  scripts/restore.sh "$snapshot" --verify
  scripts/restore.sh "$snapshot" --yes
  [ -z "$(find "$BACKUP_DIR" -mindepth 1 -maxdepth 1 -name '.restore-*')" ] || { echo "restore left a decrypted copy behind" >&2; return 1; }
  docker compose up -d --no-build --wait --wait-timeout 180
  login
  messages | grep -qx "$BEFORE" || { echo "seeded message missing after the online restore" >&2; return 1; }
  # Everything acknowledged before the backup began is in it.
  messages_all="$(drill_all_messages | jq -r '.[].content')"
  for i in $(seq 1 "$sent_before"); do
    grep -qx "online-$i" <<<"$messages_all" || { echo "upload $i from before the backup is missing after the restore" >&2; return 1; }
  done
  opened="$(drill_check_attachments "$dir")"
  [ "$opened" -gt "$sent_before" ] || { echo "only $opened attachments restored, $sent_before were sent before the backup" >&2; return 1; }
  echo "==> online restore: all $opened attachments of the restored messages open and match"
}
