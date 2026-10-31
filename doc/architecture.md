# Architecture

Mnema Talk is one Go binary that serves the REST API, a WebSocket hub, a
WebRTC selective forwarding unit (SFU) and the built Vue web app. State lives
in PostgreSQL; uploads, avatars and profile banners live in S3-compatible object storage
(SeaweedFS in the default `docker compose` stack).

```
            Browser (Vue 3 SPA)
     HTTPS / WSS │        │ UDP (DTLS-SRTP)
                 ▼        ▼
   ┌───────────────────────────────────────┐
   │ mnema-talk  (single Go binary)        │
   │  http router ─ REST handlers          │
   │  ws hub ───── events, presence,       │
   │               voice signaling         │
   │  sfu ──────── Pion WebRTC forwarding  │
   │  web.go ───── embedded web/dist       │
   └──────────┬─────────────────┬──────────┘
              ▼                 ▼
      PostgreSQL 18       SeaweedFS (S3 API)
```

## Backend components

| Package | Role |
|---|---|
| [`cmd/server`](../cmd/server/main.go) | Entry point: loads the config, sets up JSON logging, connects the database and storage, starts the retention worker, the SFU, the update checker and the HTTP server, shuts down gracefully. |
| [`internal/config`](../internal/config/config.go) | Reads every environment variable, applies defaults and validates them; the server refuses to start on a bad value. See [operations.md](operations.md#configuration-reference). |
| [`internal/server`](../internal/server/server.go) | Router wiring (chi): middleware, `/api/health`, `/api/metrics`, `/api/legal`, the API docs, the WebSocket endpoint, the authenticated and admin route groups, and the admin System tab ([`system.go`](../internal/server/system.go), [`system_update.go`](../internal/server/system_update.go), [`system_selfupdate.go`](../internal/server/system_selfupdate.go)). Integration tests live here too. |
| [`internal/httpx`](../internal/httpx/middleware.go) | Middleware: request IDs, security headers (CSP, HSTS in production), CORS, CSRF origin check, body limits, rate limits ([`ratelimit.go`](../internal/httpx/ratelimit.go)), client IP from trusted proxies ([`request.go`](../internal/httpx/request.go)), JSON errors. |
| [`internal/auth`](../internal/auth/service.go) | Password hashing, signed session cookies with a per-user `token_version` (revocation), login lockout, invite codes, admin seeding, user administration, password re-authentication for sensitive actions ([`reauth.go`](../internal/auth/reauth.go)). |
| [`internal/perm`](../internal/perm/perm.go), [`internal/roles`](../internal/roles/handler.go) | Roles and permissions: the permission bits, the in-memory `Snapshot` that resolves base, role and overwrite permissions (and the hierarchy rules), the cache with live invalidation, and the HTTP API for roles, member roles and channel overwrites. See [roles-and-permissions.md](roles-and-permissions.md). |
| [`internal/chat`](../internal/chat/handler.go) | Categories, channels (text and voice), layout, messages, replies, threads, reactions, mentions (by name, `@all`/`@here` and `@Role`, with a fan-out cap) and the mentions inbox, read state, notification levels ([`notify.go`](../internal/chat/notify.go)), search ([`search.go`](../internal/chat/search.go), filters in [`search_filters.go`](../internal/chat/search_filters.go)), members. Since 1.1 also direct messages (`dm*.go`), blocks ([`blocks.go`](../internal/chat/blocks.go)), reports ([`reports.go`](../internal/chat/reports.go)), saved messages ([`saved.go`](../internal/chat/saved.go)), forwarding ([`forward.go`](../internal/chat/forward.go)) and thread follow ([`thread_follow.go`](../internal/chat/thread_follow.go)). |
| [`internal/later`](../internal/later/service.go) | Reminders and scheduled messages: private per-member lists, the sweep that delivers and sends them, and their REST routes (see "Reminders and scheduled messages"). |
| [`internal/automod`](../internal/automod/rule.go), [`internal/moderation`](../internal/moderation/automod_rules.go) | Automod (1.2): rules compiled once per configuration version and checked in the send and sign-up paths (mention spam, new accounts, raid protection, word and link lists); a hit blocks or flags a message and adds an entry to the reports queue. `moderation` is the REST surface of the rules and of the per-member moderation history with private moderator notes. See [moderation.md](moderation.md). |
| [`internal/communityevents`](../internal/communityevents/handler.go) | Community events (1.2): events, RSVPs, cover images, the scheduler that starts events and sends reminders 15 minutes ahead. See [events.md](events.md). |
| [`internal/discordimport`](../internal/discordimport/template.go) | Discord structure import (1.2): preview, apply and undo of categories, channels, Talks and roles from a template link. See [import.md](import.md). |
| [`internal/onboarding`](../internal/onboarding/handler.go) | The first-run side of the community: name, welcome text and rules, who accepted which rules version, dismissed tips and the public invite preview. |
| [`internal/media`](../internal/media/handler.go) | Uploads with content sniffing and an allow-list ([`policy.go`](../internal/media/policy.go)), authenticated media serving through the app, admin storage stats and pruning, and the retention worker (off unless `MEDIA_RETENTION_DAYS > 0`). |
| [`internal/s3`](../internal/s3/s3.go) | S3 client (AWS SDK v2); creates the bucket only when missing. |
| [`internal/db`](../internal/db/db.go) | pgx pool and the embedded, numbered migrations in [`internal/db/migrations/`](../internal/db/migrations/). Each applied file's checksum is recorded in `schema_migrations`; an edited migration stops startup. |
| [`internal/events`](../internal/events/events.go) | The `Publisher` interface between feature packages and the hub (`Broadcast`, `SendToUsers`), with an in-memory recorder for tests. |
| [`internal/ws`](../internal/ws/hub.go) | WebSocket hub: one connection per browser session; chat events, presence and idle state, typing, voice rooms ([`voice.go`](../internal/ws/voice.go)), mute/deafen state, speaking indicators and WebRTC signaling for the SFU. |
| [`internal/sfu`](../internal/sfu/sfu.go) | Pion WebRTC SFU: one room per voice channel, forwards RTP without transcoding; per-viewer subscriptions ([`subscriptions.go`](../internal/sfu/subscriptions.go)); announced addresses from `WEBRTC_NAT_1TO1_IP`, re-resolved every 5 minutes ([`announce.go`](../internal/sfu/announce.go)). |
| [`internal/webhook`](../internal/webhook/), [`internal/ssrf`](../internal/ssrf/) | Webhooks: incoming URLs per channel in Discord's execute format (messages authored by a hidden bot account per webhook), outgoing signed events with a durable retry queue and delivery log, and the guard for outbound requests to member-chosen addresses (resolves names itself, refuses non-public addresses, connects to the vetted address). See [webhooks.md](webhooks.md). |
| [`internal/slash`](../internal/slash/slash.go), [`internal/slashcmd`](../internal/slashcmd/slashcmd.go), [`internal/poll`](../internal/poll/service.go) | The built-in slash commands: a registry of server-side commands that share the composer menu and the invoke route with bot commands (adding one is a `slash.Builtin` and a line in the server wiring). `slashcmd` holds `/me`, `/shrug`, `/tableflip`, `/unflip`, `/spoiler`, `/nick`, `/thread`, `/msg` and the moderation shortcuts `/timeout`, `/ban` and `/kick`; `poll` holds `/poll`. A command never decides on its own what a member may do. See [built-in commands and polls](bots.md#built-in-commands-and-polls). |
| [`internal/push`](../internal/push/handler.go), [`internal/mailer`](../internal/mailer/mailer.go), [`internal/mdplain`](../internal/mdplain/plain.go) | Web Push (VAPID keys, subscriptions, payloads in the account's language, previews cut to plain text by `mdplain`), and the few plain-text e-mails (address confirmation, password reset, two-factor alert, test message) sent through the operator's SMTP server. Without `SMTP_HOST` nothing is sent. German and English mail and push texts live in `locales/` files next to the code. |
| [`internal/emoji`](../internal/emoji/handler.go), [`internal/emojitext`](../internal/emojitext/emojitext.go), [`internal/opwarn`](../internal/opwarn/opwarn.go) | Custom emoji (uploads are redrawn, never stored as sent; `<:name:id>` in text), and operator warnings (configuration problems registered under a stable code and shown in the admin System tab). |
| [`internal/audit`](../internal/audit/), [`internal/bot`](../internal/bot/), [`internal/gateway`](../internal/gateway/) | The audit log with per-entry visibility classes, bot accounts, tokens (bound to the member who issued them) and slash commands, and the Discord-style bot gateway. See [bots.md](bots.md) and [moderation.md](moderation.md). |
| [`internal/metrics`](../internal/metrics/) | Process-wide counters (HTTP, realtime, SFU, storage, rate limits) rendered in the Prometheus text format without a client library; every labelled family has a series cap. Feeds `/api/metrics` and the live operator view in the admin System tab. See [operations.md](operations.md#monitoring-and-alerts). |
| [`internal/webbundle`](../internal/webbundle/), [`cmd/webbundle`](../cmd/webbundle/) | Builds, signs (Ed25519) and verifies the web app bundle that the [trusted desktop client](desktop-trusted-client.md) runs instead of server-served JavaScript. |
| [`internal/linkpreview`](../internal/linkpreview/linkpreview.go) | Link cards: fetches title, description and image of public pages with an SSRF guard (http(s) on 80/443 only, every resolved address checked, also on redirects) and a cache. |
| [`internal/imageverify`](../internal/imageverify/verifier.go), [`internal/updaterd`](../internal/updaterd/server.go) | The self-update sidecar ([`cmd/updater`](../cmd/updater/main.go)): checks the cosign signature and SLSA provenance of a new release image against the pinned release workflow identity, then pulls that digest and has the recreator restart the app. |
| [`internal/update`](../internal/update/checker.go) | Release check against GitHub every 30 minutes (ETag, backoff on 403/429), image-tag reach analysis ([`image.go`](../internal/update/image.go)) and the client for the optional updater sidecar ([`updater.go`](../internal/update/updater.go)).|
| [`internal/repo`](../internal/repo/limits_test.go) | The repository-wide checks: size limits for Go files and functions ([AGENTS.md](../AGENTS.md)). |
| [`internal/version`](../internal/version/version.go) | Version and commit baked in at build time (`VERSION` / `REVISION` build args). |
| [`web/web.go`](../web/web.go) | Embeds `web/dist` with `//go:embed` and serves the SPA. |

## Data flow: chat

1. The browser sends `POST /api/channels/{id}/messages` (or `/upload` for a
   single attachment in one request). Files for a message with several
   attachments are staged first: `POST /api/channels/{id}/attachments` stores
   one file as a media row with `staged = TRUE` and no message, visible only to
   its uploader; the message then lists up to 10 ids in `attachment_ids`, which
   are claimed in the message's transaction in that order (`media.position`),
   at most 100 MB together. A staged file nobody sends is deleted after six
   hours by [`media.SweepStaged`](../internal/media/staging.go). The request passes the CSRF origin check, the session check
   and the per-user rate limit.
2. [`chat.InsertMessage`](../internal/chat/post.go) stores the message and its
   mentions in one transaction (an upload's media row, or the claim of the
   staged files, joins the same transaction).
3. [`chat.PublishMessage`](../internal/chat/post.go) loads the stored message,
   broadcasts `message_create` through the `events.Publisher` and answers 201.
4. The hub writes the event to every open WebSocket; the
   [chat store](../web/src/stores/chat.ts) inserts it, updates unread and
   mention counters and plays the notification according to the channel's
   notification setting.

Unread state is derived, never fanned out per recipient. A member's read marker
is one `channel_reads` row per channel (the time they have read up to, plus
their notification setting); a send writes no marker for anyone. Unread and
mention counts are computed on read from the marker and the indexed
`(channel_id, created_at)` message order, so the cost of a send does not depend
on the number of members. A marker is written only when the client reports a
read: the web app sends at most one request per channel every two seconds
(leading and trailing), and the server writes only if another member's message
lies between the old marker and the new one (or the member has no marker yet),
so a second tab, Esc on a read channel or a retry costs one index probe and no
row. The "new since" divider is placed from the marker the client saw when it
opened the channel; when the first unread message is older than the loaded page,
`GET /api/channels/{id}/first-unread?since=...` (one indexed lookup) names it
and the "Jump to first unread" pill loads the history around it. Numbers:
[Capacity](#unread-tracking-with-10000-members).

Every channel has a text chat, voice channels included (the chat panel under
the Talk stage). The chat of a Talk follows the channel permissions only
(*View channels* to read, *Send messages* to write), not *Connect*, so a
member can read it from the Talk's row in the sidebar without joining. Edits, deletes, reactions, typing and read state follow the
same pattern: REST for changes, WebSocket events for fan-out. Media is never
served from S3 directly: `GET /api/media/{id}` checks the session and streams
the object through the app.

## Forum channels

Forum channels (`channels.type = 'forum'`, 1.2) are structured discussion spaces. Instead of an unthreaded message stream, a forum channel holds posts displayed as cards, each with an associated thread.

- **Post model**: A post reuses the existing thread model. It is a root message (`parent_id IS NULL`) in a forum channel with a required `title` (1..100 characters), an initial body (1..4000 characters), and up to 5 optional tags. Replies to the post form the thread (`messages.parent_id`).
- **Tags**: Each forum channel can define up to 20 admin-curated tags (`forum_tags`, name <= 20 characters, optional emoji <= 32 characters, a palette colour, a position and a moderated flag). Posts associate with tags via `forum_post_tags`. Per-forum rules (a tag is required, guidelines) live in `forum_settings`; a post's solved mark is `messages.is_resolved`; per-member read markers of posts are `forum_post_reads`.
- **Permissions**: *View channels* gates reading posts and tags; *Send messages* gates creating posts and posting replies in threads. Moderation (*Manage messages*) allows locking posts (`messages.is_locked = TRUE`), which restricts replies to moderators only, and editing tags on other members' posts.
- **Search and Unread**: Because posts are root messages, unread tracking, mentions, search indexing, and thread follows integrate seamlessly without a secondary store.

## Direct messages

Direct messages (1.1) are conversations of two to ten members. They are a
**deliberate reversal of migration 0003**, which removed the earlier 1:1 model
(recorded as a decision in the repository's decision store): the product
position changed from "no direct messages" to private conversations that
moderators cannot browse.

**Model.** A conversation is a row of `channels` with `type = 'dm'` (no
category, no name, no overwrites) with its members in `dm_members`
(migration 0050). Messages, reactions, attachments, polls and threads use the
one message pipeline, keyed by the conversation's id, so they inherit its
behaviour; a 1:1 conversation has a `dm_key` (the sorted pair), so a pair has
exactly one. Conversations have their own routes (`/api/dms`, see the OpenAPI
document) and events (`dm_created`, `dm_member_added`, `dm_member_removed`, and `dm_prefs_updated` to the
member's own sessions);
they appear in no channel list, layout, `channel_*` event, bot or webhook
payload, public message count or public message number (conversations count
down from -1 in a sequence of their own, so the public numbers show no gaps).

**The member's own list (1.2, migration 0074).** What a member does to their list
is stored per member and follows them to every device: `dm_members.hidden_at`
(a closed conversation, cleared by the next message written in it unless the
member blocked the sender), `dm_members.favourite_pos` (favourites, at most 25,
ordered), `users.dm_mute_all` and `users.dm_list_collapsed`. None of it touches
the conversation or its messages, none of it is visible to other members, and
every route checks membership first (a conversation you are not in answers 404,
also for the favourites list). `GET /api/dms` carries `hidden`,
`favourite_position` and `last_message` (the latest message the caller can see:
blocked authors are left out); with mute-all on and no setting of the
conversation's own, `notify_level` reads `mute` and `mention_count` 0, and push
sends nothing. The feature name is `dm.v2`.

**One access rule: membership.** `perm.Snapshot.Channel` answers for a
conversation before the Owner shortcut, from membership alone. A member may
read; writing, reacting and attaching also need *Send direct messages*, the
community switch `direct_messages_enabled` to be on, and no timeout or pending
rules. A non-member, the Owner and Administrators included, gets 0 and the
same 404 as for a missing channel, on every path that asks the snapshot (REST,
the WebSocket hub, media, push, reports). Searching a conversation also tests
membership in SQL, so a mistake in one layer does not open another. Nobody can
moderate a conversation: *Manage messages* never applies in it.

**Fan-out.** The hub delivers a conversation's events to its members' sockets
that asked for them (`/api/ws?dm=1`; a bundle from before direct messages
never receives one). They are never shown to the hub's `Tap` (outgoing
webhooks, bot gateway); only Web Push listens (`DMTap`), and it pushes to
members only, without a channel name. Bots and webhooks can neither open
conversations nor write in them.

**Starting and writing (the rules in `internal/chat/dm*.go`).**
- Whatever the reason a conversation cannot be started or a message cannot be
  sent because of *another member* (a block in either direction, their
  `dm_policy` of `nobody`, a disabled or erased account, a bot, an unknown id)
  the answer is the same 403 text, so none can be probed. A block makes an
  existing 1:1 conversation unwritable in both directions, silently; in a
  group, a block between two members does not stop the group, and nobody can
  add a member who blocked them. In any conversation (group or two-person),
  unread and mention counts and the last-activity time leave out messages by
  members the reader blocked, and push notifications skip blockers.
- 3 tries a minute and 10 new conversations per 24 hours per member, at most
  100 group conversations per member, at most 10 members. The member who
  started a conversation may send 2 messages before anybody else has written
  in it (counted in `channels.dm_creator_sent`, so deleting messages does not
  reset it).
- A conversation with one other person cannot be left; a group can, and when
  nobody is left it and its attachments are deleted. A member added to a group
  reads its whole history.

**What is never done with conversations.** They are not audited (an edit or
deletion leaves no `message.*` entry with text), not counted in `message_count`,
not in the mentions inbox or the channels' read states (`GET /api/dms` carries
its own unread and mention counts), not searchable except by their members
(`dm=true`), not shown in the admin media list, and not listed by the
administrative channel routes (they answer 404 for a conversation id).

**Moderation.** The only way a moderator reads anything of a conversation is a
*report* by a member (`POST /api/messages/{id}/report`): it keeps its own copy
of the message and up to 20 around it ([moderation](moderation.md#blocking-and-reports)).

**Privacy model.** Messages are readable by the server and by whoever holds
the database or backups, like channel messages: there is **no end-to-end
encryption of text** (see SECURITY.md). Account erasure blanks the member's
words in conversations (the others keep theirs) and deletes conversations
nobody is left in; the export lists the member's conversations by id and their
own messages.

**Calls.** A conversation has no call of its own: "Start a Talk" opens a temporary Talk and posts an invitation, and the server rings the other members (`dm_call_ring`, `dm_call_end`, a "missed call" message when nobody answers); see [Talks](talks.md#ringing).

## Blocking and reports

A block (`user_blocks`, migration 0049) is private: the blocked member is not
told, and nothing they can read depends on it. The server applies it where a
notification would reach the blocker: the mentions inbox (which excludes direct
messages), mention counts and push skip the blocked member's messages, decided
when the entry is listed or the push is sent. In any conversation (group direct
messages included), unread and mention counts and last activity leave out
messages by members the reader blocked. The web app collapses their messages to a
placeholder the blocker can open. A block changes nothing for anybody else. It makes
an existing 1:1 conversation unwritable in both directions (see
[Direct messages](#direct-messages)). The `block_updated` event goes to the
blocker's own sessions only.

A report (`message_reports`, migration 0049, permission *Manage reports*,
`1 << 21`) keeps its own copy of the message and up to 20 around it, so it survives an
edit or a delete. It is the only way a moderator reads anything of a conversation. Rules,
retention and the audit entries are in [moderation](moderation.md#blocking-and-reports).

## Anonymous polls

An anonymous poll (feature `polls.anonymous`) stores two facts that nothing
in the schema connects. This hides votes from every member, moderator and
administrator in the app; an operator with direct access to the database
server (its WAL, transaction ids or statement logs) could, in theory, still
correlate the timing of a voter row and a ballot. Mnema itself never stores
which option a member chose. `poll_voters (poll_id, user_id)` says that a member voted, which
makes a second vote impossible; it has no timestamp, so neither insert order
nor time can be matched against the other fact. The choice goes to
`poll_anon_ballots (poll_id, option_id)`: no member, no timestamp, appended in
the same transaction but by a separate statement. Readers add the unfolded
ballots to the counter `poll_options.anon_votes`, and `poll.RunFold` sums the
ballots into it and deletes them every ten minutes. `polls.anon_voters` is the
participant counter. No table, log line, audit entry, event, export or data export holds
a member next to a choice; a trigger on `poll_votes` (which still serves polls
that name their voters) refuses a row for an anonymous poll, and an integration
test walks every table of the schema to prove that no row mentions a voter and
an option together. A running anonymous poll broadcasts no tally at each vote, since in a small audience the
last vote would follow from it, and the REST and message payloads report 0 for every option until the poll ends (only the number of voters is live); the result goes out when the poll ends. Because a choice is never known, a vote cannot be changed
or withdrawn and the data export of a member has no entry for anonymous
polls. Erasing an account deletes its `poll_voters` rows and leaves the
counters, as a tally is not personal data. Migration 0063 converted the
anonymous polls that existed before 1.2 the same way (see
[upgrade.md](upgrade.md#11x--120)).

## Per-channel auto-delete

`channels.auto_delete_days` (0, 1, 7, 30 or 90; a check constraint also
forbids it on direct messages) is read by `chat.AutoDeleter`
([`internal/chat/auto_delete.go`](../internal/chat/auto_delete.go)), a
background loop started with the router. Each pass lists the channels that
opted in and works through each in transactions of at most 200 messages:
lock the expired rows (`FOR UPDATE ... SKIP LOCKED`, which also holds off
concurrent replies and uploads, so the list of attachment keys is complete),
collect their media keys, delete the rows, then delete the objects and
broadcast `message_delete`. The setting is re-checked inside every batch.
The audit entry per run is written by the sweeper itself.

## Forwarding and saved messages

**A forward is a reference, never a copy.** `POST /api/messages/{id}/forward`
creates an ordinary message in the target (a channel or a direct message) whose
text is only the forwarder's own comment. `messages.forward_of` points at the
root of what was forwarded (forwarding a forward points at the same root) and
`messages.forwarded` stays set when the original is later deleted
(`ON DELETE SET NULL`). The forwarder needs to see the original and to send in
the target, the target's slow mode and a conversation's rules apply, and every
refusal answers one 403 text.

**The visibility rule.** Nothing about the original is in the forwarded
message's payload, not even an id: only `forward: {forwarded: true}`. What a reader sees of the
original, with its ids, is returned by `GET /api/messages/{id}/forward`, decided for that
reader at that moment: the text, author and channel when they may see the
original (a channel they can view, or a conversation they are a member of; the
Owner has no special access to conversations), otherwise only
`available: false`. Lists, thread views, pins, search, the mentions inbox, the
WebSocket events, push notifications, the export and the payloads of bots and
webhooks therefore cannot show the original to someone who may not see it.
A forwarded message with an empty comment is pushed as "Forwarded message".
Edits and deletes of the original show through because the quote is read when
asked for.

**Saved messages** (`saved_messages`, migration 0051) are the same idea: a row
holds a reference and the member's note. `GET /api/me/saved` joins the messages
the member may see now; a conversation's messages need membership, tested in
SQL like search does.

## Reminders and scheduled messages

Both live in [`internal/later`](../internal/later/service.go) (migration 0062)
and are private to their owner. A background loop (every 15 seconds, started by
the router, `Service.Sweep`) delivers the due reminders and sends the due
scheduled messages. Times are absolute: the web app works them out in the
member's own time zone ("tomorrow morning" is 08:00 local) and the server only
checks that they lie between 10 seconds and a year ahead.

**A reminder is a reference** (`message_reminders`): at most 100 per member. The
message may be deleted later (`ON DELETE SET NULL`). `GET /api/me/reminders`
checks the member's access on every read, as saved messages do, and answers
`available: false` without text for a message they cannot see. When a reminder
falls due the row is claimed first (`delivered_at`, at most once), then the
member's sessions get `reminder_update` (`due`) and the push pipeline gets a
`push.Direct`. The access check runs again at that moment against the
permission snapshot (a conversation needs membership); a message the member can
no longer see is announced as "a message you can no longer see", with no ids,
author or text. A delivered reminder stays in the list until dismissed (or for
30 days) so a member who was offline finds it.

**A scheduled message is sent by the server as its author through the normal
send path** (`scheduled_messages`, at most 50 per member, text only). Creating
one needs Send messages in the channel, but nothing is promised: at `send_at`
the worker loads the account (disabled, banned and deleted accounts do not
send), builds the same send context as a request (`chat.SendContextFor`, so a
changed role, channel overwrite, pending rules or a timeout apply), validates
the reply and thread target again and inserts the message with
`chat.InsertMessage`, whose transaction applies slow mode and the direct message
rules (blocks, closed conversations). The scheduled row is deleted in the same
transaction, so a message is neither sent twice nor lost half-way, and a row
edited meanwhile is not sent in its old version. A refusal turns the row into a
failed draft with a reason code (`slow_mode`, `not_allowed`, `unavailable`,
`refused`; a 403 never says which rule, because it can concern another member)
and tells the author with `scheduled_message_update` (`failed`) and a push. The
author can edit the text, set a new time (which puts it back to waiting) or
delete it. A failure of the server itself (the database) leaves the message
waiting for the next round.

## Data flow: voice, cameras and screen share

Signaling runs over the existing WebSocket; media goes over UDP straight to
the SFU (`WEBRTC_UDP_PORT_MIN`–`MAX`), or through TURN when configured: the
relay built into the server ([`internal/turnrelay`](../internal/turnrelay/doc.go),
`WEBRTC_BUILTIN_TURN`) or an external one. The built-in relay opens its relayed
sockets on the server itself and forwards only to the SFU's own media sockets
(translating the announced `WEBRTC_NAT_1TO1_IP` address to the local socket and
back), so it needs no public relay ports and cannot reach anything else.

1. **Join.** Clicking a voice channel sends `voice_join`. The hub
   ([`voice.go`](../internal/ws/voice.go)) leaves any previous room, calls
   `sfu.Join` and announces the participant. The SFU is the offerer: it sends
   `webrtc_offer` and `webrtc_candidate`; the browser answers with
   `webrtc_answer` and its own candidates.
2. **Publish.** Each peer connection has fixed receive lines, told apart by
   position: the microphone (audio), the screen (first video line) and the
   camera (second video line). In 0.4.0 a second audio line carries the
   screen's sound separately (stream ID `screen:<user>`); before that, the
   screen's sound was mixed into the microphone track. Audio and screen are
   published under the user's ID as stream ID, a camera under `cam:<user>`
   ([`sfu.go`](../internal/sfu/sfu.go)).
3. **Forward.** For every incoming track the SFU creates one local track and
   copies the RTP packets to it ([`forward`](../internal/sfu/sfu.go)); a
   write error for one subscriber never ends the track for the others. No
   decoding, no transcoding, no recording. Viewers' keyframe requests (PLI/FIR)
   go to the publisher, rate-limited per track (300 ms), so a burst of
   viewers costs the publisher one keyframe.
4. **Subscriptions (opt-in).** What each viewer receives is decided per
   viewer ([`subscriptions.go`](../internal/sfu/subscriptions.go)): voice
   audio is always forwarded; cameras are on by default and can be turned off
   per person or for everyone; **screen shares are off by default** and only
   sent after the viewer clicks "Watch" (`webrtc_subscribe`). The screen's
   sound follows the screen subscription. Changes renegotiate with that
   viewer only. The hub tells the room who publishes what
   (`webrtc_media_state`) and who watches each share (`screen_viewers`), so
   clients can offer "Watch" and show viewers without receiving the video.
5. **Stop.** `webrtc_screenshare_stop` / `webrtc_camera_stop` unpublish that
   source; the screen opt-ins belong to one share and are dropped with it.
   Leaving, a failed connection or an admin kick removes the peer.

**Codec pinning.** The SFU pins the codec order of the first offer on each
video line ([`pinCodecOrder`](../internal/sfu/sfu.go)). Without it, Pion
re-offers every video line in the order of the client's screen line, where the
web client puts H.264 first; a browser would then switch a running camera
from VP8 to H.264 at the next renegotiation while the forwarded track keeps
the first codec, and viewers could not decode it. The client still reorders
its own screen line so screen shares prefer H.264 (hardware encoding) and get
a higher start bitrate ([`tuneScreenOffer`](../web/src/composables/useWebRTC.ts)).

**Layers of a screen share (0.8).** A sharer can send its screen as up to three
separate encodings of the same capture ("layers", best first), and each viewer
receives exactly one of them. The SFU offers three more receive lines for this,
after the four above: the screen's lower layers are the third and fourth
video line ([`screen.go`](../internal/sfu/screen.go),
[`layers.go`](../internal/sfu/layers.go)). They are plain lines, not RTP
simulcast (`rid`): the SFU is the offerer and a browser cannot add simulcast to
an answer, whereas separate lines work in every browser and need no SDP
munging. A client that does not know them answers them inactive, so old and
new clients interoperate in both directions. Per viewer the SFU forwards one
layer on one outgoing track and switches layers only where a keyframe of the
new layer starts (a VP8 keyframe tag or an H.264 parameter set, both of which
the browser's media transform leaves readable), asking the publisher for one
when it switches. Sequence numbers, timestamps and VP8 picture IDs and
`TL0PICIDX` are rewritten so the viewer sees one continuous stream; the sealed
payload is never touched. The sender declares the layers (`webrtc_layers`,
announced as `screen_layers`), a viewer picks one with `quality` on
`webrtc_subscribe` and is told what it receives (`screen_quality`), and the
sender is told which layers anyone needs (`screen_layers_wanted`) so it can
stop encoding the others. A layer nobody needs any more stays wanted for eight
seconds, so switching back and forth does not restart an encoder each time.
**Auto.** A viewer who leaves the choice to the server starts on layer 0 and is
moved by what its browser reports ([`adapt.go`](../internal/sfu/adapt.go)):
the loss fraction and round trip time of its RTCP receiver reports (about
every second), how often it asks for a keyframe, and a bandwidth estimate.
Lasting loss (5 % twice in a row, 15 % at once), a round trip time 250 ms above
its recent minimum or three keyframe requests in five seconds move it down one
layer; the reports of the three seconds after any move are ignored (a keyframe
burst loses packets on its own). After 12 seconds without trouble it tries one
layer up, and a try that fails within eight seconds makes the next one wait
twice as long (up to five minutes), so a link that carries the lower layer but
not the higher one does not flap. A viewer who picked a layer is never moved.
The SFU cannot read the media; it never estimates from it.

*Size of the picture.* The stage tells the server how large it shows the share
(`fit` on `webrtc_subscribe`: the index of the smallest layer that still has
the pixels the stage shows, its CSS height times `devicePixelRatio`, see
`fitLayer` in `web/src/lib/screenLayers.ts` and `useScreenFit`). Auto starts on
that layer and never goes above it, so a small tile or a share that is watched
but not on the stage costs a small stream. When the picture grows (fullscreen,
a bigger window) a viewer that sat on the old limit climbs on after four quiet
seconds per layer instead of twelve; a smaller picture moves it down at once.
The viewer's menu shows what Auto chose ("Auto (1080p)"). The bandwidth
estimate of a stream the link does not limit only ever reaches about 1.5 times
what is sent, so while it is still rising after a move up it is not held
against the better layer; it counts as a limit only when it stays below the
layer's bitrate and stops rising.

*Bandwidth* ([`bwe.go`](../internal/sfu/bwe.go)). A link that is too slow for
a layer but queues instead of dropping shows neither loss nor a higher round
trip time for a long while. So each viewer connection carries Pion's Google
Congestion Control estimator (`pkg/gcc` of `pion/interceptor`): the SFU stamps
the transport-cc sequence number on every outgoing packet (beneath the
authorization gate, so a refused packet never leaves a gap the browser would
report as lost), the browser answers with transport-wide feedback (arrival
time of each packet) and the estimator turns both into a bitrate. It is only
read: the pacer is a pass-through, so nothing is delayed or dropped, and
audio, cameras and viewers who chose a layer are unaffected. The cost is the
estimator's history of 250 packets and two small goroutines per connection.
The estimate is compared with the bitrate each layer has at the SFU (bytes
arriving per layer, measured once a second per share, smoothed; a layer the
sender switched off keeps its last size). Delivery that keeps up makes the
estimate about 1.5 times the bitrate sent, so an estimate below 1.2 times the
current layer's bitrate for four reports in a row counts as trouble and moves
the viewer down one layer. The estimate cannot rise above 1.5 times what is
being sent, so it can never show that a better layer fits; a viewer moved down
this way therefore stays put for a minute unless the estimate is above 1.5
times the better layer's bitrate for four reports, and then tries once more
under the usual backoff. The estimates are counted in
`mnema_sfu_bandwidth_estimate_reports_total{range}` (seven fixed ranges) and the
moves they cause in `mnema_sfu_bandwidth_layer_moves_total`. Browsers
that send no transport-wide feedback leave the estimate at "unknown" and the
rules above are the only ones. `BW_KBPS` of the load test gives each viewer a
link with a rate and a queue to try this.

Cameras and audio are not layered (small pictures, and every layer costs the
sender an encoder).

The web client ([`lib/screenLayers.ts`](../web/src/lib/screenLayers.ts),
`syncLayers` in [`useWebRTC.ts`](../web/src/composables/useWebRTC.ts)) sends
the share as the streamer chose it on layer 0 and up to two smaller ones
(1080p, 720p, 480p or 360p, at most 30 fps, at least a quarter smaller than the
layer above) from the same capture with `scaleResolutionDownBy` on the extra
lines. The lower layers stay switched off (`active=false`) until the server
says a viewer needs them, and layer 0 is switched off when everybody watches
a lower one. It uses these lines only when `server_info.screen_layers` says the
server has them. Viewers pick Auto or a layer in a menu on the stage, labelled
with the sizes the sender declared. The sender watches its capture: when the
width, height or frame rate it delivers (a resized window) has changed and
stayed so for a second, it sizes the best layer for the new picture and
declares the layers again; the server announces them only if they differ, and
the pickers follow live. A viewer's explicit choice is a position among the
layers and survives new sizes, but not a different number of layers or other
frame rates. Each layer's frames are sealed in a stream
of their own (see SECURITY.md), so a switch needs no coordination between
sender and receiver.

**Stream quality and volume (0.4.0).** The sharer chooses mode, resolution
and frame rate; the client applies them as capture constraints and sender
parameters (`maxFramerate`, a bitrate cap, `scaleResolutionDownBy`), so the
SFU still forwards whatever arrives. Viewers play the screen's sound in its
own element with a per-sharer volume (0–100 %, default 50 %), independent of
the person's voice volume.

**Media key lifecycle (0.8).** The browsers of a Talk form an MLS group and
derive the media keys from it; the server only relays the handshakes and
stamps the sender. The member with the lowest id commits a fresh key about
every 30 minutes, every member renews its own leaf key, members that showed no
sign of life for 20 minutes are removed and re-admitted when they return, and
every sealed frame is signed with its sender's leaf key. The connection
details show a **safety code** for the current epoch so members can compare
keys out of band. The server validates who may commit but never sees keys.
The code is [`web/src/lib/media/`](../web/src/lib/media/). Details and limits:
[SECURITY.md](../SECURITY.md#security-model-summary).

## Status expiry

A custom status may carry `users.status_expires_at` (migration 0061). The
client works out the absolute time from the member's own time zone ("tonight"
is 23:59 and "tomorrow morning" 08:00 where they are) and sends it as
`expires_at`; the server only checks that it lies ahead, at most a year. Two
mechanisms make the clearing exact without a timer per member: every read of a
user (`auth.userColumns`, the member list) maps a
status past its time to an empty one, so nobody sees a stale line even between
sweeps, and `Handler.RunStatusExpiry` (every 30 seconds, an indexed partial
scan) clears the rows and broadcasts the usual `user_update`. Setting another
member's status as a moderator never sets an expiry and always removes it.

## Notifications, search and onboarding

Three features that cut across the chat path. All of them are enforced on the
server; the client only renders the result.

**Notification levels.** A member sets `all`, `mentions` or `mute` ("Nothing") per channel
and per category, a mute optionally with an end time. The stored value
`inherit` (the default) follows the category, then `all`; the level a client
receives is always the effective one. Settings are rows in `channel_reads` and a
category counterpart (migration 0035) and are changed through
`PUT /api/channels/{id}/notifications` and `PUT /api/categories/{id}/notifications`.
Levels decide who is *notified* and what the mention badge counts. A mute stays
silent except for a message addressed to the member (by name, through a role
they hold, or as a reply); `@all` and `@here` do not break a mute. Both the
badge (`mention_count` in `GET /api/read-state`) and the push follow this rule.
Do Not Disturb silences everything. Since 1.2 a member also has a server-wide
default (`server_notify`, migration 0073; `PUT /api/notification-settings/server`,
`GET /api/notification-settings` lists it under `server`): the precedence is
channel, then category, then that default, then `all`. The default has no
`inherit` and does not reach direct messages. Its *Suppress @all and @here*
switch makes those two mention kinds count for nothing, at every level: no push
and no mention badge. Unread counts and the mentions inbox
(`GET /api/mentions`) are independent of the level, so a muted channel still
shows what was addressed to the member.

**Thread follow (1.1).** A reply notifies only the thread's followers (push and
the in-page notification alike). The author of the root, everyone who replies
and everyone mentioned in a reply follow automatically, in the reply's own
transaction (`thread_follows`, migration 0054, backfilled for existing
threads). `PUT`/`DELETE /api/threads/{id}/follow` change it; an explicit
unfollow is kept and a later reply does not undo it, but a reply that mentions
the member by name or quotes their message still reaches them. A change, and
a reply that makes someone follow, is sent to that member's own sessions as
`thread_follow_updated` (a hub tap, [`thread_follow_sync.go`](../internal/chat/thread_follow_sync.go)), and
`GET /api/threads/following` lists the followed threads for clients. The
in-page OS notification shows the message text only when the member's
"show message text" setting is on and the operator has not set
`PUSH_PREVIEWS=off`, like Web Push.

**Mentions.** A message stores one `message_mentions` row per notified member,
with a `kind` (by name, `@all`/`@here`, through a role) and the mentioned roles
in `message_role_mentions` (migration 0036), so a pill survives a role rename.
Webhooks and bots mention roles too, subject to `allowed_mentions`. An author
without the right to mention everyone notifies at most 50 members per message
([`mentions_cap.go`](../internal/chat/mentions_cap.go)).

**Search.** `GET /api/search` runs trigram-indexed substring matching on
message text (`pg_trgm`, one index since migration 0038) with filters (channel,
author, mentions, attachment kind, date window; since 1.1 also `has=poll`,
`has=edit` and `is=pinned`). Results are limited to the
channels the caller can read, using the same permission snapshot as message
listing; a query has a length and term cap, a database statement timeout and
at most two searches in flight per member.

**Onboarding and rules.** The Owner (or Administrators) writes a welcome text,
rules and a start channel through `/api/admin/onboarding` (migration 0037).
Publishing rules creates a rules version; a member who has not accepted the
current one is pending. Enforcement is part of the permission resolver
([`internal/perm`](../internal/perm/snapshot.go)): a pending member loses what
a timeout takes (posting, reacting, uploading, joining a Talk) and receives
`RULES_NOT_ACCEPTED`, in REST, the hub and the SFU alike. Empty rules enforce
nothing, which is why an upgrade never locks anyone out; the Owner and bots are
exempt. `GET /api/invites/{code}` (the one public read) serves the community
name for the sign-up page; it is rate limited and refuses weak legacy codes.

## The API contract

From 1.0.0 the public surfaces are versioned with semantic versioning
([api-stability.md](api-stability.md)): the REST API as `api/openapi.json`, the
WebSocket event registry in [`internal/events`](../internal/events/types.go),
bot and webhook payloads, the desktop bridge, environment variables and metric
names. The contract is enforced mechanically, not by convention:

- `make openapi-check` fails when `api/openapi.json` is stale and `make
  openapi-diff` fails on an incompatible change against the latest release
  tag, unless a commit carries a `BREAKING CHANGE:` footer.
- Event names are constants in `internal/events`; a golden file
  (`internal/events/testdata/events.golden.json`) and a comparison with
  `web/src/types/events.ts` fail the build when the registry and the client
  types drift.
- The alert rules in [`deploy/monitoring/mnema-alerts.yml`](../deploy/monitoring/mnema-alerts.yml)
  are checked with `promtool`, and an integration test fails when a rule names
  a metric `/api/metrics` does not export.
- `GET /api/health` lists optional features by name (`internal/server/health.go`); a new feature adds a name and never bumps `web_client_api`.
- Upgrade and restore drills install each supported release from GHCR, seed
  data through its own API, move to the build under test and check the data;
  a fresh-install check follows [deployment.md](deployment.md) on an empty
  directory.

The database schema, `internal/...` packages and the operator-only admin
endpoints are not part of the contract.

## Capacity

Measured on a 4-core home NAS with the default `APP_MEM_LIMIT=1g`: one browser
sharing a 640x360 screen at 20 fps to 99 members in one Talk used about 28 %
of one core and about 500 MB of app memory, each receiver getting about
320 kbit/s (0.7.5). App memory grows with receivers x forwarded video sources,
because Pion keeps a retransmission history of about 1.5 KB per packet for every
outgoing video track. Since 0.7.7 that history shrinks with the number of
connected members (1024 packets up to 16, halving as the count doubles, at
least 128 from 65 on; see `internal/sfu/nack.go`), but a connection keeps the
size it got when it joined, so the earliest joiners keep the large history: in
a measured Talk of 100 members watching a screen and ten cameras the live heap
reached 0.8 to 1.0 GB. The [sizing guide](benchmark.md#sizing-guide) gives
memory, CPU and upload for a given Talk. The price of the shrinking history is that
a late retransmission request goes unanswered and the picture shows an artifact
until the next keyframe. Memory is returned after the call. With about 16 real browsers plus 84
lightweight members (100 total) the app used about 1.6 cores. Real E2EE browser
clients were verified with up to 24 viewers in one call; beyond that the single
test machine, not the server, was the limit. These are single runs, not a
qualified capacity guarantee.

### Capacity table (server host)

The scenarios of [`loadtest/host-run.sh`](../loadtest/host-run.sh) run on the
server host itself, without browsers (see "On the host" in
[`loadtest/README.md`](../loadtest/README.md)). The figures are from one run
on the reference host (a 4-core x86-64 home NAS with 16 GB, server 2.2.0, other
members online: one), so read them as orders of magnitude. CPU is the app
process in cores, averaged over the steady phase; memory is its peak resident
set. The generator runs on the same host, so its own CPU (0.5 to 1 core in the
voice scenarios) is part of what the host spends.

| Scenario | Load | Server CPU (cores) | Server memory (peak) | Result |
|----------|------|--------------------|----------------------|--------|
| Chat, 100 sessions | 10 writers x 0.2 msg/s, typing, reactions, edits | 0.02 | 47 MB | messages p50/p95/p99 12/18/26 ms, no loss |
| Chat, 300 sessions | 30 writers x 0.2 msg/s | 0.09 | 62 MB | messages p50/p95/p99 14/21/44 ms, no loss |
| Chat, 1000 sessions | 100 writers x 0.2 msg/s | 0.67 | 102 MB | messages p50/p95/p99 33/84/115 ms, no loss (2.36 million deliveries) |
| Voice, direct | 1 screen (1500 kbit/s) to 99 members | 0.56 | 312 MB | 1585 kbit/s per member, 0.00 % loss, no freezes, first video p95 374 ms |
| Voice, through the built-in TURN relay | 1 screen and 3 speakers to 100 members | 1.61 | 312 MB | all 100 members through the relay, 0.00 % loss, one member froze for 12.6 s once (one connection flap) |
| Voice, mixed | 1 screen, 10 cameras (300 kbit/s), 3 speakers, 100 members watching all | 1.87 | 1044 MB | does not hold: 82 % screen and 34 % camera packet loss, members received 200 kbit/s of the screen (audio loss 0.3 %) |

The method, caveats and per-scenario detail are in [doc/benchmark.md](benchmark.md); the sizing table (members, speakers, cameras, screens to CPU, memory and upload) is in its [sizing guide](benchmark.md#sizing-guide).

What the host comfortably carries: chat for 1000 sessions, and one screen to
about 100 members directly or through the relay. Watching all of 10 cameras plus
a screen with 100 members (about 450 Mbit/s of media out of one 4-core host that
also runs the generator) is beyond it; the memory limit of 1 GB is reached there
too. The limit in that scenario is the shared host, not a leak: goroutines and
heap return to their start values afterwards.

The measurements above are reproducible with `make load-test` (see
[`loadtest/README.md`](../loadtest/README.md)): one sharer, real browsers and
lightweight Pion viewers, with join/reload stall checks, optional packet loss
and jitter, server heap and goroutine sampling, and thresholds that fail the
run. It can also run against a remote instance.

### Unread tracking with 10,000 members

The integration tests in
[`internal/server/unread_scale_integration_test.go`](../internal/server/unread_scale_integration_test.go)
(`go test -tags=integration -run 'Unread|ReadMarkers|SendingAMessage|Mentioning' -v ./internal/server`)
put 10,000 members into one community on PostgreSQL 18 and count what the
database really writes (row versions stamped by the test's own window, not
statistics). One run on an Apple M5 laptop, database in a local container,
single run, so read the figures as orders of magnitude:

| What | Result |
| --- | --- |
| Rows written by one message, 2 members vs 10,002 members | the same: 1 message row and 1 `users` row (the sender), 0 read markers for the members; the sender's own marker moves up to their message, which writes a row only if others posted since it (see below) |
| Median `POST /messages` time, 2 vs 10,002 members | 3.2 ms vs 2.8 ms (no difference beyond noise) |
| One message with `@all` to 10,001 members | 1 statement, 10,000 `message_mentions` rows, 185 ms (the only send that writes per recipient; needs the right to use `@all`) |
| All 10,000 members read the channel (32 concurrent workers) | 10,000 marker rows, 1.0 s in total (about 0.1 ms each) |
| The same 10,000 members read it again without news | 0 rows written, 0.28 s |
| `GET /api/read-state` for one member, 600 unread in a 50,000-message channel | 45 ms (cold), index scan on `idx_messages_channel_roots` |
| Same, 50,000 unread | 29 ms (warm) |
| First-unread lookup in that channel | 0.04 ms, 6 buffers, index scan |

Sending a message reads the channel for its author: `PublishMessage` calls `MarkRead` with the new message (top-level messages only; thread replies are no channel unread), which writes the sender's marker only when someone else posted since the last one, and sends `read_state` to the author's other sessions. The client does the same at once and drops the "new since" divider, so the "Jump to first unread" pill goes.

No migration was needed: the existing indexes serve every query (the plan is
printed by the test with `-v`). The remaining cost that grows with unread
volume is the count in `GET /api/read-state`, linear in the unread messages of
a member who has been away for a long time (about 30 to 45 ms for 50,000);
it is paid once per page load or reconnect, not per message.

## Talks, sessions and operator status (1.2)

**Lobbies, Talks and Stages.** A lobby channel (`channels.is_lobby`, migration 0059) opens a temporary Talk
(`is_temp`, `leader_id`) for the member who joins it. `chat.TalkManager` creates it, removes an empty one
after `DefaultTalkEmptyGrace` (30 seconds) and removes every temporary Talk at start. A Stage
(`channels.is_stage`, migration 0068) is a Talk in which talk power decides who may send
media; the permission *Manage stage* (`1 << 24`) grants it, hands raised and reactions are relayed over
the hub and kept nowhere. See [talks.md](talks.md).

**Sessions.** `user_sessions` (migrations 0064 and 0065) describes each signed cookie by its `jti`: a coarse
device label, creation and last activity. The cookie stays the credential; a periodic sweep
(`Handler.RunSessionSweep` in `internal/auth`) signs out a session that was inactive for longer than
`instance_settings.session_inactivity_days` (default 7), and the same condition is checked when the cookie is
used. See [operations.md](operations.md#sessions-and-inactivity-sign-out).

**View as role.** `GET /api/admin/roles/{id}/view-as` runs the normal permission resolver for a made-up member
that holds @everyone and the role, intersected with what the caller can view. It reads, it changes nothing.

**Operator status.** `GET /api/admin/status` (`internal/server/admin_status.go`, feature `ops.status`, Owner
and Administrators) gathers version, update state, database, storage, backup and live activity for the Status page.

## Frontend structure (`web/src`)

- **Entry**: [`main.ts`](../web/src/main.ts), [`App.vue`](../web/src/App.vue)
  (routing via [`lib/router.ts`](../web/src/lib/router.ts), layout, dialogs).
- **Addresses**: [`lib/routePaths.ts`](../web/src/lib/routePaths.ts) builds every
  address (`/c/:slug`, `/v/:slug`, `/c/:forum/p/:title-:shortId`, `/dm/:username`,
  `/dm/g/:shortId`) and `lib/router.ts` parses and resolves them client-side against the
  channels and conversations the member already sees: the current slug, then
  `previous_slugs`, then a legacy id; the last two are rewritten (replace) to the
  canonical address. The server has no slug-resolving endpoint. Slugs are assigned by
  `chat.assignSlug` inside the transaction that creates or renames a channel; former
  slugs stay reserved for the channel in `channel_slugs` (up to 10 newest, older ones
  are freed; temporary Talks keep a fixed `talk-<8hex>` slug that is never changed on
  rename and not kept in the history).
- **Stores (Pinia)**:
  [`auth`](../web/src/stores/auth.ts) (current user; the session itself is an
  HttpOnly cookie), [`chat`](../web/src/stores/chat.ts) (channels, messages,
  WebSocket events, unread state, presence),
  [`voice`](../web/src/stores/voice.ts) (call state, devices, input mode and
  push-to-talk key, per-person volumes, camera and stage choices),
  [`appVersion`](../web/src/stores/appVersion.ts) (reload banner on version
  mismatch), [`toast`](../web/src/stores/toast.ts).
- **Composables**: [`useWebRTC`](../web/src/composables/useWebRTC.ts) (peer
  connection, publishing mic/screen/camera, push-to-talk keys, screen offer
  tuning, stats), [`useTalkStage`](../web/src/composables/useTalkStage.ts) and
  [`useVideoGrid`](../web/src/composables/useVideoGrid.ts) (stage and grid
  layout), [`usePictureInPicture`](../web/src/composables/usePictureInPicture.ts),
  [`useSidebarReorder`](../web/src/composables/useSidebarReorder.ts),
  [`useSortableDrag`](../web/src/composables/useSortableDrag.ts) and
  [`useChannelLayout`](../web/src/composables/useChannelLayout.ts) (drag and
  drop with immediate save and undo), [`useNavMenus`](../web/src/composables/useNavMenus.ts)
  (context menus), [`useMessageActions`](../web/src/composables/useMessageActions.ts),
  [`useMessageKeyboard`](../web/src/composables/useMessageKeyboard.ts),
  [`useResizable`](../web/src/composables/useResizable.ts), [`useDialog`](../web/src/composables/useDialog.ts).
- **Components**: [`Sidebar.vue`](../web/src/components/Sidebar.vue),
  [`ChatArea.vue`](../web/src/components/ChatArea.vue),
  [`ThreadSidebar.vue`](../web/src/components/ThreadSidebar.vue),
  [`VoiceStage.vue`](../web/src/components/VoiceStage.vue) (the Talk view),
  [`ParticipantTile.vue`](../web/src/components/ParticipantTile.vue),
  [`VoiceChatPanel.vue`](../web/src/components/VoiceChatPanel.vue),
  [`ScreenViewers.vue`](../web/src/components/ScreenViewers.vue),
  [`PipHost.vue`](../web/src/components/PipHost.vue),
  [`MemberList.vue`](../web/src/components/MemberList.vue),
  [`AdminDashboard.vue`](../web/src/components/AdminDashboard.vue) with one
  component per tab (users, layout, invites, media,
  [system](../web/src/components/AdminSystemTab.vue)),
  [`UpdateBanner.vue`](../web/src/components/UpdateBanner.vue).
- **Libraries** (`lib/`): pure, unit-tested logic such as
  [`api.ts`](../web/src/lib/api.ts) (fetch wrapper), [`markdown.ts`](../web/src/lib/markdown.ts) (Discord-style Markdown: parser, renderer and the `registerTextRule` extension point in [`markdown/`](../web/src/lib/markdown)),
  [`channelLayout.ts`](../web/src/lib/channelLayout.ts),
  [`voiceSession.ts`](../web/src/lib/voiceSession.ts) (rejoin within 30 s),
  [`noiseSuppressor.ts`](../web/src/lib/noiseSuppressor.ts),
  [`thirdParty.ts`](../web/src/lib/thirdParty.ts) (in-app license list).
- **Styles**: Tailwind CSS 4 (`@import 'tailwindcss'` in
  [`style.css`](../web/src/style.css), the `@tailwindcss/vite` plugin; no
  `tailwind.config`). The Mnema design tokens are in its `@theme` block. The
  shared UI building blocks are plain CSS files in
  [`styles/`](../web/src/styles): `buttons.css` (the `.btn` family and the focus
  ring), `controls.css` (checkbox, radio, switch, slider, fields, select),
  `surfaces.css` (dialogs, menus, popovers, tooltips, toasts, list rows,
  elevation and motion) and `settings.css` (the cards and groups of the
  account and audio settings pages). They use tokens only and no inline style, because the
  Content-Security-Policy forbids it.
- **1.1 features in the UI**: direct messages
  ([`DmCreateDialog.vue`](../web/src/components/DmCreateDialog.vue),
  [`DmInfoDialog.vue`](../web/src/components/DmInfoDialog.vue)), blocking and
  reports ([`BlockedMessageRow.vue`](../web/src/components/BlockedMessageRow.vue),
  [`ReportDialog.vue`](../web/src/components/ReportDialog.vue)), saved messages and
  reminders in one header panel
  ([`SavedLaterPanel.vue`](../web/src/components/SavedLaterPanel.vue)), forwarding
  ([`ForwardDialog.vue`](../web/src/components/ForwardDialog.vue),
  [`ForwardedQuote.vue`](../web/src/components/ForwardedQuote.vue)), pinned
  messages with "Pinned by" ([`PinnedMessages.vue`](../web/src/components/PinnedMessages.vue))
  and the slash bar ([`SlashCommandBar.vue`](../web/src/components/SlashCommandBar.vue)).
  Each shows only when `GET /api/health` lists its feature.
- **i18n**: [`i18n/de.json`](../web/src/i18n/de.json) and
  [`i18n/en.json`](../web/src/i18n/en.json); tests fail on hard-coded UI text
  and unused keys.

## Key design decisions

- **One binary, three containers.** API, hub, SFU and web app ship together
  ([`web/web.go`](../web/web.go)); `docker compose` adds only PostgreSQL and
  SeaweedFS (optional profiles add `coturn` and the update sidecar). Pure Go (`CGO_ENABLED=0`), so images cross-compile for amd64 and
  arm64 without QEMU ([`Dockerfile`](../Dockerfile)).
- **Forward, never transcode.** The SFU only copies RTP
  ([`internal/sfu`](../internal/sfu/sfu.go)); quality is decided by the
  sender. Transport is DTLS-SRTP. Microphone, camera, screen video and
  screen-audio payloads are encrypted in the browser with MLS-derived keys,
  so the SFU cannot read them; an active or compromised server can still
  subvert group membership and serves the code, and metadata stays visible.
  Text and attachments stay readable by the server
  ([SECURITY.md](../SECURITY.md#security-model-summary)).
- **Media key lifecycle.** A new epoch (and so a new key) comes from every
  join, leave or kick, and otherwise every 30 minutes (+-2 min jitter) from a
  path commit by the committer. A member with no heartbeat or handshake for
  20 minutes is removed and must be let in again. Constants in
  [`bounds.ts`](../web/src/lib/media/bounds.ts), logic in
  [`group.ts`](../web/src/lib/media/group.ts), heartbeat format in
  [`heartbeat.ts`](../web/src/lib/media/heartbeat.ts). The e2e build
  (`MNEMA_E2E=1`) lets tests shorten them; release builds cannot.
- **Frame format and line binding.** Each sealed unit is `M3 | epoch | stream
  | counter | line | nonce | AES-GCM | Ed25519 signature`; the `line` byte
  (source in the high nibble, screen layer in the low one) is in the AAD and
  the signed bytes, and each receiving transform is armed with the source the
  SFU names for its line, so a unit moved to another line does not open
  ([`frame.ts`](../web/src/lib/media/frame.ts),
  [`mediaFrameWorker.ts`](../web/src/lib/media/mediaFrameWorker.ts)). Version 2
  (`M2`, no line) was removed in 1.2.0: it is neither sent nor opened, and a
  unit that carries it is counted and reported to the member as coming from an
  outdated client. Members still sign a capability message
  ([`capability.ts`](../web/src/lib/media/capability.ts), `M` `C`, next to the
  heartbeat) so that older clients, which choose their format by it, switch to
  version 3 ([SECURITY.md](../SECURITY.md)).
- **Screen shares are opt-in per viewer** so an idle viewer costs no
  bandwidth ([`subscriptions.go`](../internal/sfu/subscriptions.go)).
- **Invite-only, one Owner.** No public registration; the Owner is seeded
  from `ADMIN_USERNAME` ([`admin_seed.go`](../internal/auth/admin_seed.go)) and
  holds every permission; roles below them are managed through
  [`internal/perm`](../internal/perm/perm.go) and enforced server-side
  ([roles-and-permissions.md](roles-and-permissions.md)).
- **Sessions revocable by version.** Password change or "log out everywhere"
  bumps `token_version`, invalidating every cookie and closing the user's
  sockets ([`session.go`](../internal/auth/session.go), [`token.go`](../internal/auth/token.go)).
- **Forward-only, checksummed migrations** run at startup
  ([`db.go`](../internal/db/db.go)); never edit an applied one.
- **A strict page policy.** The web app is served with `script-src 'self'
  'wasm-unsafe-eval'`, `style-src 'self'` and `style-src-attr 'none'`
  ([`SPAContentSecurityPolicy`](../internal/httpx/middleware.go)): no inline
  script or style, so the Vue code sets no `style` attributes and uses classes
  and custom properties set from script. [SECURITY.md](../SECURITY.md#security-model-summary)
  has the full header list.
- **Retention off by default.** `MEDIA_RETENTION_DAYS=0` deletes nothing
  ([`admin.go`](../internal/media/admin.go)).
- **The app never touches Docker.** Self-update goes through an opt-in
  sidecar on an internal network with a token, admin password re-auth, a
  5-minute cooldown and an audit log entry; the sidecar verifies the new
  image's cosign signature and provenance before it pulls anything
  ([`internal/imageverify`](../internal/imageverify/verifier.go),
  [`internal/updaterd`](../internal/updaterd/server.go),
  [`system_selfupdate.go`](../internal/server/system_selfupdate.go),
  [`docker-compose.yml`](../docker-compose.yml),
  [SECURITY.md](../SECURITY.md#self-update-sidecar)).
- **Reload, never auto-reload.** The server sends its version on every
  WebSocket connection; a page built for another version shows a reload
  banner ([`appVersion.ts`](../web/src/stores/appVersion.ts)).
