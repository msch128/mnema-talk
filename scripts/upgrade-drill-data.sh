#!/usr/bin/env bash
# Seed and verification steps of scripts/upgrade-drill.sh (sourced, not run).
# The seed goes through the API shapes of the release being upgraded from:
# roles, overwrites, a webhook, a bot and per-channel notification levels exist
# since 0.8; category notification levels, role mentions and onboarding
# (migrations 0035-0037) only since 0.9.0, so those are seeded only from there.
# Needs the helpers and variables of upgrade-drill.sh (api, post_json, put_json,
# login, jar, messages, BASE, DEPLOY, FROM_VERSION, GENERAL, PROJECTS, CATEGORY,
# TAG) and scripts/drill-common.sh.

# Permission bits (internal/perm/perm.go): view 2, send 4, attach files 8,
# react 16.
PERM_MEMBER=22

fail() { echo "$*" >&2; exit 1; }
need_id() { [ -n "$1" ] && [ "$1" != null ] || fail "seeding failed: no $2"; }

# db_count TABLE: row count straight from PostgreSQL.
db_count() {
  docker compose exec -T postgres sh -c "psql -U \"\$POSTGRES_USER\" -d \"\$POSTGRES_DB\" -Atc 'SELECT count(*) FROM $1'" </dev/null
}

# Tables that exist in every release from 0.8 on, and those added by 0.9.0.
COUNT_TABLES_ALL='users categories channels messages roles user_roles permission_overwrites webhooks bots bot_tokens channel_reads message_mentions'
COUNT_TABLES_090='category_notify message_role_mentions'

count_tables() {
  local t tables="$COUNT_TABLES_ALL"
  if drill_version_ge "$FROM_VERSION" 0.9.0; then tables="$tables $COUNT_TABLES_090"; fi
  for t in $tables; do echo "$t $(db_count "$t")"; done
}

user_id() { api "$1" "$BASE/api/auth/me" | jq -r '.id // .user.id'; }

seed_roles() {
  MOD_ROLE="$(post_json Cool_Guy /api/admin/roles "{\"name\":\"drillmods\",\"color\":3447003,\"mentionable\":true,\"permissions\":$PERM_MEMBER}" | jq -r '.id')"
  VIEW_ROLE="$(post_json Cool_Guy /api/admin/roles '{"name":"drillviewers","color":10066329,"mentionable":false,"permissions":2}' | jq -r '.id')"
  need_id "$MOD_ROLE" 'role id'
  need_id "$VIEW_ROLE" 'role id'
  ALICE_ID="$(user_id alice)"
  BOB_ID="$(user_id bob)"
  put_json Cool_Guy "/api/admin/users/$ALICE_ID/roles" "{\"role_ids\":[\"$MOD_ROLE\"]}" >/dev/null
  put_json Cool_Guy "/api/admin/users/$BOB_ID/roles" "{\"role_ids\":[\"$VIEW_ROLE\"]}" >/dev/null
  put_json Cool_Guy "/api/admin/channels/$PROJECTS/overwrites" \
    "{\"overwrites\":[{\"type\":\"role\",\"id\":\"$MOD_ROLE\",\"allow\":8,\"deny\":0},{\"type\":\"member\",\"id\":\"$BOB_ID\",\"allow\":0,\"deny\":8}]}" >/dev/null
  put_json Cool_Guy "/api/admin/categories/$CATEGORY/overwrites" \
    "{\"overwrites\":[{\"type\":\"role\",\"id\":\"$MOD_ROLE\",\"allow\":16,\"deny\":0}]}" >/dev/null
}

seed_integrations() {
  local hook bot
  hook="$(post_json Cool_Guy "/api/admin/channels/$GENERAL/webhooks" '{"name":"drill-hook"}')"
  HOOK_ID="$(jq -r '.id' <<<"$hook")"
  HOOK_TOKEN="$(jq -r '.token' <<<"$hook")"
  need_id "$HOOK_ID" 'webhook id'
  need_id "$HOOK_TOKEN" 'webhook token'
  hook_post "hook-before-$TAG"
  bot="$(post_json Cool_Guy /api/admin/bots '{"name":"drillbot","allowed_intents":0}')"
  BOT_ID="$(jq -r '.id' <<<"$bot")"
  need_id "$BOT_ID" 'bot id'
  BOT_SECRET="$(post_json Cool_Guy "/api/admin/bots/$BOT_ID/tokens" '{"name":"drill"}' | jq -r '.secret')"
  need_id "$BOT_SECRET" 'bot token'
}

hook_post() { # content: incoming webhooks need no session, the token is the credential
  curl -fsS -H 'Content-Type: application/json' --data "{\"content\":\"$1\"}" "$BASE/api/webhooks/$HOOK_ID/$HOOK_TOKEN" >/dev/null
}

seed_notifications() {
  put_json bob "/api/channels/$GENERAL/notifications" '{"level":"mentions"}' >/dev/null
  put_json bob "/api/channels/$PROJECTS/notifications" '{"level":"mute"}' >/dev/null
}

# Only for sources that already have the 0.9.0 tables.
seed_090() {
  local version
  put_json bob "/api/categories/$CATEGORY/notifications" '{"level":"mute"}' >/dev/null
  put_json Cool_Guy /api/admin/onboarding \
    "{\"community_name\":\"Drill Community\",\"welcome_text\":\"welcome-$TAG\",\"rules_text\":\"rules-$TAG\",\"start_channel_id\":\"$GENERAL\"}" >/dev/null
  version="$(api alice "$BASE/api/onboarding" | jq -r '.rules_version')"
  for person in alice bob; do post_json "$person" /api/onboarding/accept "{\"version\":$version}" >/dev/null; done
  post_message Cool_Guy "$GENERAL" "ping @drillmods $TAG"
}

# Roles as the drill compares them. Two upgrades add a permission on purpose,
# so those bits are left out of the comparison and checked on their own in
# verify_roles: migration 0050 gives Send direct messages (bit 1 << 22) to
# @everyone, and migration 0070 gives Manage events (bit 1 << 25) to every role
# that holds Timeout members (bit 1 << 18).
ROLES_JQ='map({id, name, color, permissions: (.permissions as $p | (if (($p / 4194304 | floor) % 2) == 1 then $p - 4194304 else $p end) as $q | if (($q / 33554432 | floor) % 2) == 1 then $q - 33554432 else $q end), mentionable, position}) | sort_by(.id)'
# Holds Manage events exactly when it can time members out.
EVENTS_GRANT_JQ='all(.[]; (((.permissions / 33554432 | floor) % 2) == 1) == (((.permissions / 262144 | floor) % 2) == 1))'

snapshot_integrations() {
  ROLES_BEFORE="$(api Cool_Guy "$BASE/api/roles" | jq -S "$ROLES_JQ")"
  OVERWRITES_BEFORE="$(api Cool_Guy "$BASE/api/admin/channels/$PROJECTS/overwrites" | jq -S '.overwrites | sort_by(.id)')"
  CATEGORY_OVERWRITES_BEFORE="$(api Cool_Guy "$BASE/api/admin/categories/$CATEGORY/overwrites" | jq -S '.overwrites | sort_by(.id)')"
  COUNTS_BEFORE="$(count_tables)"
}

seed_integration_data() {
  seed_roles
  seed_integrations
  seed_notifications
  if drill_version_ge "$FROM_VERSION" 0.9.0; then seed_090; fi
  snapshot_integrations
}

# --- after the upgrade -------------------------------------------------------
verify_counts() {
  local after
  after="$(count_tables)"
  [ "$after" = "$COUNTS_BEFORE" ] || fail "row counts changed by the upgrade:
$(diff <(echo "$COUNTS_BEFORE") <(echo "$after") || true)"
}

verify_roles() {
  [ "$(api Cool_Guy "$BASE/api/roles" | jq -S "$ROLES_JQ")" = "$ROLES_BEFORE" ] ||
    fail 'the roles changed'
  api Cool_Guy "$BASE/api/roles" | jq -e 'any(.[]; ((.permissions / 4194304 | floor) % 2) == 1)' >/dev/null ||
    fail 'no role holds Send direct messages after the upgrade (migration 0050 gives it to @everyone)'
  api Cool_Guy "$BASE/api/roles" | jq -e "$EVENTS_GRANT_JQ" >/dev/null ||
    fail 'Manage events does not match Timeout members after the upgrade (migration 0070)'
  [ "$(api Cool_Guy "$BASE/api/admin/channels/$PROJECTS/overwrites" | jq -S '.overwrites | sort_by(.id)')" = "$OVERWRITES_BEFORE" ] ||
    fail 'the channel overwrites changed'
  [ "$(api Cool_Guy "$BASE/api/admin/categories/$CATEGORY/overwrites" | jq -S '.overwrites | sort_by(.id)')" = "$CATEGORY_OVERWRITES_BEFORE" ] ||
    fail 'the category overwrites changed'
  # The overwrite still bites: bob may not attach files in the project channel.
  # Deterministic binary content (sniffs as application/octet-stream); random
  # bytes occasionally match a disallowed signature and the upload got a 415.
  { printf 'MNEMA-DRILL\000'; head -c 1012 /dev/zero | LC_ALL=C tr '\000' '\001'; } >"$DEPLOY/small.bin"
  if api bob -F "file=@$DEPLOY/small.bin;type=application/octet-stream" "$BASE/api/channels/$PROJECTS/upload" >/dev/null 2>&1; then
    fail 'bob could upload despite the member overwrite'
  fi
  api alice -F "file=@$DEPLOY/small.bin;type=application/octet-stream" "$BASE/api/channels/$PROJECTS/upload" >/dev/null ||
    fail 'alice (role overwrite) can no longer upload'
}

verify_integrations() {
  [ "$(api Cool_Guy "$BASE/api/admin/channels/$GENERAL/webhooks" | jq -r '[.[] | select(.name == "drill-hook")] | length')" = 1 ] ||
    fail 'the webhook is gone'
  hook_post "hook-after-$TAG"
  messages Cool_Guy "$GENERAL" | grep -qx "hook-after-$TAG" || fail 'the old webhook token no longer posts'
  [ "$(api Cool_Guy "$BASE/api/admin/bots" | jq -r '[.[] | select(.name == "drillbot")] | length')" = 1 ] || fail 'the bot is gone'
  [ "$(curl -fsS -H "Authorization: Bot $BOT_SECRET" "$BASE/api/bot/v1/users/@me" | jq -r '.username // .name')" = drillbot ] ||
    fail 'the old bot token no longer authenticates'
}

verify_notifications() {
  local settings
  settings="$(api bob "$BASE/api/notification-settings")"
  [ "$(jq -r --arg c "$GENERAL" '.channels[] | select(.channel_id == $c) | .level' <<<"$settings")" = mentions ] ||
    fail 'the channel level "mentions" was not kept'
  [ "$(jq -r --arg c "$PROJECTS" '.channels[] | select(.channel_id == $c) | .level' <<<"$settings")" = mute ] ||
    fail 'the channel level "mute" was not kept'
  if drill_version_ge "$FROM_VERSION" 0.9.0; then
    [ "$(jq -r --arg c "$CATEGORY" '.categories[] | select(.category_id == $c) | .level' <<<"$settings")" = mute ] ||
      fail 'the category level was not kept'
  fi
}

verify_onboarding_and_mentions() {
  local state
  if drill_version_ge "$FROM_VERSION" 0.9.0; then
    state="$(api alice "$BASE/api/onboarding")"
    [ "$(jq -r '.welcome_text' <<<"$state")" = "welcome-$TAG" ] || fail 'the onboarding text was not kept'
    [ "$(jq -r '.rules_pending' <<<"$state")" = false ] || fail 'the accepted rules are pending again'
    api alice "$BASE/api/mentions" | jq -r '.messages[].content' | grep -qx "ping @drillmods $TAG" ||
      fail 'the role mention is gone from the mentions list'
  else
    api alice "$BASE/api/onboarding" >/dev/null || fail 'GET /api/onboarding failed on the migrated database'
  fi
}

# Direct messages, blocks, saved messages and health features added in 1.1
# (migrations 0049-0051). Exercises new features against the upgraded data.
exercise_110_features() {
  local health dm dm_id dm_msg dm_msg_id bob_dms

  health="$(curl -fsS "$BASE/api/health")"
  drill_health_features_ok "$health" dm blocks ||
    fail 'GET /api/health does not report dm and blocks features'

  ALICE_ID="${ALICE_ID:-$(user_id alice)}"
  BOB_ID="${BOB_ID:-$(user_id bob)}"
  need_id "$ALICE_ID" 'alice user id'
  need_id "$BOB_ID" 'bob user id'

  # 1:1 direct message between alice and bob, with one message from alice.
  dm="$(post_json alice /api/dms "{\"user_ids\":[\"$BOB_ID\"]}")"
  dm_id="$(jq -r '.id' <<<"$dm")"
  need_id "$dm_id" 'direct message id'

  dm_msg="$(post_json alice "/api/channels/$dm_id/messages" "{\"content\":\"dm-$TAG\"}")"
  dm_msg_id="$(jq -r '.id' <<<"$dm_msg")"
  need_id "$dm_msg_id" 'direct message message id'

  # Bob sees the conversation with 1 unread message before blocking.
  bob_dms="$(api bob "$BASE/api/dms")"
  [ "$(jq -r --arg id "$dm_id" '.[] | select(.id == $id) | .unread_count // empty' <<<"$bob_dms")" = 1 ] ||
    fail 'GET /api/dms did not report expected unread_count 1 before block'

  # Bob blocks alice, then checks the conversation still lists with unread_count 0.
  put_json bob "/api/me/blocks/$ALICE_ID" '{}' >/dev/null
  [ "$(api bob "$BASE/api/me/blocks" | jq -r '.[].user_id' | grep -cx "$ALICE_ID")" = 1 ] ||
    fail 'bob block of alice not listed in GET /api/me/blocks'

  bob_dms="$(api bob "$BASE/api/dms")"
  [ "$(jq -r --arg id "$dm_id" '.[] | select(.id == $id) | .unread_count // empty' <<<"$bob_dms")" = 0 ] ||
    fail 'GET /api/dms did not report expected unread_count 0 after blocking author'

  # Bob saves alice's direct message.
  put_json bob "/api/me/saved/$dm_msg_id" '{"note":"drill-saved"}' >/dev/null
  [ "$(api bob "$BASE/api/me/saved" | jq -r --arg id "$dm_msg_id" '.items[] | select(.message_id == $id) | .note // empty')" = 'drill-saved' ] ||
    fail 'saved direct message not found in /api/me/saved'
}

# Every leg also writes through the tables of 0035-0037, whatever it started on.
# Last, because it raises the rules version, which blocks members who have not
# accepted from posting.
exercise_new_tables() {
  put_json bob "/api/categories/$CATEGORY/notifications" '{"level":"mentions"}' >/dev/null
  [ "$(api bob "$BASE/api/notification-settings" | jq -r --arg c "$CATEGORY" '.categories[] | select(.category_id == $c) | .level')" = mentions ] ||
    fail 'a category level could not be set after the upgrade'
  put_json Cool_Guy /api/admin/onboarding \
    "{\"community_name\":\"Drill Community\",\"welcome_text\":\"new-$TAG\",\"rules_text\":\"new-rules-$TAG\",\"start_channel_id\":\"$GENERAL\",\"require_reacceptance\":true}" >/dev/null
  [ "$(api alice "$BASE/api/onboarding" | jq -r '.rules_pending')" = true ] || fail 'raising the rules version did not ask alice to accept again'
  post_message Cool_Guy "$GENERAL" "after @drillmods $TAG"
  api alice "$BASE/api/mentions" | jq -r '.messages[].content' | grep -qx "after @drillmods $TAG" ||
    fail 'a role mention made after the upgrade did not reach alice'
}

verify_integration_data() {
  verify_roles
  verify_integrations
  verify_notifications
  verify_onboarding_and_mentions
}
