# Bots

Mnema Talk has **bot accounts**: programs that read and post messages through
an API and a gateway, with exactly the access an admin gave them. The model
follows Discord where it maps (bot users, roles and channel overwrites,
`Authorization: Bot <token>`, intents), and differs where this server is
smaller (one community, UUID ids); the
[compatibility table](#differences-from-discord) lists the differences.

This page describes accounts, access and tokens, adding a bot from its
manifest, then the REST API, the gateway, slash commands (a bot's own and the
server's built-in ones), message components (buttons and select menus) and
modals, the official SDKs, what bots do not see (direct messages) and the
management API. Why the platform has this shape, and what it leaves out on
purpose, is in the [design note](bot-platform.md).

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
| Adding a bot from a manifest (`POST /bots/install`) | *Manage bots* (checked again under the permission lock); channel permissions only, never elevated; allow overwrites of the new bot's own role in the channels the installer ticked and can view, each within what the installer holds there (the refusal names the permission and the channel) |
| `@everyone` | not inherited, its overwrites do not apply |
| Managing the bot afterwards (tokens, edits) | needs at least the bot's rights, server-wide and per channel; a bot holding any elevated permission anywhere is the Owner's. The access view lists only channels the caller can see |

## What a bot cannot do

A bot reads and writes messages, reactions, slash commands and components, and
nothing that manages people or the server. The routes under `/api/bot/v1` are
the full list ([REST API](#rest-api)): there is none for roles, members,
bans or timeouts, webhooks, channels, bots or settings, and no role or
permission a bot holds adds one, not even Administrator. Besides, a bot cannot
join a Talk (voice), reads no direct messages, and is not told about other
members' presence.

In particular a bot **cannot give or take a role**, so there are no *reaction
roles* (a reaction that gives a role): the reaction event reaches the bot, but
nothing it could call changes a member's roles. The way to let members choose
their own roles is a **self-assignable role**: an admin with *Manage roles*
turns on *Members can assign this role to themselves*, and members switch it on
or off under *Settings > Roles*, without any bot or admin involved. It is
limited to harmless roles ([roles members choose
themselves](roles-and-permissions.md#roles-members-choose-themselves)); bots
cannot use it either.

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

Or start from an SDK (see [SDKs](#sdks)) and its examples.

## Adding a bot from a manifest

A bot written by someone else describes itself in a **manifest**: a JSON
document in its README. The Owner or a member with *Manage bots* adds it in
**Admin area > Bots > Add from manifest**, by pasting the manifest or by
opening an **add link**, sees what it asks for, picks the channels it may
work in, confirms, and copies the token (shown once) into the bot's
configuration.

```json
{
  "manifest_version": 1,
  "name": "Mnemo",
  "description": "Answers questions and keeps notes.",
  "homepage": "https://github.com/msch128/mnema-talk-mnemo-bot",
  "avatar": "data:image/png;base64,iVBORw0KGgo...",
  "intents": ["GUILDS", "GUILD_MESSAGES"],
  "permissions": ["view_channel", "send_messages", "add_reactions"],
  "commands": [
    {"name": "ask", "description": "Ask Mnemo something",
     "options": [{"name": "question", "description": "Your question", "type": 3, "required": true}]}
  ]
}
```

| Field | Rule |
|---|---|
| `manifest_version` | required, `1` |
| `name` | required, the bot's name (1 to 24 characters, as when creating a bot) |
| `description` | optional, up to 200 characters, shown in the Bots tab |
| `homepage` | optional `https` URL, up to 512 characters, shown as a link |
| `avatar` | optional `data:` URL of a PNG, JPEG or WebP image of at most 256 KB; the web app uploads it through the avatar route, the server never fetches anything |
| `intents` | optional, names from [Intents and events](#intents-and-events): `GUILDS`, `GUILD_MEMBERS`, `GUILD_MESSAGES`, `GUILD_MESSAGE_REACTIONS`; another name is refused |
| `permissions` | optional, channel permissions only: `view_channel`, `send_messages`, `attach_files`, `add_reactions`, `manage_messages`, `mention_everyone`. Anything else (Administrator, admin-area permissions, voice) is refused: the Owner grants those by hand |
| `commands` | optional, at most 50, each as for `PUT /api/bot/v1/commands` |

Unknown fields are ignored, so a newer manifest still installs on an older
server with what that server understands.

**The add link** is

```
https://<your server>/admin/bots/add#manifest=<the manifest as base64url>
```

The manifest is in the URL fragment, which the browser never sends to the
server, so it is not logged anywhere, and opening the link only opens the
confirm screen (after signing in, for someone with *Manage bots*). Both SDKs
build it (`addBotLink(serverUrl, manifest)`, `mnema.AddBotLink`). A bot's
README usually shows the manifest and the link with a placeholder for the
server's address.

**The confirm screen** shows the name, avatar, description and homepage,
quoted and labelled as the bot's own words (never like text of the app),
each intent and permission the bot asks for (ticked, except Mention
everyone and Manage messages, which ping or moderate and start unticked;
each can be ticked or unticked within what the manifest asks), its commands
(one with a built-in's name is marked as the bot's: it is listed next to the
built-in, not instead of it), and the channels the installer can see (none
ticked). Nothing is created until **Add bot**:

- The requested permissions become **allow overwrites of the bot's own
  role** in the ticked channels (and nothing server-wide: bots do not inherit
  `@everyone`, so a server-wide grant would include private channels). A
  bot added without a ticked channel can see nothing yet, like a bot created
  by hand.
- The allowed intents are the ticked ones; the commands are registered as
  if the bot had sent them.
- A token named after the manifest is issued and shown once.

All of it happens in one transaction under the permission lock and the bot
invariant (see [Every way a bot can gain permissions](#every-way-a-bot-can-gain-permissions)):
the installer may only grant, in each channel, what they hold there
themselves, the installer can untick but never add, and a manifest can never
ask for anything elevated. Installing needs *Manage bots*, not *Manage
channels*: the overwrites it writes belong to the new bot's own role, which
nobody else holds, so they change nothing for any member. They do change
what the **bot** may do there, by the usual resolution order
([roles and permissions](roles-and-permissions.md#channel-and-category-overwrites)): in a channel, all of a member's
role denies apply first and then all of its role allows, so an allow written
on the bot's own role **wins over a deny on the shared Bot role** (or on any
other role the bot holds) in that channel and its category. Only a deny set
for the bot itself (a member overwrite) still beats it. This is the same as
giving a bot a role that has channel overwrites, which *Manage bots* could
always do; the bot invariant still caps every allow at what the installer
holds in that channel. To keep a bot out of a channel whatever its roles say,
deny the bot itself there, or do not tick the channel. The install is
logged (`bot.installed`). Afterwards it is an ordinary bot.

The routes are `POST /api/admin/bots/manifests/check` (the body is the
manifest; it is validated and answered normalised: cleaned text, `intents` as
`{"name", "bit"}` in bit order, `permissions` as `{"key", "name", "bit"}` in
the order of the permission list, no duplicates, without the avatar; no side
effects) and `POST /api/admin/bots/install`
(`{"manifest": {...}, "permissions": [...], "intents": [...], "channel_ids": [...]}`,
the lists being what the installer kept ticked, at most 500 channels; answers
`201` with `{"bot": {...}, "token": {...}}`, the token as when issuing one,
its `secret` shown once). A refusal creates nothing: `400` for an invalid
manifest or a kept permission or intent that is not in it (the message names
it), `404` for a listed id that is not a channel of the community the installer
can view (categories and direct message conversations included), `403` for a
kept permission the installer does not hold in a listed channel (the message
names both), `409` at the bot limit or for a temporary Talk (it follows its
lobby). The manifest's description and homepage are part of the bot
(`description`, `homepage`, absent for a bot created by hand).

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
| `POST /channels/{id}/messages` | Send messages; body `content`, `embeds`, `components`, `allowed_mentions`, `message_reference`, `parent_id` |
| `PATCH /channels/{id}/messages/{id}` | Send messages, own messages only; `content`, `components` (each optional, absent ones stay; `"components": []` removes them) and `allowed_mentions` (as on a post; an edit that sends it counts towards the 5 posts per 5 seconds of the channel, one without it notifies the members it names and never `@all`, `@here` or a role) |
| `DELETE /channels/{id}/messages/{id}` | own messages; others' with Manage messages |
| `PUT`, `DELETE /channels/{id}/messages/{id}/reactions/{emoji}/@me` | Add reactions (taking back always works); one emoji with and without U+FE0F is one reaction |
| `POST /channels/{id}/upload` (multipart `file`, optional `content`, `allowed_mentions`, `parent_id`, `reply_to_id`) | Send messages and Attach files; the Owner's upload limit for the bot's roles applies |
| `GET /members`, `GET /members/{id}` | the `GUILD_MEMBERS` intent allowed; only members who can view a channel the bot can view; id, name, avatar |

Slow mode: a channel's slow mode holds a bot back like a member, on every route
that posts (a message, an upload, the response to a command): a post inside the
wait answers `429` with `retry_after`. A bot whose roles give it Manage messages
is exempt, again like a member, and may also reply under a locked forum post.

Mentions (all the rules in one place: [allowed mentions](allowed-mentions.md)): `@username` notifies members who can view the channel; `@all` and
`@here` only when the bot may mention everyone **and** `allowed_mentions.parse`
contains `"everyone"`. `@Role Name` notifies the role's members when `allowed_mentions.parse` contains `"roles"`, or `allowed_mentions.roles` lists the role's ID (UUID). A bot may mention roles marked mentionable; with Mention @all and @here it may mention any custom role (not `@everyone` or the managed Bot roles). Role mentions are **off by default**: a message without `allowed_mentions` mentions no role, and so does an edit (which takes no `allowed_mentions`, so editing a message removes its role mentions instead of adding them). Only members who can view the channel are notified. The upload route takes the same object as the form field `allowed_mentions` (JSON-encoded, for example `{"parse":["everyone","roles"]}`) and applies the same rules to its caption: without the field the caption notifies the members it names, never `@all`, `@here` or a role. Bots cannot join Talks (voice) in v1.

Custom emoji travel as the token the app stores, `<:name:id>` (a bot can copy
one from a message it reads); a plain `:name:` in a message posted through the
API stays literal text, because only the web composer turns shortcodes into
tokens. Unicode emoji work as typed.

## Gateway

The gateway pushes events to a bot over a WebSocket, with Discord's opcodes
and flow. OpenAPI cannot describe it, so this page is its reference; the payload of each
event is in [gateway events](gateway-events.md).
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

`READY` carries `v`, `user` (`id`, `bot_id`, `username`, `handle`, `bot`,
`avatar_url`), `session_id`, `intents` (granted), `resume_gateway_url` and
`heartbeat_interval`. `username` is the bot's name as members see it (its
display name, as it always was); `handle` is the account's unique name, the
one members type to mention it (`@bot-…`; a message's `mentions` lists the
bot's `id`), so a bot can find `@` plus its handle in a text without the
`GUILD_MEMBERS` intent. `GET /users/@me` carries both too.

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

These are the bot gateway's codes. The member WebSocket (`/api/ws`) has codes of
its own: there `4002` means "renew the media connection: reconnect and resume
the call", not an undecodable frame.

### Intents and events

IDENTIFY lists the **intents** the bot wants, using Discord's bit numbers; the
admin sets the intents a bot is *allowed* to ask for (Bots tab), and asking for
more closes with `4014`. Shrinking the allowed intents later closes the
bot's open sessions that asked for more with `4014` as well; the bot reconnects
with fewer intents. Intents choose event classes. **Which channels an
event comes from is not an intent**: every event is checked, at the moment it
is sent, against what the bot's roles and overwrites let it view, so a bot
never receives an event of a channel it cannot see, a private channel included,
and loses a channel's events the moment its access ends.

| Intent | Bit | Events |
|---|---|---|
| `GUILDS` | 1 | `CHANNELS_UPDATE` `{}`: the channel layout or the bot's access changed, fetch `GET /channels` again |
| `GUILD_MEMBERS` | 2 | `GUILD_MEMBER_ADD` (`id`, `username`, `display_name`, `avatar_url`), for new members who share a channel with the bot |
| `GUILD_MESSAGES` | 512 | `MESSAGE_CREATE` (the message, as in the REST API; it carries the sender's `nonce` when the sender sent one), `MESSAGE_UPDATE` (the message), `MESSAGE_DELETE` (`id`, `channel_id`, `parent_id`) |
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
per bot and 10 options per command. Commands an admin disabled are kept by a
bulk `PUT` and count toward the 50 (more is a `400`); `DELETE /commands/{id}` of
a command an admin disabled answers `404`, so a bot cannot remove (or re-create)
what the admin switched off. Option types: `3` string, `4` integer,
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
  `{"type": 4, "data": {"content": "pong", "embeds": [...], "components": [...], "flags": 0}}` posts
  the message as the bot; `type 5` only acknowledges and the answer follows
  (with `"data": {"flags": 64}` the answer will be ephemeral); `type 9` opens
  a [modal](#modals) instead. All callback types are in
  [Interactions in detail](#interactions-in-detail).
- `POST .../followups` with `data` as the body sends more messages, up to 5
  messages in total (callback `4`, the message posted after `type 5` and the
  follow-ups; acknowledging and editing count none).
- `PATCH .../messages/@original` edits the first response, and after a
  `type 5` posts it; `DELETE .../messages/@original` deletes it.
- **Ephemeral:** `"flags": 64` shows the response to the member who ran the
  command only. It is sent as the `interaction_response` WebSocket event to
  that member's sessions, shown as a private note in the channel, and **never
  stored**; if the member is offline they miss it. Each note has an `id`; an
  `interaction_response` with the same `id` replaces it (an edit of
  `@original`, callback `7` on its components), one with `"deleted": true`
  removes it.

The bot needs *Send messages* in the channel when it responds. A message a
response posts (callback `4`, a follow-up, the first `PATCH @original` after
callback `5`) and the new text of an update or an `@original` edit take
`allowed_mentions` exactly like a post: without it they notify the members
they name and never `@all`, `@here` or a role; with it they notify what it
asks for and the bot may. An update (callback `7`) or an `@original` edit
that sends `allowed_mentions` takes a post from the channel's limit of 5 per
5 seconds, like the REST edit (`429` leaves the interaction to be answered
otherwise). `flags` (`64`, ephemeral) is taken by callback `4`,
callback `5` and follow-ups; an `@original` edit refuses it (`400`, "flags
cannot be set on @original"), because the first response decides whether an
answer is ephemeral. Every interaction carries the member who caused it in
`member.user`. Interactions live in the server's memory: a restart
within the 15 minutes makes the token unknown (`404`). Components are not
affected: their `custom_id` is stored with the message and every click is a
new interaction (see [Message components](#message-components)).
Whatever closes a bot's connections also ends its pending interactions,
notes and modals: disabling or deleting it, regenerating or revoking a
token, changing its allowed intents. A bot enabled again within the 15
minutes cannot answer what it was sent before (`404`). An ephemeral answer
or a modal is shown only while the member can still view the channel; one
who lost access meanwhile is not shown it, and the answer is refused
(`403`, "the member can no longer see this channel"), like their own clicks
would be.

## Message components

A bot message can carry **components**: buttons and select menus in up to 5
**action rows**, in Discord's format. They go into `components` of
`POST /channels/{id}/messages`, of an interaction response (callback types 4
and 7, follow-ups, `@original`) and of `PATCH /channels/{id}/messages/{id}`.
Only bot accounts can set them, on their own messages.

```json
{"content": "Deploy v2.4 to production?",
 "components": [{"type": 1, "components": [
   {"type": 2, "style": 3, "label": "Deploy", "custom_id": "deploy:v2.4"},
   {"type": 2, "style": 2, "label": "Cancel", "custom_id": "cancel"},
   {"type": 2, "style": 5, "label": "Changelog", "url": "https://example.org/changes"}]},
  {"type": 1, "components": [
   {"type": 3, "custom_id": "env", "placeholder": "Environment",
    "options": [{"label": "Staging", "value": "staging"}, {"label": "Production", "value": "prod", "default": true}]}]}]}
```

| type | component | fields |
|---|---|---|
| `1` | action row | `components`: up to 5 buttons, or exactly one select |
| `2` | button | `style` `1` primary, `2` secondary, `3` success, `4` danger (each with `custom_id`) or `5` link (with `url`, `http` or `https`, no `custom_id`); `label` (up to 80 characters) and/or `emoji` (`{"name": "👍"}`, a Unicode emoji); `disabled` |
| `3` | string select | `custom_id`, `options` (1 to 25: `label` and `value` up to 100 characters, `description` up to 100, `default`; values unique), `placeholder` (up to 150), `min_values` (0 to 25, default 1), `max_values` (1 to 25, default 1), `disabled` |
| `5` | user select | members (not bots); `custom_id`, `placeholder`, `min_values`, `max_values`, `disabled` |
| `6` | role select | roles (not `@everyone`, not the managed bot roles); same fields |
| `8` | channel select | text, voice and forum channels (never a conversation); same fields |

A `custom_id` is 1 to 100 characters, unique within the message; it is the
bot's own state (`"deploy:v2.4"`), returned with every click. At most 5 rows,
25 components. A message may consist of components only (no content, no
embeds). Mentionable selects (type `7`) and Discord's newer layout
components are not offered; a message that contains them is refused (`50035`),
so a bot learns at once instead of posting a message with holes. Labels,
placeholders and option texts are cleaned like names (one line, no invisible
characters); a `custom_id` or an option `value` is returned exactly, so one
with control characters is refused instead of changed. An edit that leaves
the content as it is (only `components`, or callback `7` without `content`)
does not mark the message edited.

**Clicks.** Any member who can view the channel can use a message's
components, also in a read-only channel, unless they are timed out. The web
app shows them under the message (keyboard reachable, a link button opens
its address after the usual external link prompt). A click is
`POST /api/channels/{id}/messages/{id}/components`
`{"custom_id": "env", "values": ["prod"]}` (members and the web app; `values`
only for selects). The server checks the message still has that enabled
component, that string select values are among its options, the count
between `min_values` and `max_values` (each value once), and that user, role
and channel values are ones the member can see: active members (not bots) who
can view the channel, like a member option of a command, roles that exist,
channels the member can view **and the bot can view**. It then
creates a **new** interaction and delivers it to the bot (`202`, `409` when
the bot is not connected, `404` when the message, the component or the bot's
access is gone: the component disabled, the bot disabled or deleted or unable
to view the channel; a member who cannot view the channel gets the `404` of a
hidden channel, a timed-out one `403`). Clicks and modal submits are limited
to 30 a minute per member, apart from the 20 command runs. `message` is the
message as the bot API shows it at the click; one deleted meanwhile is not
delivered.

```json
{"op": 0, "t": "INTERACTION_CREATE", "s": 12, "d": {
  "id": "...", "token": "...", "type": 3, "channel_id": "...", "application_id": "<bot id>",
  "message": {"id": "...", "content": "Deploy v2.4 to production?", "components": [...], "...": "..."},
  "data": {"custom_id": "env", "component_type": 3, "values": ["prod"]},
  "member": {"user": {"id": "...", "username": "max", "display_name": "Max", "avatar_url": "/api/media/..."}}}}
```

User, role and channel selects add `data.resolved` with what was picked:
`users` (`id`, `username`, `display_name`, `avatar_url`), `roles` (`id`,
`name`, `color`) and `channels` (`id`, `name`, `type`), each keyed by id.

**Ephemeral notes** (responses with flags `64`) can carry components too.
They are not stored, so their components work for the 15 minutes the
interaction lives and only for the member who sees the note; a click is
`POST /api/channels/{id}/ephemeral/{noteID}/components` and reaches the bot
with `message: {"id": "<note id>", "flags": 64, "content": ..., "components": ...}`.
The server remembers at most 5,000 notes (1,000 per bot); a note beyond that
is still shown, but its components answer `404`.

## Modals

A **modal** is a small form the member fills in, opened as the response to a
command or a component click (not to a modal submit), with callback type 9:

```json
{"type": 9, "data": {"custom_id": "feedback", "title": "Feedback",
  "components": [
    {"type": 1, "components": [{"type": 4, "custom_id": "subject", "label": "Subject", "style": 1, "max_length": 80}]},
    {"type": 1, "components": [{"type": 4, "custom_id": "body", "label": "What happened?", "style": 2, "required": false, "placeholder": "Tell us more"}]}]}}
```

`title` up to 45 characters; 1 to 5 rows of one **text input** (type `4`)
each: `custom_id`, `label` (up to 45), `style` `1` short or `2` paragraph,
`min_length` (0 to 4000), `max_length` (1 to 4000), `required` (default
true), `value` (prefilled, up to 4000), `placeholder` (up to 100). The modal
reaches the member as the `interaction_modal` WebSocket event and the web app
shows it as a dialog. Submitting is
`POST /api/channels/{id}/modals/{modalID}` `{"values": {"subject": "...", "body": "..."}}`;
the server trims each answer, checks the lengths and required inputs (an
input left out is not filled in; an unknown one is refused) and delivers
`INTERACTION_CREATE` type 5:

```json
{"type": 5, "data": {"custom_id": "feedback", "components": [
  {"type": 1, "components": [{"type": 4, "custom_id": "subject", "value": "Login"}]},
  {"type": 1, "components": [{"type": 4, "custom_id": "body", "value": ""}]}]}, "...": "..."}
```

A modal can be submitted once, by the member it was opened for, within 15
minutes; after a restart of the server an open modal is gone (submitting
answers `404`, the web app says so and the member starts again). A submit
refused for its answers (`400`) or because the bot is not connected (`409`)
leaves the modal open. Closing it sends nothing to the bot.

## Interactions in detail

| Interaction `type` | When |
|---|---|
| `2` | a slash command ran (`data`: `id`, `name`, `options`) |
| `3` | a component was used (`data`: `custom_id`, `component_type`, `values`, `resolved`; `message` is the message it is on) |
| `5` | a modal was submitted (`data`: `custom_id`, `components`; `message` when the modal came from a component) |

| Callback `type` | Effect | Allowed for |
|---|---|---|
| `4` | post a message (`data`: `content`, `embeds`, `components`, `flags`) | 2, 3, 5 |
| `5` | acknowledge, the message follows (`PATCH @original` or follow-ups) | 2, 3, 5 |
| `6` | acknowledge a click without changing anything yet | 3 |
| `7` | update the message the component is on (`data`: `content`, `embeds`, `components`; absent fields stay) | 3 |
| `9` | open a modal | 2, 3 |

Callback `5` or `6` also tells the member that the bot is on it: the
`interaction_deferred` WebSocket event (`bot_id`, `channel_id`,
`interaction_id`, the id their run or click was answered with), so the web
app says the bot is taking longer instead of inviting another click.

Callback `6` or `7` on a modal submit that came from a component work like on
the component. The first answer must come within 15 minutes; follow-ups and
`@original` edits work for the same 15 minutes. The original response is
the message posted by callback `4` (or by the first `PATCH @original` after
callback `5`); for a component interaction answered with `6` or `7` it is
the message (or note) the component is on. `PATCH .../messages/@original`
takes `content`, `embeds`, `components` (absent ones stay) and
`allowed_mentions`, but no `flags`, and answers
the message (`200`), or `204` for an ephemeral note, which the member sees
replaced. `DELETE` answers `204`; afterwards `@original` is unknown
(`10008`), and so it is before an answer that has one (`409` before any
answer, `404` after a modal or before the message of a callback `5` is
posted). Updating or editing a message is allowed while the bot can send in
the channel, like any of its edits; it is announced as `MESSAGE_UPDATE` and
`message_update`. When the message a component is on was deleted after the
click, callback `7` answers `404` (`10008`) and the interaction can still be
answered otherwise (`4`, `5`, `6`). Follow-ups are refused after a modal
(`409`): the bot answers the submit instead.

## SDKs

Two official SDKs live in this repository and are tested against the server
in CI on every change: [TypeScript](../sdk/typescript/README.md) (Node 24 or
newer, no runtime dependencies) and [Go](../sdk/go/README.md) (module
`github.com/msch128/mnema-talk/sdk/go`). Both cover the REST API with rate
limits, the gateway with heartbeats, resume and reconnects, slash command
registration, interactions (`reply`, `deferReply`, `editReply`, `followUp`,
`update`, `deferUpdate`, `showModal`), components and modals, and the add
link. Each README shows how to install it and has three examples: an echo
bot, a slash command bot, and a buttons and modal bot.

A complete first bot in both languages is in the
[SDK quick start](bot-sdk-quickstart.md); [recipes](bot-recipes.md) answer common
tasks, [gateway events](gateway-events.md) shows every event's payload and
[allowed mentions](allowed-mentions.md) whom a message may notify.

```ts
import { Client, Intents, actionRow, button, ButtonStyle } from 'mnema-talk-sdk'

const bot = new Client({ baseUrl: 'https://talk.example.org', token: process.env.MNEMA_TOKEN!, intents: Intents.GuildMessages })
bot.on('messageCreate', async (m) => {
  if (m.content === '!ping') await bot.rest.createMessage(m.channel_id, {
    content: 'pong', components: [actionRow(button({ customId: 'again', label: 'Again', style: ButtonStyle.Primary }))] })
})
bot.on('interactionCreate', async (i) => { if (i.isButton() && i.customId === 'again') await i.update({ content: 'pong again' }) })
await bot.connect()
```

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
for direct messages to a bot is not part of 1.1. The chat of a conversation's
call counts as part of the conversation: since 3.0.0 its events reach no bot
and no outgoing webhook either.

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
| `POST /bots/manifests/check`, `POST /bots/install` | check a manifest, add a bot from it (see [Adding a bot from a manifest](#adding-a-bot-from-a-manifest)) |
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
| Adding someone else's bot | an OAuth2 invite link with permissions, to any server | a manifest (in an add link's fragment, or pasted) on your own server; channel permissions only, granted per channel the installer picks, never elevated |
| Components | buttons, selects (string, user, role, mentionable, channel), modals, newer layout components | buttons, selects (string, user, role, channel), modals with text inputs; no mentionable select, no layout components |
| Interaction tokens | 15 minutes | the same, kept in memory: a restart drops the ones in flight; components keep working, each click is a new interaction |
| Ids | snowflakes | UUIDs |
| A bot's names | `username` is the unique handle, `global_name` the display name | `username` is the display name (the name members see), `handle` the unique name members mention (`@bot-…`); there is no `global_name`. Kept this way because existing bots read `username` as the name (doc/api-stability.md) |
| Default access | bots get `@everyone` | bots get nothing until given a role |
| Member list | bots are listed, with a BOT tag | the same; webhook authors are not listed and are tagged Webhook (Discord tags them BOT) |
| The bot's role | every bot has a managed role named after it (the "integration" role) | the same: each bot has a managed role named after it, held by it only, renamed and deleted with it, with permissions under the bot invariant. Plus one shared managed role named Bot that every bot holds (it groups bots in the member list). Existing bots' grants were not moved into their new role |
| Presence | online, idle, do-not-disturb, invisible and an activity, set with gateway op 3 (and `presence` in IDENTIFY) | the same statuses, shown as online, away, do not disturb, offline; one activity (playing, listening, watching, custom, competing; no streaming); 5 updates per 60 s per bot, extra ones dropped; no presence events *to* bots |
| Timeout and ban | apply to bots as to members | the same: a timed-out bot cannot post or respond, a banned bot's token is refused and its gateway sessions close |
| Admin power | an invite with permissions | only the Owner can give Administrator or admin-area roles |
