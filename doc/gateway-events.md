# Gateway events and their payloads

Every dispatch the [bot gateway](bots.md#gateway) sends, with the `d` of its
frame. The frame is always `{"op": 0, "t": "NAME", "s": 12, "d": {...}}`; the
examples show `d` only. Ids are UUIDs, times are RFC 3339 in UTC.

The shapes come from the server's own types: the message is the one the REST API
returns (`api/openapi.json`, tag *Bot API*), and the other payloads are the Go
types in `internal/gateway`. They are named in
[`src/types/gateway.ts` of the TypeScript SDK](https://github.com/mnema-xyz/bot-sdk-js/blob/main/src/types/gateway.ts),
the Go SDK in [`sdk/go/types.go`](../sdk/go/types.go).

| Event | Intent | Sent when |
|---|---|---|
| [`READY`](#ready) | none | the session started (IDENTIFY accepted); always the first dispatch |
| [`RESUMED`](#resumed) | none | a RESUME succeeded and the missed events were replayed |
| [`MESSAGE_CREATE`](#message_create-and-message_update) | `GUILD_MESSAGES` (512) | a message was posted in a channel the bot can view, the bot's own included |
| [`MESSAGE_UPDATE`](#message_create-and-message_update) | `GUILD_MESSAGES` | a message changed: an edit, a pin or unpin, a change to a forum post |
| [`MESSAGE_DELETE`](#message_delete) | `GUILD_MESSAGES` | a message was deleted |
| [`MESSAGE_REACTIONS_UPDATE`](#message_reactions_update) | `GUILD_MESSAGE_REACTIONS` (1024) | a reaction was added or removed |
| [`CHANNELS_UPDATE`](#channels_update) | `GUILDS` (1) | the channel layout, or what the bot may see of it, changed |
| [`GUILD_MEMBER_ADD`](#guild_member_add) | `GUILD_MEMBERS` (2) | a new member registered and shares a channel with the bot |
| [`INTERACTION_CREATE`](#interaction_create) | none | a member ran one of the bot's slash commands, used one of its components or submitted its modal |

Which channels an event comes from is not an intent: an event of a channel the
bot cannot view is never sent, and the check runs when the event is sent.

## READY

```json
{
  "v": 1,
  "user": {
    "id": "00000000-0000-4000-8000-000000000030",
    "bot_id": "00000000-0000-4000-8000-000000000031",
    "username": "Dice",
    "handle": "bot-dice",
    "bot": true,
    "avatar_url": "/api/media/..."
  },
  "session_id": "...",
  "intents": 512,
  "resume_gateway_url": "wss://talk.example.org/api/bot/v1/gateway",
  "heartbeat_interval": 41250
}
```

`user.id` is the bot's member id: the author of its messages and the id that
appears in a message's `mentions` when someone mentions the bot. `user.bot_id`
is the id of the bot record (the `application_id` of its interactions).
`username` is the name members see, `handle` the unique name they mention
(`@bot-...`); `avatar_url` is absent without an avatar. `intents` are the intents
granted, `v` the gateway version.

## RESUMED

```json
{}
```

The frame's `s` is the session's current sequence number; `RESUMED` does not
use one of its own.

## MESSAGE_CREATE and MESSAGE_UPDATE

`d` is the message, exactly as `GET /channels/{id}/messages/{id}` returns it:

```json
{
  "id": "00000000-0000-4000-8000-000000000001",
  "number": 4821,
  "channel_id": "00000000-0000-4000-8000-000000000010",
  "user_id": "00000000-0000-4000-8000-000000000020",
  "username": "mara",
  "display_name": "Mara",
  "content": "!ping @bot-dice",
  "is_pinned": false,
  "is_edited": false,
  "reply_count": 0,
  "attachments": [],
  "reactions": [],
  "mentions": ["00000000-0000-4000-8000-000000000030"],
  "direct_mentions": ["00000000-0000-4000-8000-000000000030"],
  "mention_roles": [],
  "created_at": "2026-11-13T09:30:00Z",
  "nonce": "00000000-0000-4000-8000-000000000099",
  "updated_at": "2026-11-13T09:30:00Z"
}
```

| Field | |
|---|---|
| `id`, `number`, `channel_id` | the message id, its short link number (`/m/4821`) and its channel |
| `user_id`, `username`, `display_name`, `avatar_url` | the author; `avatar_url` is absent without an avatar. For a webhook, `display_name` is the webhook's name or the name the sender gave |
| `content` | the text, up to 4000 characters, always included |
| `parent_id` | present on a reply in a thread: the id of the thread's root message |
| `reply_to_id`, `reply_to` | present on a reply to a message; `reply_to` is a preview (`id`, `deleted`, and while it exists the author and the start of its text) |
| `is_pinned`, `pinned_at`, `pinned_by` | pin state; the other two are absent on an unpinned message |
| `is_edited`, `created_at`, `updated_at` | edit state and times |
| `reply_count` | replies in the thread of this message |
| `attachments` | `id`, `original_filename`, `mime_type`, `size_bytes`, `url` (`/api/media/{id}`), `is_deleted` |
| `reactions` | `emoji`, `count`, `users` (member ids) |
| `mentions` | the members notified by this message, including the bot when it was mentioned (see [allowed mentions](allowed-mentions.md)) |
| `direct_mentions` | the members of `mentions` that are addressed by name or through a role, not only by `@all` or `@here` |
| `mention_roles` | the roles the message mentions |
| `is_bot`, `is_webhook` | present and `true` for a message of a bot (`is_bot`) or of an incoming webhook (both) |
| `embeds`, `components`, `poll` | present when the message has them |
| `forward` | `{"forwarded": true}` on a forwarded message; the original's text and ids are not part of the event |
| `expires_at` | present on a message with a lifetime: when the server deletes it (RFC 3339, see [timed messages](timed-messages.md)) |
| `title`, `tags`, `is_locked`, `is_resolved`, `last_activity_at` | forum posts |
| `nonce` | `MESSAGE_CREATE` only, and only when the sender sent one |

Absent fields are omitted, not `null`. `MESSAGE_UPDATE` has the same shape with
the new state (`"is_edited": true` after an edit, no `nonce`).

## MESSAGE_DELETE

```json
{
  "channel_id": "00000000-0000-4000-8000-000000000010",
  "id": "00000000-0000-4000-8000-000000000002",
  "parent_id": null
}
```

`parent_id` is the root message of the thread when the deleted message was a
reply in one, and `null` for a message of the channel itself.

## MESSAGE_REACTIONS_UPDATE

```json
{
  "message_id": "00000000-0000-4000-8000-000000000001",
  "channel_id": "00000000-0000-4000-8000-000000000010",
  "reactions": [
    { "emoji": "👍", "count": 2, "users": ["00000000-0000-4000-8000-000000000020", "00000000-0000-4000-8000-000000000021"] }
  ]
}
```

`reactions` is the new summary of the whole message, not the one change: compare
it with the summary you had to see who reacted or took a reaction back.

## CHANNELS_UPDATE

```json
{}
```

There is no data. The channel layout changed or the bot's access did (a role, an
overwrite, a channel moved): fetch `GET /channels` again. The server also sends
it to one bot when its own permissions change.

## GUILD_MEMBER_ADD

```json
{
  "id": "00000000-0000-4000-8000-000000000040",
  "username": "noor",
  "display_name": "Noor",
  "avatar_url": "/api/media/..."
}
```

A newly registered member who shares at least one channel with the bot, with the
fields that `GET /members/{id}` shows a bot. `avatar_url` is absent without an
avatar.

## INTERACTION_CREATE

Sent to one live session of the bot (the most recently started) and needs no
intent. Without a live session the member's action fails with `409`.
`d.type` says what happened; `id` and `token` answer it for 15 minutes
([Interactions in detail](bots.md#interactions-in-detail)).

Common fields: `id`, `token`, `type`, `channel_id`, `application_id` (the bot's
`bot_id`), `data`, and `member` (`user`: `id`, `username`, `display_name`,
`avatar_url`; `permissions`: effective channel permissions as a decimal string
bitfield).

### A slash command (`type` 2)

```json
{
  "id": "...", "token": "...", "type": 2,
  "channel_id": "00000000-0000-4000-8000-000000000010",
  "application_id": "00000000-0000-4000-8000-000000000031",
  "data": {
    "id": "<command id>", "name": "roll",
    "options": [{ "name": "sides", "type": 4, "value": 20 }]
  },
  "member": {
    "user": { "id": "...", "username": "mara", "display_name": "Mara", "avatar_url": "/api/media/..." },
    "permissions": "3072"
  }
}
```

`options` follow the command's definition: `type` is the option type (`3` string,
`4` integer, `5` boolean, `6` user and `7` channel, the last two as an id) and
`value` the validated value.

### A component (`type` 3)

```json
{
  "id": "...", "token": "...", "type": 3, "channel_id": "...", "application_id": "...",
  "data": { "custom_id": "env", "component_type": 3, "values": ["staging"] },
  "member": { "user": { "id": "...", "username": "mara", "display_name": "Mara" } },
  "message": { "id": "...", "content": "Deploy v2.4?", "components": [ ... ] }
}
```

`component_type` is `2` for a button (no `values`), `3` for a string select, `5`,
`6` and `8` for a user, role and channel select. A select carries `values`; the
user, role and channel selects also carry `resolved` with what was picked:

```json
"resolved": { "users": { "<id>": { "id": "<id>", "username": "...", "display_name": "...", "avatar_url": "..." } } }
```

or `roles` (`id`, `name`, `color`) or `channels` (`id`, `name`, `type`), keyed by
id. `message` is the message the component is on, as the REST API shows it. For a
component on an ephemeral note it is `{ "id", "flags": 64, "content", "components" }`.

### A modal submit (`type` 5)

```json
{
  "id": "...", "token": "...", "type": 5, "channel_id": "...", "application_id": "...",
  "data": {
    "custom_id": "feedback",
    "components": [
      { "type": 1, "components": [{ "type": 4, "custom_id": "subject", "value": "Dark mode" }] },
      { "type": 1, "components": [{ "type": 4, "custom_id": "body", "value": "It would help." }] }
    ]
  },
  "member": { "user": { "id": "...", "username": "mara", "display_name": "Mara" } },
  "message": { "id": "...", "content": "..." }
}
```

One action row per text input, in the modal's order. `message` is present when
the modal was opened from a component.
