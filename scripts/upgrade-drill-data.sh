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
  MOD_ROLE="$(post_json Herzog /api/admin/roles "{\"name\":\"drillmods\",\"color\":3447003,\"mentionable\":true,\"permissions\":$PERM_MEMBER}" | jq -r '.id')"
  VIEW_ROLE="$(post_json Herzog /api/admin/roles '{"name":"drillviewers","color":10066329,"mentionable":false,"permissions":2}' | jq -r '.id')"
  need_id "$MOD_ROLE" 'role id'
  need_id "$VIEW_ROLE" 'role id'
  ALICE_ID="$(user_id alice)"
  BOB_ID="$(user_id bob)"
  put_json Herzog "/api/admin/users/$ALICE_ID/roles" "{\"role_ids\":[\"$MOD_ROLE\"]}" >/dev/null
  put_json Herzog "/api/admin/users/$BOB_ID/roles" "{\"role_ids\":[\"$VIEW_ROLE\"]}" >/dev/null
  put_json Herzog "/api/admin/channels/$PROJECTS/overwrites" \
    "{\"overwrites\":[{\"type\":\"role\",\"id\":\"$MOD_ROLE\",\"allow\":8,\"deny\":0},{\"type\":\"member\",\"id\":\"$BOB_ID\",\"allow\":0,\"deny\":8}]}" >/dev/null
  put_json Herzog "/api/admin/categories/$CATEGORY/overwrites" \
    "{\"overwrites\":[{\"type\":\"role\",\"id\":\"$MOD_ROLE\",\"allow\":16,\"deny\":0}]}" >/dev/null
}

seed_integrations() {
  local hook bot
  hook="$(post_json Herzog "/api/admin/channels/$GENERAL/webhooks" '{"name":"drill-hook"}')"
  HOOK_ID="$(jq -r '.id' <<<"$hook")"
  HOOK_TOKEN="$(jq -r '.token' <<<"$hook")"
  need_id "$HOOK_ID" 'webhook id'
  need_id "$HOOK_TOKEN" 'webhook token'
  hook_post "hook-before-$TAG"
  bot="$(post_json Herzog /api/admin/bots '{"name":"drillbot","allowed_intents":0}')"
  BOT_ID="$(jq -r '.id' <<<"$bot")"
  need_id "$BOT_ID" 'bot id'
  BOT_SECRET="$(post_json Herzog "/api/admin/bots/$BOT_ID/tokens" '{"name":"drill"}' | jq -r '.secret')"
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
  put_json Herzog /api/admin/onboarding \
    "{\"community_name\":\"Drill Community\",\"welcome_text\":\"welcome-$TAG\",\"rules_text\":\"rules-$TAG\",\"start_channel_id\":\"$GENERAL\"}" >/dev/null
  version="$(api alice "$BASE/api/onboarding" | jq -r '.rules_version')"
  for person in alice bob; do post_json "$person" /api/onboarding/accept "{\"version\":$version}" >/dev/null; done
  post_message Herzog "$GENERAL" "ping @drillmods $TAG"
}

snapshot_integrations() {
  ROLES_BEFORE="$(api Herzog "$BASE/api/roles" | jq -S 'map({id, name, color, permissions, mentionable, position}) | sort_by(.id)')"
  OVERWRITES_BEFORE="$(api Herzog "$BASE/api/admin/channels/$PROJECTS/overwrites" | jq -S '.overwrites | sort_by(.id)')"
  CATEGORY_OVERWRITES_BEFORE="$(api Herzog "$BASE/api/admin/categories/$CATEGORY/overwrites" | jq -S '.overwrites | sort_by(.id)')"
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
  [ "$(api Herzog "$BASE/api/roles" | jq -S 'map({id, name, color, permissions, mentionable, position}) | sort_by(.id)')" = "$ROLES_BEFORE" ] ||
    fail 'the roles changed'
  [ "$(api Herzog "$BASE/api/admin/channels/$PROJECTS/overwrites" | jq -S '.overwrites | sort_by(.id)')" = "$OVERWRITES_BEFORE" ] ||
    fail 'the channel overwrites changed'
  [ "$(api Herzog "$BASE/api/admin/categories/$CATEGORY/overwrites" | jq -S '.overwrites | sort_by(.id)')" = "$CATEGORY_OVERWRITES_BEFORE" ] ||
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
  [ "$(api Herzog "$BASE/api/admin/channels/$GENERAL/webhooks" | jq -r '[.[] | select(.name == "drill-hook")] | length')" = 1 ] ||
    fail 'the webhook is gone'
  hook_post "hook-after-$TAG"
  messages Herzog "$GENERAL" | grep -qx "hook-after-$TAG" || fail 'the old webhook token no longer posts'
  [ "$(api Herzog "$BASE/api/admin/bots" | jq -r '[.[] | select(.name == "drillbot")] | length')" = 1 ] || fail 'the bot is gone'
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

# Every leg also writes through the tables of 0035-0037, whatever it started on.
# Last, because it raises the rules version, which blocks members who have not
# accepted from posting.
exercise_new_tables() {
  put_json bob "/api/categories/$CATEGORY/notifications" '{"level":"mentions"}' >/dev/null
  [ "$(api bob "$BASE/api/notification-settings" | jq -r --arg c "$CATEGORY" '.categories[] | select(.category_id == $c) | .level')" = mentions ] ||
    fail 'a category level could not be set after the upgrade'
  put_json Herzog /api/admin/onboarding \
    "{\"community_name\":\"Drill Community\",\"welcome_text\":\"new-$TAG\",\"rules_text\":\"new-rules-$TAG\",\"start_channel_id\":\"$GENERAL\",\"require_reacceptance\":true}" >/dev/null
  [ "$(api alice "$BASE/api/onboarding" | jq -r '.rules_pending')" = true ] || fail 'raising the rules version did not ask alice to accept again'
  post_message Herzog "$GENERAL" "after @drillmods $TAG"
  api alice "$BASE/api/mentions" | jq -r '.messages[].content' | grep -qx "after @drillmods $TAG" ||
    fail 'a role mention made after the upgrade did not reach alice'
}

verify_integration_data() {
  verify_roles
  verify_integrations
  verify_notifications
  verify_onboarding_and_mentions
}
