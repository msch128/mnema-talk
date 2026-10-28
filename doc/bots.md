# Bots

Mnema Talk has **bot accounts**: programs that read and post messages through
an API and a gateway, with exactly the access an admin gave them. The model
follows Discord where it maps (bot users, roles and channel overwrites,
`Authorization: Bot <token>`, intents), and differs where this server is
smaller (one community, UUID ids); the
[compatibility table](#differences-from-discord) lists the differences.

This page describes accounts, access and tokens, then the REST API, the
gateway, slash commands (a bot's own and the server's built-in ones), what
bots do not see (direct messages) and the management API.

## Who may create bots

Only the **Owner** and members with the **Manage bots** permission (it opens
the *Bots* tab in the admin area). Administrators hold every permission and so
do too. Rules that keep the power with the Owner:

- **Manage bots can only be granted by the Owner**, never passed on by someone
  with *Manage roles*.
- The usual hierarchy applies: you can only change a bot whose highest role is
  below yours, give it roles below your own highest role, and no permission
  you do not hold yourself.
- A role with **Administrator** or any admin-area permission can only be given
  to a bot by the Owner. The interface warns when a bot holds such a role.
- Managing a bot (tokens, roles, edits) needs at least the rights the bot
  has: everything it may do server-wide and in each channel it can view must
  be within what you may do yourself, and a bot with Administrator or
  admin-area permissions is the Owner's. Adding such permissions to a role a
  bot holds is Owner-only. Otherwise *Manage bots* would be a way to act with
  rights you do not have.
- A bot cannot sign in (no password; login, sessions and re-authentication
  skip bots), cannot hold the Owner role, and the Owner cannot be targeted by
  anything a bot can do. No bot endpoint manages roles, bots, webhooks or
  members, whatever roles the bot holds.

## What a bot is

A bot is a member-like account (it cannot sign in) plus a record with its name, avatar,
allowed intents and tokens. Its messages show the author's name and avatar
and the **BOT** tag. A bot is a **member**: it is listed in the member list,
can be mentioned, and is online while it has a gateway connection (see
*Members, presence and the Bot role* below). Incoming webhooks are different:
their hidden authors are never listed and their messages carry a **Webhook**
tag instead.

## Members, presence and the Bot role

- **Member list.** Enabled bots appear in `GET /api/members` (`is_bot`,
  `added_by`) and so in the member list, the profile, the mention picker and
  reply previews, always with the same **BOT** tag. A disabled bot is hidden.
  The hidden accounts of incoming webhooks are never listed: a bot has a row
  in `bots`, a webhook author does not (its username starts with `wh-`), and
  messages say which it is (`is_bot` for both, plus `is_webhook`).
- **The Bot role.** One built-in, *managed* role, created by migration and
  held by **every** bot from its creation (existing bots were backfilled). It
  cannot be removed from a bot (a role list without it keeps it), cannot be
  given to a person, cannot be deleted and cannot be renamed. Its colour,
  position and *hoist* (own group in the member list, on by default) are
  editable by role managers. See [roles and permissions](roles-and-permissions.md#the-managed-bot-role).
- **The bot's own role.** Like Discord's integration role, each bot also gets a
  managed role named after it, held by that bot only, renamed with the bot and
  deleted with it. It cannot be given to anyone else, removed from the bot,
  renamed or deleted by hand; its colour and permissions are editable, with
  permissions going through the bot invariant. It starts empty (existing bots
  got an empty one: their current grants did not move). See
  [roles and permissions](roles-and-permissions.md#each-bots-own-role).
- **Permissions of the Bot role** start empty, so a bot still **does nothing
  until an admin grants it something**, either on the Bot role (everything every
  bot should have, e.g. *View channels* in a channel through an overwrite) or on
  a role of its own. There is deliberately no default: a sensible minimum
  differs per community, and an empty default keeps the rule "nothing without a
  grant". The role editor says so.
- **Presence.** A bot is **online** while it has at least one live gateway
  connection (a session that can only be resumed does not count) and offline
  otherwise. It can set its own status and an activity with the gateway's
  `PRESENCE_UPDATE` (see [Presence updates](#presence-updates)): *online*,
  *idle* (shown as away), *do not disturb* or *invisible* (shown offline). It is
  announced to members as `presence_update` (with `activity` when the bot set
  one) and is part of the connect-time `presence_snapshot`. Bots are not counted
  in the online total and are never reached by `@here`. The gateway does not
  dispatch other members' presence to bots in v1.
- **Mentions.** Mention a bot by its username (`@bot-…`, shown in the picker
  under its name). The message's `mentions` list then contains the bot's user
  id, which `READY` gave the bot as `user.id`, so a bot recognises a mention of
  itself in `MESSAGE_CREATE` (the bot must be able to view the channel, like
  anyone). `@all` and `@here` never include bots. `GET /members` lists bots too,
  flagged `bot: true`.

## Permissions: roles and overwrites

A bot's permissions come from the same engine as a member's: its **roles**
(`PUT /api/admin/bots/{id}/roles`) and the **channel and category overwrites**
for those roles (the usual channel permissions dialog). Unlike a member it
does **not inherit @everyone**, neither the base permissions nor @everyone
overwrites: a new bot can see and do nothing until an admin gives it a role
and allows that role in channels. A private channel stays invisible to it
unless one of its roles is allowed there. This keeps one enforcement path
for members, bots, the REST API and the gateway.

`GET /api/admin/bots/{id}/access` (the *Access* view of the Bots tab) shows
the effective result: the server-wide permissions and, per viewable channel,
what the bot may do.

Voice is out of scope in v1: bots cannot join Talks (their roles' Connect,
Speak and Video bits do nothing for them).

### Every way a bot can gain permissions

One rule covers all of it, checked on the outcome in the same transaction as
every permission change (roles, role bits and positions, memberships,
overwrites, removed denies, channel moves), so no path can get around it:
**a change by anyone but the Owner can add nothing to a bot, server-wide or in
any channel, that is elevated** (Administrator or any admin-area permission,
including Disconnect members) **or that the person making the change does not
hold there themselves.** The table lists the friendlier early errors on the
common paths.

| Path | Rule |
|---|---|
| Roles of the bot (`PUT /bots/{id}/roles`) | *Manage bots*, a role below your highest, only permissions you hold; a role with Administrator, an admin-area permission, or an overwrite that allows Disconnect members anywhere is the Owner's to give |
| `PUT /admin/users/{id}/roles` | refused for bots |
| Editing a role a bot holds | the usual role rules, and adding Administrator or admin-area bits is Owner-only |
| Reordering roles | moves positions only, grants no permission |
| Channel or category overwrite for a role a bot holds, or for the bot itself | the usual overwrite rules (*Manage channels*, only what you hold there), and allowing Disconnect members (the one elevated channel permission) is Owner-only |
| `@everyone` | not inherited, its overwrites do not apply |
| Managing the bot afterwards (tokens, edits) | needs at least the bot's rights, server-wide and per channel; a bot holding any elevated permission anywhere is the Owner's. The access view lists only channels the caller can see |

## Tokens

Each bot holds up to 10 active API tokens (`mnb_...`, 256 random bits).

- The secret is **shown once**, when the token is issued or regenerated. Only a
  SHA-256 hash is stored; a lost token cannot be shown again.
- Optional **expiry**; every token shows its last characters, state
  (`active`, `expired`, `revoked`) and **last used** time.
- **Regenerate** issues a new token with the same name and expiry and revokes
  the old one at once. **Revoke** and disabling or deleting the bot take
  effect immediately, also for connections that are open.
- A token remembers **who issued it**. Every use re-checks (at most every 30
  seconds, also for open gateway connections) that this person is still active
  and is the Owner, or holds *Manage bots* and everything the bot can do. When
  they are demoted, disabled, banned, or the bot gains a permission they lack,
  their tokens stop working; regenerate them as someone who qualifies.
- **Tokens issued before 1.0** have no recorded issuer. Nobody has to
  regenerate them: the first time such a token is used after the upgrade to
  1.1 it is bound to the person who most plausibly issued it, the bot's
  creator or else the Owner, whoever still qualifies at that moment, so the
  token keeps working and from then on is re-checked like any other. Nothing is
  revoked by this. The bot's token list marks these tokens: *Unbound* until
  they are first used (or while nobody qualifies to take the binding), *Issuer
  assumed* afterwards. An assumed issuer is a guess: regenerate the token to
  bind it to the person who really manages the bot. The binding is logged
  (`"audit":"bot_token_bound"`).

## Getting started

1. As the Owner (or with *Manage bots*): **Admin area > Bots > New bot**, or
   `POST /api/admin/bots {"name": "helper"}`.
2. Give it a **role** (for example *Send messages*, *Add reactions*) and allow
   that role in the channels it should work in (the channel permissions
   dialog). The *Access* view shows the result.
3. Issue a **token** and copy it: it is shown once.
4. Call the API:

```sh
export MNEMA=https://talk.example.org
export TOKEN=mnb_...
curl -s -H "Authorization: Bot $TOKEN" $MNEMA/api/bot/v1/users/@me
curl -s -H "Authorization: Bot $TOKEN" $MNEMA/api/bot/v1/channels
curl -s -X POST -H "Authorization: Bot $TOKEN" -H 'Content-Type: application/json' \
  -d '{"content": "build **passed**", "embeds": [{"title": "Run 42", "color": 65280}]}' \
  $MNEMA/api/bot/v1/channels/$CHANNEL_ID/messages
```

## REST API

The base path is **`/api/bot/v1`**. It is a separate, versioned path, not the
member routes with another header: the contract (shapes, errors, limits) stays
stable for bots while the web app's API evolves, and a breaking change gets
`/v2` next to it. The handlers behind it are the same ones the web app uses,
so a bot can never do something through the API that its permissions would not
allow in the app. The OpenAPI document describes every route (tag *Bot API*,
security scheme `botAuth`).

- **Authentication:** `Authorization: Bot <token>` only. No cookie is read, so
  there is no CSRF path (no Origin header is needed), and a token is refused on
  every other route (members' routes, `/api/admin/*`, webhooks) with `401`.
  Wrong tokens are counted per client address (30 a minute, then `429`).
- **Ids** are UUIDs, not snowflakes.
- **Errors** use Discord's shape, `{"message": "...", "code": 10003}`, with
  Discord's codes where one fits: `10003` unknown channel (also for a channel
  the bot may not see), `10008` unknown message, `50013` missing permissions,
  `50035` invalid form body, `50006` empty message, `40005` too large, `0` for
  plain HTTP errors (`401: Unauthorized`, `404: Not Found`).
- **Rate limits** per bot, with separate buckets: **50 requests per second**
  overall (`global: true` when that one is hit) and **5 messages per 5 seconds
  per channel** for posts and uploads. A `429` carries
  `{"message": "You are being rate limited.", "retry_after": 1.2, "global": false}`
  and a `Retry-After` header. Other bots' limits are unaffected.

| Route | Needs |
|---|---|
| `GET /users/@me` | a valid token |
| `GET /channels` | the channels the bot can view, with their categories |
| `GET /channels/{id}/messages` (`limit`, `before`, `after`, `around`) | View channels |
| `GET /channels/{id}/messages/{id}` | View channels |
| `POST /channels/{id}/messages` | Send messages; body `content`, `embeds`, `allowed_mentions`, `message_reference`, `parent_id` |
| `PATCH /channels/{id}/messages/{id}` | Send messages, own messages only |
| `DELETE /channels/{id}/messages/{id}` | own messages; others' with Manage messages |
| `PUT`, `DELETE /channels/{id}/messages/{id}/reactions/{emoji}/@me` | Add reactions (taking back always works) |
| `POST /channels/{id}/upload` (multipart `file`, optional `content`) | Send messages and Attach files; the Owner's upload limit for the bot's roles applies |
| `GET /members`, `GET /members/{id}` | the `GUILD_MEMBERS` intent allowed; only members who can view a channel the bot can view; id, name, avatar |

Mentions: `@username` notifies members who can view the channel; `@all` and
`@here` only when the bot may mention everyone **and** `allowed_mentions.parse`
contains `"everyone"`. `@Role Name` notifies the role's members when `allowed_mentions.parse` contains `"roles"`, or `allowed_mentions.roles` lists the role's ID (UUID). A bot may mention roles marked mentionable; with Mention @all and @here it may mention any custom role (not `@everyone` or the managed Bot roles). Role mentions are **off by default**: a message without `allowed_mentions` mentions no role, and so does an edit (which takes no `allowed_mentions`, so editing a message removes its role mentions instead of adding them). Only members who can view the channel are notified. Bots cannot join Talks (voice) in v1.

Custom emoji travel as the token the app stores, `<:name:id>` (a bot can copy
one from a message it reads); a plain `:name:` in a message posted through the
API stays literal text, because only the web composer turns shortcodes into
tokens. Unicode emoji work as typed.

## Gateway

The gateway pushes events to a bot over a WebSocket, with Discord's opcodes
and flow. OpenAPI cannot describe it, so this page is its reference.
`GET /api/bot/v1/gateway` (with the token) returns its address, the
heartbeat interval and the connection cap; the same path is the gateway when
the request is a WebSocket upgrade.

**Authentication.** Send `Authorization: Bot <token>` with the upgrade
request. The token is never taken from the URL, so it cannot end up in logs.
Clients that cannot set headers (browsers) may instead put the token in
IDENTIFY, like Discord; an upgrade without a valid token closes with `4004`.

### Frames

Every frame is JSON, `{"op": n, "d": ..., "s": seq, "t": "NAME"}`; `s` and `t`
are only set on dispatches (`op 0`). Frames from the bot are at most 4096
bytes and 120 a minute.

| op | name | direction | |
|---|---|---|---|
| 10 | HELLO | server | first frame: `{"heartbeat_interval": 41250}` (ms) |
| 2 | IDENTIFY | bot | `{"token": "...", "intents": 512, "properties": {...}}`; token optional when the header authenticated; intents required |
| 0 | DISPATCH | server | an event, numbered by `s`; the first is `READY` |
| 1 | HEARTBEAT | bot | send every `heartbeat_interval`; `d` may carry the last `s` |
| 11 | HEARTBEAT_ACK | server | answer to HEARTBEAT |
| 3 | PRESENCE_UPDATE | bot | `{"status": "dnd", "activities": [{"name": "chess", "type": 0}]}`, see [Presence updates](#presence-updates) |
| 6 | RESUME | bot | `{"token": "...", "session_id": "...", "seq": 41}` on a new connection |
| 9 | INVALID_SESSION | server | `d: false`: the session cannot be resumed; send IDENTIFY on the same connection |
| 7 | RECONNECT | server | reserved; the server closes instead |

`READY` carries `v`, `user` (`id`, `bot_id`, `username`, `bot`, `avatar_url`),
`session_id`, `intents` (granted), `resume_gateway_url` and
`heartbeat_interval`.

### Presence updates

A bot sets its own status and activity with op 3, in Discord's format, after
IDENTIFY (before it, the connection closes with `4003`). IDENTIFY may also carry
the starting presence as `d.presence` (same shape; an invalid one is ignored).

```json
{"op": 3, "d": {"since": null, "afk": false, "status": "idle",
  "activities": [{"name": "chess", "type": 0}]}}
```

- `status` is required: `online`, `idle`, `dnd` or `invisible` (`offline` is an
  alias of `invisible`). Members see them as online, away, do not disturb and
  offline; an invisible bot shows no activity.
- `activities` is optional; only the **first** is used. `type` is `0` playing,
  `2` listening, `3` watching, `4` custom (the text is `state`, or `name`) or `5`
  competing; streaming (`1`) needs a URL and is refused. The text is cleaned
  (control and formatting characters removed, white space collapsed) and cut to
  64 characters; an activity with no text left is none. `since` and `afk` are
  accepted and ignored.
- A bad `status` or `type`, or an undecodable payload, closes with `4002`.
- At most **5 updates per 60 seconds per bot** (all its connections together);
  further updates are dropped without an error, as in Discord.
- The status belongs to the bot, not to a connection: the last update wins. It
  ends with the bot's last connection, so a bot that reconnects starts as online
  without an activity.

**Heartbeats.** A bot that sends nothing for 1.5 intervals after identifying
is closed with `4009`; one that does not identify within 10 seconds, `4003`.

**Resume.** Keep the last `s`. After a disconnect, open a new connection and
send RESUME within **2 minutes**: the server replays the events after `seq`
(it keeps the last 512 per session) and sends `RESUMED`. If it cannot (unknown
session, `seq` ahead of the session or older than the buffer, window over) it
answers INVALID_SESSION and the bot identifies instead. Resuming takes the
session over from a connection that still holds it (closed with `4000`).
Events during the gap are replayed through the same scope checks that applied
when they happened.

### Close codes

| code | meaning |
|---|---|
| `4000` | session taken over by another connection |
| `4001`, `4002` | unknown opcode, undecodable frame |
| `4003` | not authenticated (no IDENTIFY in time) |
| `4004` | authentication failed: bad, revoked or expired token, bot disabled or deleted. Revoking a token or disabling a bot closes its open connections with this code **immediately** |
| `4005` | already identified |
| `4008` | rate limited: too many frames, IDENTIFYs, or connections (at most **3** live connections and 6 resumable sessions per bot) |
| `4009` | session timed out (missed heartbeats) |
| `4013`, `4014` | invalid intents; intents the admin has not allowed for this bot |
| `1009`, `1013` | frame too large; the bot reads too slowly (resume) |

### Intents and events

IDENTIFY lists the **intents** the bot wants, using Discord's bit numbers; the
admin sets the intents a bot is *allowed* to ask for (Bots tab), and asking for
more closes with `4014`. Intents choose event classes. **Which channels an
event comes from is not an intent**: every event is checked, at the moment it
is sent, against what the bot's roles and overwrites let it view, so a bot
never receives an event of a channel it cannot see, a private channel included,
and loses a channel's events the moment its access ends.

| Intent | Bit | Events |
|---|---|---|
| `GUILDS` | 1 | `CHANNELS_UPDATE` `{}`: the channel layout or the bot's access changed, fetch `GET /channels` again |
| `GUILD_MEMBERS` | 2 | `GUILD_MEMBER_ADD` (`id`, `username`, `display_name`, `avatar_url`), for new members who share a channel with the bot |
| `GUILD_MESSAGES` | 512 | `MESSAGE_CREATE`, `MESSAGE_UPDATE` (the message, as in the REST API), `MESSAGE_DELETE` (`id`, `channel_id`, `parent_id`) |
| `GUILD_MESSAGE_REACTIONS` | 1024 | `MESSAGE_REACTIONS_UPDATE` (`message_id`, `channel_id`, `reactions`: the new summary) |

The bot's own messages are dispatched to it as well. Message content is always
included. Not offered in v1: typing, other members' presence, voice, member
removal.

### A minimal client

JavaScript (Node 22, no libraries):

```js
const token = process.env.TOKEN
const ws = new WebSocket('wss://talk.example.org/api/bot/v1/gateway', { headers: { Authorization: `Bot ${token}` } })
let seq = null, timer
const send = (op, d) => ws.send(JSON.stringify({ op, d }))
ws.onmessage = ({ data }) => {
  const f = JSON.parse(data)
  if (f.s != null) seq = f.s
  if (f.op === 10) {
    send(2, { intents: 512, properties: { os: 'linux' } }) // GUILD_MESSAGES
    timer = setInterval(() => send(1, seq), f.d.heartbeat_interval)
  } else if (f.op === 0 && f.t === 'MESSAGE_CREATE' && !f.d.is_bot && f.d.content === '!ping') {
    fetch(`https://talk.example.org/api/bot/v1/channels/${f.d.channel_id}/messages`, {
      method: 'POST',
      headers: { Authorization: `Bot ${token}`, 'Content-Type': 'application/json' },
      body: JSON.stringify({ content: 'pong', message_reference: { message_id: f.d.id } })
    })
  }
}
ws.onclose = (e) => { clearInterval(timer); console.log('closed', e.code) } // resume or identify again
```

Go (`github.com/gorilla/websocket`):

```go
hdr := http.Header{"Authorization": {"Bot " + token}}
c, _, err := websocket.DefaultDialer.Dial("wss://talk.example.org/api/bot/v1/gateway", hdr)
if err != nil { log.Fatal(err) }
var hello struct{ D struct{ HeartbeatInterval int `json:"heartbeat_interval"` } }
if err := c.ReadJSON(&hello); err != nil { log.Fatal(err) }
_ = c.WriteJSON(map[string]any{"op": 2, "d": map[string]any{"intents": 512}})
var mu sync.Mutex
var seq atomic.Int64
go func() {
	for range time.Tick(time.Duration(hello.D.HeartbeatInterval) * time.Millisecond) {
		mu.Lock()
		_ = c.WriteJSON(map[string]any{"op": 1, "d": seq.Load()})
		mu.Unlock()
	}
}()
for {
	var f struct {
		Op int             `json:"op"`
		S  *int64          `json:"s"`
		T  string          `json:"t"`
		D  json.RawMessage `json:"d"`
	}
	if err := c.ReadJSON(&f); err != nil { log.Fatal(err) } // resume or identify again
	if f.S != nil { seq.Store(*f.S) }
	if f.T == "MESSAGE_CREATE" { log.Printf("%s", f.D) }
}
```

## Slash commands

A bot registers commands over its API; members run them from the composer by
typing `/`, and the bot answers over the REST API. It is Discord's application
command and interaction flow, reduced to what a single community needs.

**Register** (`PUT /api/bot/v1/commands` replaces them all, as Discord's bulk
overwrite; `POST` adds or updates one by name; `GET`, `DELETE
/commands/{id}`):

```json
{"name": "ping", "description": "Replies with pong",
 "options": [{"name": "text", "description": "What to say", "type": 3, "required": true}]}
```

Names are 1 to 32 lowercase letters, digits, `-` or `_`; at most 50 commands
per bot and 10 options per command. Option types: `3` string, `4` integer,
`5` boolean, `6` user (a member's ID), `7` channel (an ID); required options
come first. A text option may list `choices` (at most 25, each 1 to 100
characters, unique): the composer shows them as a list and the server refuses
any other value.

**Where they show.** Commands are not tied to channels by the bot. A member
sees a command in the composer of every channel where the **bot can view and
send** (its roles and overwrites decide, so the admin's channel scoping is the
command scoping), and only while neither the command nor the bot is disabled.
The composer lists them with the bot's name and avatar. Running one needs *Send
messages* in the channel. The admin sees all of a bot's commands (Bots tab) and
can **disable** any of them or delete them; a disabled command stays off even
when the bot registers it again.

**Run.** `POST /api/channels/{id}/interactions {"command_id": "...",
"options": {"text": "hello"}}` validates the options (types, required ones,
members and channels the caller can see) and delivers an `INTERACTION_CREATE`
dispatch to one live gateway session of the bot (no intent needed). It answers
`202`, or `409` when the bot is not connected. At most 20 runs a minute per
member.

```json
{"op": 0, "t": "INTERACTION_CREATE", "s": 7, "d": {
  "id": "...", "token": "...", "type": 2, "channel_id": "...", "application_id": "<bot id>",
  "data": {"id": "<command id>", "name": "ping", "options": [{"name": "text", "type": 3, "value": "hello"}]},
  "member": {"user": {"id": "...", "username": "max", "display_name": "Max", "avatar_url": "/api/media/..."}}}}
```

**Respond** within 15 minutes, with `id` and `token` from the event:

- `POST /api/bot/v1/interactions/{id}/{token}/callback`
  `{"type": 4, "data": {"content": "pong", "embeds": [...], "flags": 0}}` posts
  the message as the bot; `type 5` only acknowledges and the answer follows.
- `POST .../followups` with `data` as the body sends more messages, up to 5
  responses in total.
- **Ephemeral:** `"flags": 64` shows the response to the member who ran the
  command only. It is sent as the `interaction_response` WebSocket event to
  that member's sessions, shown as a private note in the channel, and **never
  stored**; if the member is offline they miss it.

The bot needs *Send messages* in the channel when it responds; responses never
notify `@all` or `@here`. Interactions live in the server's memory: a restart
within the 15 minutes makes the token unknown (`404`).

## Moderation: timeouts and bans

A bot is a member for moderation too. Members with *Timeout members* or *Ban
members* find both in the member context menu of a bot (the usual rules: a
higher role than the bot's highest role, never the Owner).

- **Timeout.** A timed-out bot keeps reading (REST and gateway) but loses what
  a timeout takes from any member: it cannot post, react, attach files or
  mention everyone. That covers REST posts, uploads, reactions and the
  responses to slash commands (`403`, code `50013`). Its gateway connection
  stays open. The timeout ends by itself or when lifted.
- **Ban.** A banned bot is refused like an unknown token (`401`) while the ban
  runs, and its open gateway connections and resumable sessions close at once
  with `4004`. It cannot identify or resume until the ban ends or is lifted;
  its tokens are untouched, so lifting the ban brings it back. A ban is
  separate from the bot's own *disabled* switch. Disabling the account in the
  admin area does the same as a ban.

## In the app

- Bot messages carry the **BOT** tag (webhook messages the **Webhook** tag).
  Bots are listed in the member list with the same tag, under the Bot role when
  it is hoisted, online while connected. Their profile shows the tag, their
  roles and who added them.
- A bot's status shows as its presence dot (online, away, do not disturb), and
  its activity ("Playing chess") as the second line of its member row and in its
  profile. An invisible bot looks offline.
- Typing `/` in a channel lists the slash commands available there, the
  server's built-in ones first (see below); picking one
  shows its options in a bar above the composer (Enter on an empty composer or
  the Run button sends it, Esc drops it). A bot's private reply appears below
  the messages with a note that only you can see it, until you dismiss it.
- Admins find **Bots** in the admin area: create, roles, intents, access view,
  tokens and commands.

## Built-in commands and polls

The server has slash commands of its own. They appear in the same menu as the
bots' commands (marked *Built-in*, listed first), are run through the same
route (`POST /api/channels/{id}/interactions`) and go through the same checks:
the member needs *Send messages* in the channel, runs are limited to 20 a
minute, and the options are validated against the command's definition before
the command sees them. `GET /api/channels/{id}/commands` marks them
`builtin: true` (with an empty `bot_name` and a fixed ID that is the same on
every server); running one answers `200` with its result (`message_id` when it
posted a message) instead of the `202` a bot's interaction gets. A bot's command with the same name is listed next to the built-in one,
not instead of it. Descriptions are English fallbacks; the web app shows its
own translations.

Adding a command means one type that implements `slash.Builtin`
(`internal/slash`: a `Definition`, an `Available` check for rights beyond
sending, and `Run`) and one line in `builtinCommands` (`internal/server`).
Today there are these (the web app translates the menu texts):

- **`/poll`** asks the channel a question. Options: `question` (up to 300
  characters), `answers` (2 to 10, each up to 55 characters, separated by
  `|`), `duration` (`1h`, `4h`, `8h`, `24h` by default, `3d`, `1w`),
  `multiple` and `anonymous`. In the web app `/poll` and the poll button next
  to the attach button open the same form; the REST route
  `POST /api/channels/{id}/polls` does the same for scripts.
- **`/me text`**, **`/shrug [text]`**, **`/tableflip [text]`**,
  **`/unflip [text]`** and **`/spoiler text`** post a message of the caller
  with a transformed text: `/me` stores `_text_` (italics, so every client
  shows an action line), `/spoiler` stores `||text||`, the other three append
  the familiar emoticon. They go through the same validation (4000 characters),
  mention rules and slow mode as a message typed in the composer. The result
  carries the `message_id`.
- **`/msg @member text`** opens (or reuses) the direct message with the member
  and writes the text there. It runs the service behind `POST /api/dms`, so
  *Send direct messages*, the community switch, blocks, the member's
  `dm_policy` and the limits apply, and a refusal about the other member is the
  route's one text. The member must be able to see the channel the command is
  run in.
- **`/nick name`** changes the caller's own display name through the profile
  code (`PUT /api/users/me/profile`): 24 characters at most, the same cleaning
  of the text, the bio stays, `user_update` is broadcast. At most 3 changes in
  10 minutes. It can only ever rename the caller.
- **`/thread title`** starts a thread on the caller's latest message of the
  channel that is not itself a reply; the title becomes the thread's first
  reply. Anyone who may send may start a thread.
- **`/timeout @member duration [reason]`** and **`/ban @member [duration]
  [reason]`** run the same service functions as the admin routes
  (`POST /api/admin/users/{id}/timeout` and `.../ban`): they need *Timeout
  members* / *Ban members* and a highest role above the member's, never act on
  the Owner or the caller, and write the same audit entries
  (`moderation.timeout`, `moderation.ban`, with `via: slash`). A duration is a
  number and a unit, for example `30m`, `2h`, `7d` or `2w`; a ban without one
  is permanent.
- **`/kick @member [reason]`** signs the member out everywhere and drops them
  from their Talk, with no sanction; it needs *Kick members*
  ([moderation](moderation.md#kicks)).

The moderation commands only appear in the menu of members who hold the
permission, and they check it again on the server: a request that names one
without the right is refused (`404`, the command is not available to the
caller), and the shared service functions repeat the hierarchy checks. A
member option accepts only an active, non-bot member who can see the channel.
The names `poll`, `me`, `msg`, `shrug`, `tableflip`, `unflip`, `spoiler`, `nick`,
`thread`, `timeout`, `ban` and `kick` are taken by built-ins (a bot may
register a command of the same name, listed next to it). `/tts` is not offered.
`/giphy` is not built in 1.1: GIF search waits for a provider that allows
server-side proxying.

**Polls.** A poll is a message whose text is the question, with a `poll`
object (options with their vote counts, `multiple`, `anonymous`, `closes_at`,
`closed`, `total_voters`). A poll cannot be edited; deleting its message
deletes it. Members vote with `PUT .../messages/{id}/poll/votes`
(`{"option_ids": [...]}` replaces their votes, an empty list withdraws them;
`409` once the poll has ended). The author, or anyone with *Manage messages*,
ends it early with `POST .../poll/end`. Every change is broadcast as a
`poll_update` event (the poll as every viewer sees it); an anonymous poll
broadcasts only when it ends, so a small audience cannot infer a choice from
the tally moving. Results are live while the poll is open and stay after it
ended. A poll that is not anonymous lists
the voters of each option. An **anonymous** poll never does: not in the
message, the message list, search, the mentions inbox, the WebSocket, the
bot gateway or the outgoing webhooks, and not for the author or the Owner.
Since 1.2 (feature `polls.anonymous`) it does not even store the choice: the
server keeps only that a member voted and a counter per option, so nobody in
the app, moderators and administrators included, can tell what a member chose
(an operator with direct database access could in theory correlate vote
timing; see SECURITY.md). Hence a
vote in an anonymous poll is final: the second `PUT .../poll/votes` answers
`409` and an empty `option_ids` list `400`. `GET .../poll` returns the caller's
own votes in `my_votes` (always empty for an anonymous poll) and `has_voted`,
which is how a client knows that the caller took part. Push and desktop
notifications read "Poll: question".

## Direct messages

Bots and webhooks can neither open direct message conversations nor write in
them in 1.1, and the gateway's `MESSAGE_CREATE`, the outgoing webhook events
and every bot REST route never carry a message of a conversation: a bot is
never a member, so it holds no permission there, and the hub does not show
conversation events to the gateway or to outgoing webhooks. A future intent
for direct messages to a bot is not part of 1.1.

A forwarded message reaches a bot as an ordinary message: its `content` is the
forwarder's comment (possibly empty) and `forward: {forwarded: true}`. Neither
the original's text nor its ids are part of the event, so a forward cannot show
a bot a channel it may not view. Forwards into conversations never reach bots.

## Management API

All under `/api/admin/bots`, documented in the OpenAPI document. Every
mutation is reported to the audit log.

| Route | Purpose |
|---|---|
| `GET`, `POST /bots` | list, create (name, optional intents) |
| `GET`, `PATCH`, `DELETE /bots/{id}` | read, change (name, intents, disabled), delete; renaming renames the bot's own role, deleting deletes it |
| `POST /bots/{id}/avatar` | avatar (multipart, field `avatar`) |
| `PUT /bots/{id}/roles` | the bot's roles (the Bot role and its own role stay, whatever the list says; another bot's own role is refused) |
| `GET /bots/{id}/access` | effective permissions |
| `GET`, `POST /bots/{id}/tokens` | list, issue |
| `POST /bots/{id}/tokens/{tokenID}/regenerate`, `DELETE ...` | rotate, revoke |

## Differences from Discord

| | Discord | Mnema Talk |
|---|---|---|
| Who creates bots | anyone with a developer account | the Owner and *Manage bots* (Owner-granted) |
| Ids | snowflakes | UUIDs |
| Default access | bots get `@everyone` | bots get nothing until given a role |
| Member list | bots are listed, with a BOT tag | the same; webhook authors are not listed and are tagged Webhook (Discord tags them BOT) |
| The bot's role | every bot has a managed role named after it (the "integration" role) | the same: each bot has a managed role named after it, held by it only, renamed and deleted with it, with permissions under the bot invariant. Plus one shared managed role named Bot that every bot holds (it groups bots in the member list). Existing bots' grants were not moved into their new role |
| Presence | online, idle, do-not-disturb, invisible and an activity, set with gateway op 3 (and `presence` in IDENTIFY) | the same statuses, shown as online, away, do not disturb, offline; one activity (playing, listening, watching, custom, competing; no streaming); 5 updates per 60 s per bot, extra ones dropped; no presence events *to* bots |
| Timeout and ban | apply to bots as to members | the same: a timed-out bot cannot post or respond, a banned bot's token is refused and its gateway sessions close |
| Admin power | an invite with permissions | only the Owner can give Administrator or admin-area roles |
