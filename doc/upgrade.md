# Upgrade notes

What operators need to know when moving between versions, and the update
procedure for a self-hosted (NAS) compose deployment. The full list of
changes is in [CHANGELOG.md](../CHANGELOG.md); the configuration is
described in [operations.md](operations.md#configuration-reference).

Database migrations are embedded in the binary and run automatically at
startup, in order, and are **forward-only**: an older version cannot use a
schema a newer one migrated. Take a backup before every update.

## Broad trusted proxies

`TRUSTED_PROXY_CIDRS` entries wider than an IPv4 `/24` or an IPv6 `/64` used
to log a startup warning when the listener was not loopback. Production and
staging now refuse to start with that configuration. A single proxy address,
an IPv4 prefix of `/24` or longer, an IPv6 prefix of `/64` or longer, and any
range while the listener is loopback, still start. Set the value to the
reverse proxy's address before updating, or the new process exits during
configuration and the previous container is already gone.

## Update procedure (compose, released image)

Run in the deployment directory (where `docker-compose.yml` and `.env` are).
Replace the version and paths with yours.

```sh
cd /path/to/mnema-talk
NEW=1.0.0                                  # version to install

# 1. Back up database, media and .env (see operations.md, "Backups")
BACKUP_DIR=/path/to/backups ./scripts/backup.sh

# 2. Note what runs now, for a rollback
grep '^MNEMA_IMAGE=' .env                  # e.g. ghcr.io/msch128/mnema-talk:0.9.0
curl -fsS http://127.0.0.1:8080/api/health # {"status":"ok","version":"..."}
cp docker-compose.yml docker-compose.yml.bak

# 3. Fetch the compose file (and scripts) of the new tag
base=https://raw.githubusercontent.com/msch128/mnema-talk/v$NEW
curl -fsSLo docker-compose.yml        "$base/docker-compose.yml"
curl -fsSLo .env.example.new          "$base/.env.example"
curl -fsSLo scripts/backup.sh         "$base/scripts/backup.sh"
curl -fsSLo scripts/backup-common.sh  "$base/scripts/backup-common.sh"
curl -fsSLo scripts/upgrade-postgres.sh "$base/scripts/upgrade-postgres.sh"
curl -fsSLo scripts/restore.sh        "$base/scripts/restore.sh"
curl -fsSLo scripts/health-check.sh   "$base/scripts/health-check.sh"
chmod +x scripts/*.sh
# New or changed variables since your .env was written:
diff <(grep -o '^[A-Z_0-9]*=' .env | sort) <(grep -o '^[A-Z_0-9]*=' .env.example.new | sort)

# 4. Point the app at the new image
if grep -q '^MNEMA_IMAGE=' .env; then
  sed -i "s|^MNEMA_IMAGE=.*|MNEMA_IMAGE=ghcr.io/msch128/mnema-talk:$NEW|" .env
else
  echo "MNEMA_IMAGE=ghcr.io/msch128/mnema-talk:$NEW" >> .env
fi

# 5. Check the configuration, pull and start
docker compose config -q
docker compose pull app
docker compose up -d              # add --profile turn / --profile autoupdate if you use them

# 6. Verify
docker compose ps                 # app "healthy" after ~15-30 s
curl -fsS http://127.0.0.1:8080/api/health
docker compose logs --since 10m app | grep -E 'migration applied|"level":"(WARN|ERROR)"'
```

Then open the app: other open tabs show a reload notice; voice calls
reconnect after the reload. Admin → System shows the new version.

If you follow a minor line instead (`MNEMA_IMAGE=ghcr.io/msch128/mnema-talk:0.4`),
step 4 is unnecessary within that line: `docker compose pull app && docker
compose up -d app`, or "Update now" in the System tab when the updater
sidecar is enabled (see the [deployment guide](deployment.md#updates)). A new minor
line (e.g. `0.4` → `0.5`) always needs this full procedure.

If you build on the server instead (`MNEMA_IMAGE=mnema-talk:local`):
`git fetch --tags && git checkout v$NEW && docker compose up -d --build`
replaces steps 3–5.

### Rollback

> **Do not roll back below this release once roles or private channels exist.**
> Migration 0017 adds the tables that restrict access. A binary that does not
> know them (0.7.x before roles, or older) would ignore them and show every
> private channel to everyone. This release and later ones refuse to start on a
> database migrated by a newer version, but binaries from before that guard
> existed cannot. To go back, restore a backup taken before the update.


Stop the app before changing its image or database. If the new version applied
migrations (`migration applied` in its log), restore the pre-update database
and media before starting the old app. **Released 0.4.x binaries, including
0.4.4, have no downgrade refusal guard.** The newer migration runner adds
that guard; it cannot protect an older binary. Never run the old image against
the migrated PostgreSQL 18 database.

For a PostgreSQL 17 → 18 upgrade, use the saved 0.4.x compose file and image
with the preserved `postgres-data` volume. PostgreSQL 18 has a separate
`postgres18-data` volume; keep both until the rollback or recovery is verified.
The preserved PostgreSQL 17 database reflects the upgrade's cutover, so writes
made afterward on PostgreSQL 18 are not in that rollback.

```sh
set -e
docker compose stop app postgres
cp docker-compose.yml.bak docker-compose.yml
sed -i "s|^MNEMA_IMAGE=.*|MNEMA_IMAGE=ghcr.io/msch128/mnema-talk:0.4.4|" .env # the saved old version
docker compose pull app
docker compose up -d --wait postgres seaweedfs
# If the preserved volume is unavailable or a backup rollback is required,
# restore a complete pre-update backup here, while the app is still stopped:
# ./scripts/restore.sh /path/to/backups/<timestamp> --verify
# ./scripts/restore.sh /path/to/backups/<timestamp> --yes
docker compose up -d --wait app
curl -fsS http://127.0.0.1:8080/api/health
```

For an update that kept the same database volume, the backup restore is
required if migrations changed its schema. The restore replaces the database
and media with the backup's state and preserves the current `.env` and compose
configuration. Keep the complete backup from step 1 and the saved compose file
until the new release has passed your application checks.

## Version notes

### 1.1.x → 1.2.0

Update with the procedure above and take the backup first. 1.2.0 only adds. A
1.1.x web app keeps working against a 1.2 server, and a 1.2 web app hides what
a 1.1.x server lacks (it reads the `features` list of `GET /api/health`).
Nothing in `.env` has to change. A rollback to a 1.1.x image needs the restore
of the pre-update backup (see "Rollback" above).

#### Database

| Migration | Adds |
|---|---|
| 0059 | `channels.is_lobby`, `is_temp`, `lobby_id` and `leader_id`: Create-a-Talk lobbies and the temporary Talks they open (see [Talks](talks.md)). Existing channels keep `false` and null. |
| 0061 | `users.status_expires_at` (a status line that clears itself, feature `status.expiry`). It also raises the limit of `channels.slow_mode_seconds` from 6 hours to 24 hours. |
| 0062 | `message_reminders` and `scheduled_messages` (see "Reminders and scheduled messages"). |
| 0063 | `poll_voters`, `poll_anon_ballots`, `poll_options.anon_votes`, `polls.anon_voters` and a trigger on `poll_votes` (anonymous polls); `channels.auto_delete_days` with its checks. |
| 0064 | `user_sessions`: one row per browser session (device label, created, last active). |
| 0065 | `instance_settings.session_inactivity_days` (1 to 365, default 7). |
| 0066 | `automod_rules` and `automod_state` with the default rules (below), and the columns `source`, `automod_rule`, `automod_outcome` and `hit_count` of `message_reports` (existing reports keep `source = 'member'`). |
| 0067 | `moderation_notes`: private moderator notes about members. |
| 0068 | `channels.is_stage`: Stages. Existing channels keep `false`; a Stage is never a lobby or a temporary Talk. |
| 0069 | `channels.type = 'forum'`, `messages.title`, `messages.is_locked`, `forum_tags` and `forum_post_tags`: Forum channels with tag management and post threads (see [Forum channels](forums.md)). Existing rows keep null and false. |
| 0070 | `events` and `event_rsvps` tables: community events, RSVPs (see [events](events.md)); the new `Manage events` permission is granted to no role by default. |
| 0071 | `discord_imports` and `discord_import_objects`: the record of each Discord structure import and the ids it created, for undo (see [Import](import.md)). Nothing existing is touched. |
| 0072 | `media_original_filename_trgm_idx`: GIN trigram index on `media.original_filename` for fast case-insensitive attachment filename search. |
| 0073 | `server_notify`: a member's server-wide notification default (see [Notification settings](#notification-settings)). Adds a table only. |

#### Message search

- **New search filters and ordering.** The message search API (`GET /api/search`)
  supports `during:YYYY-MM-DD` (matching messages within a single local day,
  accepting `tz` offset parameter from the client), `filename:<text>` (matching
  original filenames of attachments case-insensitively via trigram index),
  `author:bot|webhook|member` (filtering by author type), `has:embed`, and a
  `sort=newest|oldest|relevance` parameter (default `newest`).
- **Interactive filter dropdown and chips.** When the search box is focused,
  suggestion chips allow quick selection of `from:`, `in:`, `mentions:`, `has:`,
  `during:`, `author:`, and `filename:` filters, rendered as removable chips in
  the query line. A sort order toggle in the results header allows switching
  between Newest, Oldest, and Relevance.
- **Feature flag.** Advertised as `search.filters.v3` in `GET /api/health`.
  A web client connected to an older server hides the new dropdown and sort toggle.

#### Talks

- A Talk can be marked as a lobby; joining it opens a temporary Talk ("Talk 1",
  "Talk 2", ...) for the member, who leads it. Members need no *Manage channels*
  for that. Empty temporary Talks are removed after 30 seconds, and the server
  removes all of them at start.
- "Start a Talk" in a direct message opens a temporary Talk from the first lobby
  the member can use and posts an invitation card. The card only links to the Talk;
  joining runs the usual permission checks.
- Feature name `talks.lobby` in `GET /api/health`. New events `talk_leader` and
  `talk_moved`; new `voice_kicked` reasons `talk_rate_limited` and
  `talk_unavailable`. REST: `PATCH /api/talks/{id}`, `POST /api/talks/{id}/invites`,
  `POST /api/dms/{id}/talk`, and the fields `is_lobby`, `is_temp`, `leader_id` of a
  channel (additive).
- A Talk can be a **Stage** (feature `talks.stage`): without talk power nobody sends
  audio, camera or screen there. The new permission *Manage stage* (bit `1 << 24`, off
  for everyone but the Owner and Administrators) gives and takes talk power; members
  raise a hand to ask. Nothing about a Stage session is stored. Existing Talks stay
  normal Talks. New events `stage_hand`, `stage_talk_power`, `talk_reaction`; REST:
  the field `is_stage` of a channel and of `POST /api/admin/channels` and
  `PATCH /api/admin/channels/{id}` (additive).
- **Live reactions** (feature `talks.reactions`): a member in a Talk sends an emoji
  that floats over their tile for everyone in that Talk, at most 5 per 10 seconds.
  The server relays it and keeps nothing.
- Role and overwrite editors list *Manage stage*. A role that holds *Administrator*
  has it already; no existing role or overwrite changes.

#### Automod and moderation history

**Defaults at the upgrade.** Automod is on after the upgrade, with conservative
rules that every server gets the same way (new servers included). Look at
them under **Admin > Automod** and change or delete what does not suit you:

| Rule | Default |
|---|---|
| Mention spam | on: a message with more than **10** different mentions is blocked |
| New accounts | on: accounts younger than **10 minutes** cannot post links (blocked) |
| Raid protection | on: more than **10 joins within 10 seconds** time the newcomers out for 10 minutes and add one alert to the reports queue |
| Blocked words | **off**, word list empty |
| Links | **off** (block-list mode, empty list) |

The Owner and Administrators are never checked. The effect you may notice: a
member who joined a few minutes ago gets "it breaks the server rule" when
posting a link, and a burst of invitations used at once (a party of more than 10
within 10 seconds) is held back for 10 minutes; delete or loosen the rule if
that does not suit your community. Nothing changes for existing messages.

**Moderation tab.** Members with *Manage reports* get a Moderation tab in the
profile (history and private moderator notes). No permission changes hands:
the Owner and Administrators have it, other roles only if you gave them
*Manage reports*. Notes about a member are moderator data; they are not in the
member's own data export (see [moderation](moderation.md#moderator-notes-are-moderator-data)
and the privacy page, "Moderation records").

**API.** Mostly additive: the report objects gain `source`, `automod_rule`,
`automod_outcome` and `hit_count`, `kind` may be `alert` and `category`
`automod`. New routes `/api/admin/automod*` and
`/api/admin/users/{id}/moderation*`. The health `features` list gains
`automod` and `moderation.history`. **One contract change:** `message_id` and `channel_id` of an admin
report entry (`GET /api/admin/reports`, `/reports/{id}`) are now optional: an
automod entry about a blocked message (never stored) has no `message_id`, and a
raid alert has neither. Clients must treat both as optional; reports filed by
members always carry them.

#### Operator status

- A dedicated **Admin → Status** page (feature key `ops.status`, endpoint `GET /api/admin/status`, Owner and Administrator only) provides an at-a-glance operational overview: version, available updates and auto-deploy status, database size and migration count, storage reachability and disk free space, backup status, live activity (active sessions, WebSocket connections, Talks and participants), and active operator warnings. See [operations.md](operations.md#operator-status).

#### Discord import

- *Admin*, *Import* copies the categories, channels, Talks and roles of a Discord
  server from a server template link (see [Import](import.md)). It is limited to
  the Owner and members with *Administrator*, strictly additive (appended at the
  end, roles below every existing role, never Administrator, `@everyone`
  untouched), runs after a preview and three confirmations in one transaction, and
  can be undone.
- Feature name `import.discord` in `GET /api/health`. REST: `POST
  /api/admin/import/discord/preview`, `POST /api/admin/import/discord/apply`,
  `GET /api/admin/import/discord/imports`, `GET
  /api/admin/import/discord/imports/{id}` and `POST
  /api/admin/import/discord/imports/{id}/undo` (all additive). Audit actions
  `import.discord` and `import.discord_undo`, visible to the Owner and
  Administrators only.
- The server needs outbound HTTPS access to `discord.com` for the preview and the
  import. Nothing else is contacted, and nothing is sent to Discord beyond the
  template code.

#### Forum channels

- Admins can create channels with type `'forum'` (`Manage channels`). A forum channel holds posts; each post is a root message with a required title (<=100 characters), an initial message, and up to 5 optional tags. Replies form a standard thread (`messages.parent_id`).
- Up to 20 admin-defined tags per forum channel (name <=20 characters, optional emoji <=32 characters). Members can filter posts by tag and sort by latest activity or creation.
- Moderation: Members holding *Manage messages* can lock posts to prevent replies from regular users and update tags on others' posts.
- Feature name `channels.forum` in `GET /api/health`. New event `forum_tags_changed`. REST: `GET/POST /api/channels/{id}/tags`, `PATCH/DELETE /api/channels/{id}/tags/{tagID}`, `GET/POST /api/channels/{id}/posts`, and `PATCH /api/channels/{id}/posts/{postID}`.

#### Community events

- Scheduled community events with title, markdown description, start and end times, optional cover image (re-encoded 3:1 banner, max 5 MB), and location (see [events](events.md)).
- Location can be a Talk channel or free text. Talk-based events are only visible to members holding *View channels* on that Talk.
- New permission `Manage events` (`1 << 25` in `internal/perm`); no role gets it by default (Administrators and the Owner have it implicitly).
- RSVPs: `going`, `interested`, and `not_going` responses, with participant counts and avatars.
- Automated reminders sent 15 minutes before event start via in-app notifications and Web Push to members RSVP'd `going` or `interested`.
- Automatic transition from `scheduled` to `active` at start time, showing a direct "Join Talk" button for voice events.
- Feature key `events` in `GET /api/health`. Real-time WebSocket events `event_update` and `event_reminder`.
- Audit logging for `event.create`, `event.edit`, and `event.cancel`.
- Account erasure deletes personal RSVPs and clears event creator links; account export includes `rsvps.json`.

#### Notification settings

- Migration 0073 adds `server_notify` (one row per member who changed their
  server-wide default: level, optional mute end, "suppress @all and @here").
  Without a row a member keeps today's behaviour: every channel without its own
  or its category's setting notifies for all messages.
- Precedence: channel, then category, then the server default. The level in
  `GET /api/read-state` (`notify_level`) is the effective one, so existing
  clients show it correctly without changes. A server-wide "Nothing" behaves like
  a channel mute: only messages addressed to the member by name, through a role
  or as a reply still notify and count. Direct messages ignore the server default.
- Feature name `notify.levels.v2` in `GET /api/health`. New
  `PUT /api/notification-settings/server` (`level` `all`, `mentions` or `mute`,
  `muted_until` for a timed mute, `suppress_everyone`); `GET /api/notification-settings`
  gains a `server` object (additive). Changes reach the member's other sessions
  with the existing `read_state` refresh event. Account export and erasure
  include the new row.
- Desktop client: the rail's right-click menu can call the same endpoints as the
  web app (`PUT /api/channels/{id}/notifications`,
  `PUT /api/categories/{id}/notifications`, `PUT /api/notification-settings/server`);
  it should hide the server-wide entries when `notify.levels.v2` is missing.

### 1.0.x → 1.1.0

Update with the procedure above and take the backup first. 1.1.0 only adds.
A 1.0.x web app keeps working against a 1.1 server, and a 1.1 web app hides
what a 1.0.x server lacks (it reads the `features` list of `GET /api/health`).
Nothing in `.env` has to change.

#### Database

Eight migrations run at the first start. All are additive and forward-only. A
rollback to a 1.0.x image needs the restore of the pre-update backup, because
the 1.0.x image refuses a schema that a newer version migrated (see
"Rollback" above).

| Migration | Adds |
|---|---|
| 0049 | `user_blocks` and `message_reports`. |
| 0050 | Direct messages: `dm_members`, the `dm` channel type, `users.dm_policy`, `instance_settings.direct_messages_enabled`, `messages.in_dm`. The permission *Send direct messages* is added to @everyone. |
| 0051 | `saved_messages`. |
| 0052 | `messages.forward_of` and `messages.forwarded`. |
| 0054 | `thread_follows`, filled from the existing threads (root authors and repliers) so threads keep notifying the people who took part. |
| 0056 | `bot_tokens.minter_inferred`, a flag for tokens bound to their likeliest issuer (see "Bots and webhooks"). |
| 0057 | The nullable column `users.banner_s3_key` (the profile banner). It also extends the tombstone trigger of 0043 so a deleted account keeps no banner. |
| 0058 | `users.avatar_reencoded_at`, the marker of the one-time avatar redraw (see "Profile pictures"). |

The numbers 0053 and 0055 are unused.

#### New permissions

| Permission | Bit | Default |
|---|---|---|
| *Manage reports* | `1 << 21` | Off for @everyone and every existing role. |
| *Send direct messages* | `1 << 22` | On for @everyone (set by migration 0050). An administrator can take it away. |
| *Kick members* | `1 << 23` | Off for @everyone and every existing role. |

The Owner and Administrators have all three implicitly, so nobody else gains
access to reports or kicking until a role is given the permission.

#### Configuration

- **New:** `INITIAL_LANGUAGE` (`de` default, or `en`) names the default
  categories and channels of a brand-new installation. An upgrade never
  renames anything.
- **Changed defaults:** `LEGAL_OPERATOR_COUNTRY` and `LEGAL_PROJECT_NOTICE` are
  now empty by default. The legal page then shows a translated default (the
  country "Germany" and "Private, non-commercial project") instead of the former
  fixed German text. A value you set in `.env` is unchanged. If you relied on
  the old defaults and use the English interface, set the two variables.
- **Self-update (profile `autoupdate`):** fetch the compose file of the 1.1
  tag and run `docker compose --profile autoupdate up -d` after the update. The
  `updater` service is now this project's verifying sidecar (started from the
  app image). A second service, `recreator` (the Watchtower that used to be the
  updater), only restarts the app from the image the updater verified. "Update
  now" refuses an image whose cosign signature or build provenance does not
  verify. It needs outbound HTTPS from the updater to the registry and to
  Sigstore. The new networks `mnema-recreator` and `mnema-updater-egress` are
  created by compose; a custom compose file needs the same two services and
  networks. Details: [operations.md](operations.md#verifying-a-release-image),
  [SECURITY.md](../SECURITY.md#self-update-sidecar).

#### What members and moderators get

- **Direct messages.** Conversations of two to ten members
  ([architecture](architecture.md#direct-messages),
  [SECURITY](../SECURITY.md#direct-messages-privacy-model)). The Owner can
  switch them off for the community (`direct_messages_enabled`, under **Welcome
  & rules**): existing conversations stay readable and nothing new can be
  written. Members choose in their privacy setting (`dm_policy`) who may start
  conversations with them. Channels and messages are numbered as before;
  private messages are numbered apart.
- **Blocking and reports.** Members block each other and report a message to
  the moderators ([moderation](moderation.md#blocking-and-reports)). In any
  direct message conversation (group too), unread counts, mention counts and the
  last-activity time leave out messages by members the reader blocked, and push
  recipients skip blockers. The admin console gets a **Reports** tab for
  holders of *Manage reports*.
- **Profile banner** (feature `profile.banner`). A picture across the top of a
  member's profile, set under **Change banner** next to **Change avatar** and
  removed with **Remove banner**. The server never stores the upload: it crops
  the centre 3:1, shrinks it to at most 1500x500 and writes a new file, so EXIF
  and other metadata are gone. PNG, JPEG, GIF or WebP up to 5 MB and at least
  300x100 pixels; an animated GIF stays animated within the same pixel budget as
  animated emoji, an animated WebP is rejected. The file is stored under
  `banners/` (the daily orphan check covers it), is deleted when replaced or
  removed, goes into the data export as `banner.*` and is removed when the
  account is erased. Without the feature (a 1.0.x server) the 1.1 web app shows
  no banner controls.
- **Profile pictures** (privacy fix). Until 1.0 an avatar was stored exactly as
  uploaded, so the EXIF block of a phone photo (GPS position, camera model) was
  readable by every member. From 1.1 an avatar or banner is decoded, cropped
  (square, or 3:1 for a banner), shrunk (at most 512x512, or 1500x500, never
  enlarged) and written again, so EXIF, XMP, trailing bytes and polyglot payloads
  cannot survive. Limits and types are unchanged (5 MB; PNG, JPEG, GIF, WebP).
  Animations stay within the pixel budget animated emoji have; a one-frame GIF
  becomes a PNG and an animated WebP is rejected. Bot and webhook avatars follow
  the same rules. Chat attachments are stored byte for byte, so a photo sent as
  a file keeps its metadata. A **Remove avatar** button sits next to **Change
  avatar** (`DELETE /api/users/me/avatar`, feature `profile.avatar_remove`).
- **Avatars stored before 1.1 are redrawn** once by a background job (migration
  0058). It starts 30 seconds after the server, processes the accounts without
  the marker, writes the new file, points the account at it and only then deletes
  the old object (a short extra copy per avatar exists meanwhile). It repeats
  hourly until nothing is left, so a storage outage at start only delays it, and
  it never touches an avatar twice. Old files were accepted without limits, so
  the job is more lenient than an upload: a canvas of up to 30,000 pixels an edge
  and 64 million in total, and for a long or large animation only its first
  frame. An old avatar that cannot be decoded even so is **removed**: the file is
  deleted, the account has no avatar, and an audit entry `media.avatar_removed`
  with the reason is written (Owner and Administrators only). Keeping the original
  would keep its metadata public. The member can set a new avatar.
- **Saved messages.** A private list of up to 1,000 messages per member with
  an optional note of up to 200 characters. A row is a reference, never a copy,
  and access is checked on every read: a message the member can no longer see
  comes back as unavailable.
- **Forwarding and "Pinned by".** A message can be forwarded into a channel or
  a direct message ([architecture](architecture.md#forwarding-and-saved-messages)).
  Older web apps show a forwarded message as just its comment; a forwarded message
  with an empty comment is pushed as "Forwarded message". The pinned list
  shows who pinned a message and when.
- **Slash commands.** `/me`, `/shrug`, `/tableflip`, `/unflip`, `/spoiler`,
  `/nick`, `/thread`, `/msg`, `/timeout`, `/ban` and `/kick` join `/poll` in the
  menu ([bots](bots.md#built-in-commands-and-polls)). `/kick` signs the member
  out on every device and drops them from their Talk; they can sign in again.
- **Threads and search.** A thread reply notifies only the thread's followers:
  the root's author, repliers, people mentioned in a reply and anyone who
  followed it by hand. Search gains the filters `has` (file, image, video, link,
  poll, edit), `is=pinned`, `after` and `before_date` (see `api/openapi.json`).
- **A Talk's chat without joining.** A chat button on each Talk row opens the
  Talk's chat with a *Join* button in its header. The chat follows the channel
  permissions only (*View channels* to read, *Send messages* to write,
  [roles](roles-and-permissions.md)). No server change.
- **Web app.** A skip link and a fixed Tab order, the connection banner also
  on the browser's offline event, Talk controls that stay visible when alone in
  the call on a phone, and in-page notifications that follow the "show message
  text" setting. The style is built on Tailwind CSS 4; nothing to configure.
- **Audit log.** New entries `message.edit_history_view` and `poll.end`; the
  `message.delete` entry of an edited message carries its earlier versions.
  Audit retention applies to them as to any entry.

#### API, events and export

All changes are additive; `web_client_api` is unchanged.

- `GET /api/health` has a `features` list: `blocks`, `dm`, `reports`, `saved`,
  `forward`, `search.filters.v2`, `threads.follow`, `slash.builtins`,
  `profile.banner`, `profile.avatar_remove`.
- New routes: `/api/me/saved`, `/api/messages/{messageId}/forward`,
  `/api/threads/{id}/follow`, `/api/threads/following`, `PUT` / `DELETE
  /api/users/me/banner` (multipart field `banner`), `DELETE /api/users/me/avatar`, plus the routes for
  blocks, reports and direct messages (see `api/openapi.json`).
- `banner_url` is added to `GET /api/users/{userID}` and `GET /api/auth/me` only,
  never to message, member or event payloads.
- Messages carry a `forward` marker (`{forwarded: true}`, no ids and no quoted
  text, for bots and webhooks too). Messages and the pin event carry
  `pinned_at` and `pinned_by` while pinned.
- New events: `dm_created`, `dm_member_added`, `dm_member_removed`,
  `block_updated` (only to the blocker's own sessions), `saved_updated` (only to
  the member's own sessions), `thread_follow_updated` and `kicked`.
- The web app asks for conversation events by opening the socket as
  `/api/ws?dm=1`. A desktop client from before 1.1 forwards the socket without
  that query, so conversations there do not update live until the client is
  updated (the list and sending work).
- The account export gains `blocks.json`, `reports.json`, `saved.json` (ids,
  notes and times, not the saved text), `thread_follows.json` and `banner.*`. Erasing an
  account removes the saved list.

#### Security

- **Stricter style policy.** The web app's Content-Security-Policy no longer
  allows inline styles: `style-src 'unsafe-inline'` became `style-src 'self'`
  plus `style-src-attr 'none'` (see [SECURITY.md](../SECURITY.md#security-model-summary)
  under "Security headers"). Nothing to configure. A browser tab that was
  loaded before the update may show the emoji picker unstyled until it is
  reloaded; installed apps update on their next start. The desktop shells apply
  the strict web CSP (`style-src 'self'; style-src-attr 'none'`) only for web
  bundles >= 1.1.0; older bundles keep the old policy. The bundle's
  `security_floor` is not raised.
- **Bots and webhooks.** Bot tokens issued before 1.0 are bound to their
  likeliest issuer the first time they are used; nothing is revoked and no token
  needs regenerating ([bots.md](bots.md#tokens)). The admin console marks tokens
  that were bound by assumption.
- **Media frame format.** No change. Refusing frame version 2 was re-evaluated
  and deferred to 2.0 (SECURITY.md).

### 1.0.0 → 1.0.1

A patch release: fixes and polish, no new features, and nothing in it changes
a public surface ([API stability](api-stability.md)). Update with the
procedure above; take the backup first.

- **No migrations.** The schema is the one 1.0.0 left, so a rollback to the
  1.0.0 image needs no restore.
- **Three new rate limiters**, per member, each under its own label in
  `mnema_ratelimit_hits_total{limiter}` (see
  [operations.md](operations.md#what-each-metric-means)): `poll_create` (10 polls per minute),
  `pin` (60 pins or unpins per minute) and `emoji_upload` (10 uploads per
  10 minutes). Members and bots never meet them in normal use; a script that
  creates polls or pins in bulk now gets `429` with `Retry-After`.
- **Unknown or deleted custom emoji are refused as new reactions** (`400`,
  "invalid emoji"): a token whose emoji was deleted, or never existed, cannot
  be added. A reaction that already exists can always be taken back.
- **The data export is complete.** Besides profile, settings, messages, edits,
  reactions, uploads, audit entries and sign-in setup, the archive now holds
  the polls the member started, the messages they pinned, their push devices
  (label and dates only, no keys) and the custom emoji they uploaded (with the
  image files). Its `README.txt` also says that the files are English and
  machine-readable and keep messages as typed.
- **Mute rule, now the same everywhere.** In a muted channel or category, a
  message addressed to the member breaks the mute: `@username`, a role the
  member holds, or a reply to their message. `@all`, `@here` and Do Not
  Disturb stay silent. The unread mention badge, Web Push and the notifications
  shown while the page is open follow this rule alike.
- **A deleted account is named "Deleted member" on the server too.** New
  deletions store that name; accounts deleted under 1.0.0 keep the stored
  "Deleted user" (the web app shows "Deleted member" in the member's language
  for both). Bots and API clients that read `display_name` see the stored text.
- **The release workflow mirrors the required checks.** `release.yml` reads the
  checks that `main`'s ruleset requires (including the `upgrade-drill` jobs)
  and reports each one's result from the tested commit on the release pull
  request; it does not merge when one of them did not succeed. Nothing to do for
  operators; forks with their own ruleset get the same behaviour.
- **Recovery codes have their own rate limiter.** The authenticator lockout no
  longer blocks them, so a member locked out of the app can still sign in with a
  recovery code.
- **Bot and webhook names are stricter.** ZWJ and ZWNJ stay only inside emoji
  and in scripts that need them; other invisible characters are stripped.
- **`@everyone` and `@here` are reserved on a normalized skeleton**, so look-alike
  spellings of those names are refused too.
- **Push and OS notification text is plain text.** Markdown is stripped and
  spoilers show as "Spoiler".
- **Mail language:** the member's stored language, then the request's
  `Accept-Language`, then English.
- **Erasing an account updates open pin panels** right away.

### 0.9.x → 1.0.0

1.0.0 is the first release under the [API stability contract](api-stability.md):
nothing in it is a breaking change to a public surface, and `1.x` keeps working
for existing clients, bots, webhooks, dashboards and alerts. What changes for an
operator is below. Update with the procedure above; take the backup first.

- **Ten migrations run on first start: 0038 to 0045, 0047 and 0048** (0046 is
  intentionally unused). All are quick on any instance; 0044 and 0047 are
  described under custom emoji and edit history below.
  - 0038 drops `idx_messages_content_trgm`, a duplicate of the trigram search
    index `messages_content_trgm_idx` (both came from earlier releases), so
    message writes no longer maintain the same index twice and search is
    unchanged. `DROP INDEX` takes a short exclusive lock on `messages`; the
    migration runs in a transaction, so it is not `CONCURRENTLY`. On a large
    instance this takes well under a second.
  - 0039 adds the nullable column `bot_tokens.created_by` (who issued a bot
    token), with a foreign key to `users`. It rewrites no rows and holds only a
    brief lock on `bot_tokens` while the foreign key is added.
  - 0040 adds the nullable columns `users.email` and `users.email_verified_at`
    (with a partial unique index on confirmed addresses) and the table
    `account_tokens` for single-use e-mail links. Adding nullable columns
    rewrites no rows. No account has an address afterwards.
  - 0041 adds the tables `user_totp`, `recovery_codes`, `login_challenges`
    and `second_factor_failures` for optional two-factor sign-in. They start
    empty; nobody is asked for a code until they turn it on.
  - 0042 belongs to Web Push and the installable web app (see below).
  - 0043 adds the nullable column `users.deleted_at`, the table `data_exports`
    and replaces the audit log's immutability trigger function with one that
    allows blanking names and details during an account deletion (and nothing
    else). No existing row changes.
  - 0045 adds the nullable columns `messages.pinned_at` and `messages.pinned_by`
    (who pinned a message and when; existing pins get their last-change time)
    and a small partial index on pinned messages. Nothing else changes: the
    pin flag already existed, and the limit of 50 pins per channel applies to
    new pins only, so a channel that already has more keeps them.
  - 0048 adds the tables `polls`, `poll_options` and `poll_votes` for the
    `/poll` command. They start empty and nothing else changes.
  - The app waits for these locks like any other statement; there is nothing to
    stop or schedule. If startup is slow, check `docker compose logs app` for
    `migration applied`.
- **Edit history for moderators (migration 0047).** The new table
  `message_edits` keeps the previous text of every edit from now on (up to 50
  per message). Members with *Manage messages* can read it from the message
  menu; nobody else can, and it is not searched or exported to anyone but the
  author. It starts empty and the migration touches no existing row. Nothing to
  configure; `EDIT_HISTORY_RETENTION_DAYS` (default `0`, keep) drops old
  versions after that many days. See [moderation](moderation.md#edit-history).
- **Unread divider and "Jump to first unread".** No migration and no setting.
  The new read-only endpoint `GET /api/channels/{id}/first-unread` is additive.
  Marking a channel read now writes nothing when it has no news since the last
  read (the first read still stores the member's marker), which reduces writes
  on busy instances; see [Capacity](architecture.md#unread-tracking-with-10000-members).
- **Desktop client is Beta.** The Windows desktop client stays Beta at 1.0.0;
  see [desktop-app.md](desktop-app.md).
- **Optional e-mail, SMTP and password reset by mail.** Members may now add an
  e-mail address in Account & security (never required, never shown to other
  members or to the Owner). Setting `SMTP_HOST` (see
  [operations.md](operations.md#outgoing-e-mail)) lets the server confirm an
  address by a single-use link and lets members reset a forgotten password with
  an address they confirmed (link valid 30 minutes, signs every device out). Without
  `SMTP_HOST` nothing changes: no mail is sent, saved addresses stay unconfirmed
  and unused, and the "Forgot your password?" link does not appear. The Owner can
  still set a temporary password for any member (Admin > Members). Reset by mail
  also works for the Owner account if the Owner confirmed an address. Put
  `SMTP_PASSWORD` in the environment or mount a file and name it in
  `SMTP_PASSWORD_FILE`; `SMTP_TLS=none` is refused in production.
- **Two-factor sign-in (optional).** Every member may turn on TOTP in Account
  & security; the Owner is advised to. A client that does not know the new
  second step (an old browser tab, a script) gets `202` with a challenge instead
  of a session for such an account and cannot sign in; the shipped web app and
  desktop clients handle it. Stored authenticator secrets are encrypted with a
  key derived from `JWT_SECRET`: rotating it requires resetting two-factor for
  the accounts that use it (Admin > Members, or `mnema-talk reset-2fa <username>`
  for the Owner). See [operations.md](operations.md).
- **Members can delete their account and export their data.** Both are in
  Account & security; the Owner account cannot be deleted. A deleted account
  leaves its messages in place without text, written by "Deleted member" (1.0.0
  stored "Deleted user" and keeps it for accounts deleted before 1.0.1; the web
  app ignores the stored text and names a deleted author in the member's
  language from the reserved `deleted-…` username). Exports need object storage
  and live there for at most 24 hours. Clients built before this release show a
  deleted author by the placeholder name instead.
  See [operations.md](operations.md#account-deletion-and-data-exports).
- **Weak custom invite codes stop working.** Custom invite codes typed by an
  admin that fall below the new minimum (12 or more characters, 7 or more
  distinct, letters and digits below 16 characters, no common word with little
  around it) are treated as unknown by the invite preview and by registration.
  Admin > Invites marks them "Disabled, regenerate"; delete them and create new
  ones (a random generated code is always accepted). Generated codes and
  custom codes that meet the minimum are unaffected. Invite lookups and
  registrations are also limited per IPv6 /48 in addition to the per-client
  limits.
- **Uploads have a daily budget per account.** `UPLOAD_QUOTA_MB_PER_DAY`
  (default `2048`) is what one member, bot or webhook may upload in any rolling
  24 hours; further uploads answer 429, and deleted files still count. `0`
  turns it off. Two optional guards are off by default: `MEDIA_MAX_TOTAL_MB`
  (pause all uploads once stored media reaches this size, `0` = no cap) and
  `MEDIA_MIN_FREE_MB` (default `1024`) together with `MEDIA_DISK_PATH` (a
  volume the app can see; empty = no free-space check). A busy community that
  shares large files will hit the default; raise it before updating if members
  upload more than 2 GiB each per day. `MEDIA_RETENTION_DAYS` still defaults
  to `0`: nothing is deleted automatically. See
  [operations.md](operations.md#configuration-reference).
- **coturn is capped** (only if you run the `turn` profile). The compose file
  now starts coturn with `--user-quota=8` (relayed sessions per credential) and
  `--total-quota=48` (all sessions, matching the 49 ports of
  `49152-49200`). Fetch the compose file of the tag (step 3) and restart the
  profile. If you widened the relay port range, raise `--total-quota` with it;
  see [TURN](operations.md#turn-coturn).
- **`PUBLIC_URL` over plain `http://` warns.** In production and staging, a
  `PUBLIC_URL` that is not `https://` logs a startup warning and shows
  "PUBLIC_URL is not https" under Admin > System. The server still starts, so a
  trusted-LAN install keeps working; behind a TLS-terminating reverse proxy set
  it to the public `https://` address (see
  [operations.md](operations.md#public_url-and-secure-cookies)).
- **Bot tokens are bound to whoever issued them.** From 1.0 a token records its
  issuer, and stops working when that person loses *Manage bots*, is disabled,
  or can no longer act as the bot. Tokens created before the upgrade have no
  recorded issuer: they keep working and are not re-checked until someone
  regenerates them ([bots](bots.md#tokens)). Regenerate the tokens of every bot
  you still use to bind them, and delete the ones you do not.
- **Audit log visibility.** Category and layout entries (`category.*`,
  `layout.apply`) are visible to the Owner and Administrators only, like the
  other privileged entries.
- **Realtime limits are stricter.** Sockets end with the session that opened
  them (a sign-out closes them at once), oversized or flooding frames are
  dropped, and there are per-account budgets for frames, events and voice
  handshakes across connections. Ordinary use is not affected. A reverse proxy
  must still pass WebSocket upgrades; nothing else changes.
- **Mention fan-out is capped.** A message whose author may not mention everyone
  notifies at most 50 members (named users first, then role members).
- **Security floor trailer.** The desktop client's security floor (the lowest
  web bundle it accepts) now rises only for a commit with a
  `Security-Floor: raise` trailer; a `fix(security)` subject alone no longer
  does. Operators need to do nothing. Forks and maintainers must add the
  trailer to security fixes in anything the web bundle ships, see
  [desktop-trusted-client.md](desktop-trusted-client.md) and `AGENTS.md`.
- **Alert rules moved.** The example alert rules are now the file
  [`deploy/monitoring/mnema-alerts.yml`](../deploy/monitoring/mnema-alerts.yml),
  checked with `promtool` and against `/api/metrics` in CI. Metric names are
  part of the stability contract.
- **Web Push (migration 0042).** Members can turn on push notifications per
  device (Account menu > App & notifications) and get messages while the app
  is closed; on iPhone and iPad only for the installed app. The migration
  creates `push_vapid` (the server's VAPID key, generated on first start: it
  is in your database backups, so restores keep working subscriptions),
  `push_subscriptions` (one row per device) and `users.push_preview`. Nothing
  is sent until a member subscribes. New settings, all optional:
  `PUSH_ENABLED` (default `true`), `PUSH_PREVIEWS` (`user`, default, or `off`
  to never put message text in a push) and `PUSH_VAPID_SUBJECT`; see
  [operations.md](operations.md#web-push-and-the-installable-app). The server
  needs outbound HTTPS to the browser vendors' push services (Google, Mozilla,
  Apple, Microsoft) when a member uses push; it connects only to public
  addresses. Rolling back needs the pre-upgrade backup like every migration.
- **Custom emoji (migration 0044).** Members holding the new permission *Manage
  emoji* (the Owner always does; nobody else gets it from an upgrade) add PNG,
  GIF or WebP pictures under Admin > Emoji; everyone uses them as `:name:` in
  messages and reactions. 0044 creates the table `custom_emoji` and widens
  `message_reactions.emoji` from 32 to 80 characters (a custom emoji reaction is
  the token `<:name:id>`); no row is rewritten. `EMOJI_MAX` (default `200`, `0`
  forbids new ones) caps the count. Uploads are decoded and redrawn on the
  server (at most 128 x 128, metadata and trailing bytes dropped, 256 KiB,
  SVG/JPEG/animated WebP refused) and stored under `emoji/` in object storage.
  Older web bundles show the token text `<:name:id>` instead of the picture.
- **Installable app.** The web app now has a manifest, icons and a service
  worker (`/sw.js`) that keeps its shell available offline; browsers offer
  "Install", and Account menu > App explains it (on iPhone and iPad: Share >
  Add to Home Screen). Nothing to configure and no migration. The worker never
  stores API answers. A reverse proxy must pass `/sw.js` and
  `/manifest.webmanifest` through unchanged (no caching of `/sw.js`, no
  rewriting); the app's own `Cache-Control: no-cache` already says so.
- **Polls and built-in slash commands (migration 0048).** `/poll` and a poll
  button in the composer let members ask a question with 2 to 10 answers
  (single or multiple choice, open for 1 hour to 1 week, optionally anonymous).
  Voting needs *View channels* and *Add reactions*, posting *Send messages*;
  nothing to configure and no new setting. The server's built-in commands share
  the slash menu with the bots' ([bots](bots.md#built-in-commands-and-polls));
  bots can now give a text option `choices`. New surface, all additive: the
  `poll` object on messages, the `poll_update` WebSocket event, the routes under
  `/api/channels/{id}/polls` and `.../messages/{id}/poll`, and `200` as an
  answer of running a built-in command. A reverse proxy needs no change. The
  data export gains `poll_votes.json`.
- **Rollback means restoring the pre-upgrade backup.** Migrations are
  forward-only and the 0.9.x code has not been verified against the 1.0 schema
  and data (the bot token column, the dropped index), so do not simply start
  the old image on the migrated database. Stop the app, restore the backup
  taken before the update with `scripts/restore.sh` and start the old image
  (see "Rollback" above). Anything written after the update is lost with the
  restore. CI drills this path for the supported source versions: the old
  release refuses to start on the migrated data and a restore into the new
  build succeeds.
- **Messages use Discord's Markdown.** The stored text does not change and
  neither does any API: bots, webhooks, exports and search see the same text.
  Only how the web app draws it does: `__underline__`, `# `/`## `/`### `
  headings, `-# ` small text, `-`/`*`/`1.` lists with nesting, `>>> ` quotes to
  the end of the message, `[text](https://…)` links (opening one whose text does
  not name its destination asks first), `<https://…>` without a link card, `\`
  escapes, and syntax highlighting with a copy button in fenced code blocks.
  Two old habits render differently, as on Discord: `>quote` needs a space
  after the `>`, and `__text__` is underlined instead of left as typed. The
  composer has a formatting cheat sheet next to the emoji button. Desktop
  clients render with the web bundle they load, so they follow this too.

### 0.8.x → 0.9.0

- **Three migrations run on first start: 0035, 0036 and 0037.** 0035 adds
  notification levels per channel and category (with timed mutes), 0036 adds
  role mentions (`message_mentions.kind`, `message_role_mentions`) and 0037 adds
  the community onboarding settings (welcome text, rules, start channel) and
  each member's accepted rules version. Take the usual backup first.
- **Nobody loses notifications.** 0035 rewrites every stored channel level
  `all` to `inherit`, so a category setting now reaches those channels; with no
  category setting `inherit` still means `all`. Explicit `mentions` and `mute`
  levels are kept as they were.
- **Rules enforcement is off until an Owner or Administrator writes rules.**
  Empty rules text enforces nothing, so the upgrade itself never locks anyone
  out. Writing rules for the first time (or publishing a new version) puts
  every member who has not accepted them in a pending state.
- **What pending members lose.** Once rules exist, a member who has not
  accepted the current version cannot send messages, react, upload or join a
  Talk (`RULES_NOT_ACCEPTED`, and `voice_kicked` with reason
  `rules_not_accepted` for someone already in a Talk). They keep read access
  and can accept through the web app. The Owner and bots are exempt. See
  [SECURITY.md](../SECURITY.md) for the model.
- **Rollback means restoring the pre-upgrade backup.** The 0.8.16 code has not
  been verified against `inherit` notification rows or the new tables, so do
  not simply start the old image on the migrated database; restore the backup
  taken before the upgrade (see "Rollback" above).

### 0.8.x (up to 0.8.16)

- **Upload limits are now editable by the Owner.** `MAX_UPLOAD_SIZE_MB`
  (default `50`, unchanged) stays the hard ceiling and the initial limit, so
  nothing changes until the Owner acts. In the System tab the Owner can lower
  the global limit; in the role editor a role can set its own limit (empty
  inherits). A member gets the highest limit among their roles, otherwise the
  global one, never above the ceiling; the Owner always gets the ceiling.
  Migration 0025 adds the `instance_settings` table and `roles.max_upload_mb`.
- **To allow bigger files, raise `MAX_UPLOAD_SIZE_MB` and your reverse proxy.**
  The proxy must accept at least that body size plus a little for multipart
  framing: nginx `client_max_body_size` (for example `client_max_body_size
  140m;` for 128 MB), Caddy `request_body { max_size 140MB }`. The default
  of nginx is 1 MB, so large uploads fail with `413` there first. Then raise
  the global limit in the System tab (it does not follow the env value upward
  once the Owner has set one).

### 0.7.x → 0.8.0

- **The compose file publishes the app port on loopback only**
  (`127.0.0.1:${PORT:-8080}`, before: all interfaces). Set `PUBLISH_ADDR=0.0.0.0`
  (or a LAN address) in `.env` to reach the app directly from the network.
  A reverse proxy on the same host keeps working.
- **`TRUSTED_PROXY_CIDRS` is empty (loopback only) in compose** (before:
  also `172.16.0.0/12`). Requests through Docker's published port, or from a
  proxy container, arrive from the Docker bridge, so the whole range could
  forge `X-Forwarded-For`. **If you run a reverse proxy on another host or in
  a container, set `TRUSTED_PROXY_CIDRS` to that proxy's address**, e.g.
  `192.168.0.20` or the container's IP (a proxy on the compose host reaches
  the app from the bridge gateway: `docker network inspect <project>_default`).
  Until you do, X-Forwarded-For is ignored: nothing breaks, but all clients
  share the proxy's address for rate limits and login lockout, and the log
  says `ignoring X-Forwarded-For from a peer that is not in TRUSTED_PROXY_CIDRS`.
  A deployment that still trusts a broad private range logs a startup warning.
  Setups:
  - Caddy or nginx on the compose host: set `PUBLISH_ADDR` unchanged
    (loopback); the proxy reaches `127.0.0.1:8080` through Docker, so the app
    sees the bridge gateway, not loopback. Set `TRUSTED_PROXY_CIDRS` to that
    gateway (`docker network inspect <project>_default`, field `Gateway`).
  - A proxy container on the same Docker network: set it to the container's
    IP, or the network's subnet if the proxy's IP can change (the subnet holds
    only your own containers).
  - A proxy on another host: set `PUBLISH_ADDR` to a LAN address (or
    `0.0.0.0` behind a firewall) and `TRUSTED_PROXY_CIDRS` to the proxy's
    address.
  An existing deployment with its own compose file that already sets
  `TRUSTED_PROXY_CIDRS` (or its own default) is unaffected by this change.
- **Owner password: 14 characters minimum** when the administrator is
  created (`ADMIN_INITIAL_PASSWORD`) or changes their password (members keep
  10). An existing administrator is not affected until they change it, and a
  too-short `ADMIN_INITIAL_PASSWORD` is only logged as ignored once an
  administrator exists.
- **Login lockout is stricter.** After 100 failed attempts on one account
  across addresses, only browsers that signed in to it before (a signed
  known-device cookie, set again at the next login) are checked as usual; all
  other devices together share about one password check per minute for the
  account. The cap lasts at most 15 minutes; `mnema-talk unlock` clears it (see
  [operations.md](operations.md#the-owner-cannot-sign-in-too-many-requests)).
- **Signing out revokes the session** (migration `0021_revoked_sessions`).
  Sessions issued before the upgrade stay valid until they expire.
- **`APP_ENV=development` only listens on loopback.** `BIND_ADDR` defaults to
  `127.0.0.1` there and any other address is refused at startup. Production,
  staging and test are unchanged (`0.0.0.0`).

- **Two new migrations**: `0021_revoked_sessions` and `0022_search_trigram`
  (a `pg_trgm` index for search). Both run at startup; the trigram index can
  take a moment on a large message table. **Do not roll back below 0.7.20
  once roles or private channels exist**, and the app now refuses to start on a
  database that a newer version migrated (restore a backup instead).
- **Media keys rotate every 30 minutes** and members silent for 20 minutes are
  removed from the call's key group and re-admitted when they are active
  again. Calls between 0.7.25 and 0.8 tabs interoperate, so a running call
  survives the update; reload tabs when the "new version available" notice
  appears to get the new key lifecycle.
- **Screen shares carry quality layers**: the source plus smaller ones, and each
  viewer picks one or leaves **Auto**. Nothing to configure; sharers need a
  current browser.
- **Release images are signed** with cosign (keyless); see
  [Verifying a release image](operations.md#verifying-a-release-image). Pin the
  digest in `MNEMA_IMAGE` if you want the verified bytes to stay put.
- **Desktop client**: it runs a signed web bundle with a security floor that
  rises with releases that carry a `Security-Floor: raise` commit; see
  [desktop-trusted-client.md](desktop-trusted-client.md). The server needs no
  configuration for it.

### 0.4.x → 0.5.0

- **PostgreSQL 17 → 18, one manual step.** The database moves to a new
  volume (`postgres18-data`); the PostgreSQL 17 volume (`postgres-data`)
  stays untouched for rollback. Take the complete database/media/config backup
  in step 1 with 0.4.x still running. Fetch every script listed in step 3,
  including `backup-common.sh` and `upgrade-postgres.sh`. Instead of the
  ordinary startup in step 5, run:

  ```sh
  # steps 1–4 completed: backup saved, new compose/scripts/image selected
  docker compose pull
  ./scripts/upgrade-postgres.sh
  ```

  The script stops both the app and the existing PostgreSQL service before
  copying the old volume. It mounts the original read-only, makes a cold copy
  in a disposable volume, and starts the throwaway PostgreSQL 17 without a
  network on that copy. It dumps the database to `BACKUP_DIR`, imports it into
  PostgreSQL 18, checks the imported tables, and then starts the stack. The
  original PostgreSQL 17 volume receives no temporary-server writes; media
  is not changed by the upgrade.

  Normal startup beside an old PostgreSQL 17 cluster requires the persistent
  completion marker `/var/lib/postgresql/.mnema-pg17-upgrade-complete`, with
  contents `mnema-postgres17-upgrade-v1`. The script publishes it atomically
  only after import and verification succeed. Merely initializing an empty
  PostgreSQL 18 cluster, or completing part of an import, does not authorize
  startup. `MNEMA_PG_UPGRADE=1` is a temporary import bypass; do not save it in
  `.env` or leave it exported. If import fails, keep the app stopped and use
  the rollback above or the complete-backup recovery below. The upgrade's
  standalone SQL dump is not a complete backup.

  Keep the old volume until application checks and recovery verification
  succeed. The script prints its removal command. A local development database
  (`make dev`) upgrades the same way; `docker compose down -v` discards all
  database and media volumes and is suitable only for intentionally disposable
  development data.
- **Migrations** `0013_readable_numbers.sql` (link numbers for channels and
  messages) and `0014_channel_user_limit.sql`. The new migration runner rejects
  a schema newer than its embedded migrations. Released 0.4.x binaries lack
  this guard: roll back using the saved 0.4.x compose file and original
  PostgreSQL 17 volume, or a complete pre-update backup, before starting them.
- **Production needs `ADMIN_INITIAL_PASSWORD` on a fresh install** (no
  effect once the admin exists). The generated password is no longer logged.
- **Voice: one shared UDP port.** `WEBRTC_UDP_MUX_PORT` now defaults to
  `WEBRTC_UDP_PORT_MIN`; no Docker or router change. An `.env` still saying
  `WEBRTC_UDP_MUX_PORT=0` keeps the old per-peer mode (about 25 people in
  calls at once) and logs a warning: empty or remove the line.
- **Per-channel member limit** replaces any idea of a global one: voice
  channels have an optional `user_limit` (default: none).
- **Compose:** memory and pids limits, log rotation, digest-pinned images,
  `POSTGRES_PASSWORD` and `S3_SECRET_KEY` required at interpolation. New
  optional variables `APP_MEM_LIMIT` (1g), `APP_TMP_SIZE` (64m),
  `POSTGRES_MEM_LIMIT` (1g), `SEAWEEDFS_MEM_LIMIT` (1g). Uploads stream to S3
  and no longer pass through `/tmp`.

### Recover PostgreSQL 18 from a complete backup after a failed upgrade

Use this path when the new PostgreSQL 18 volume is initialized but its import
failed, and you have a complete canonical backup from before the update. It
also restores media from that same backup. Run in the existing deployment
with the new compose file and all tagged scripts; the stopped app and existing
SeaweedFS containers must still exist. Keep the original PostgreSQL 17 volume
and the backup. Do not replace or delete volumes during this recovery.

Verify the backup before live changes. `--verify` checks the canonical manifest,
completion marker, checksums, archive safety and an isolated SQL import. An
upgrade-only SQL dump or an incomplete backup does not qualify; do not use
`--allow-legacy` for this recipe. The verification proves structural recovery;
login, media retrieval and voice/device checks still follow afterward.

```sh
(
  set -e
  MNEMA_RECOVERY_BACKUP="/path/to/backups/<timestamp>"
  ./scripts/restore.sh "$MNEMA_RECOVERY_BACKUP" --verify

  # Existing app stays stopped throughout database and media restoration.
  docker compose stop app
  MNEMA_PG_UPGRADE=1 docker compose up -d --wait --no-deps postgres
  ./scripts/restore.sh "$MNEMA_RECOVERY_BACKUP" --yes

  # Reached only after SQL AND media restoration have succeeded.
  docker compose exec -T postgres sh -c 'umask 077; printf "%s\n" mnema-postgres17-upgrade-v1 > /var/lib/postgresql/.mnema-pg17-upgrade-complete.tmp && mv /var/lib/postgresql/.mnema-pg17-upgrade-complete.tmp /var/lib/postgresql/.mnema-pg17-upgrade-complete'

  # Clear the temporary bypass even if inherited from the shell or .env.
  MNEMA_PG_UPGRADE= docker compose up -d --wait --no-deps postgres
  MNEMA_PG_UPGRADE= docker compose up -d --wait
  curl -fsS http://127.0.0.1:8080/api/health
)
```

`restore.sh --yes` records that the app was already stopped, so it does not
resume it. If restore or marker publication fails, `set -e` stops the sequence;
keep the app stopped and resolve the failure before proceeding. The marker
lives in the PostgreSQL 18 volume and survives container recreation. Remove any
`MNEMA_PG_UPGRADE=1` setting from `.env` and the invoking shell before later
normal operations; the empty assignments above apply only to those commands.
After health succeeds, test login, an uploaded object and voice on the restored
release. The newer app applies its embedded migrations when it finally starts.

### 0.3.x → 0.4.0

Operator-relevant changes (0.3.1, 0.3.2, 0.3.3 and 0.4.0):

- **New compose file.** The app's image is now `${MNEMA_IMAGE:-mnema-talk:local}`
  instead of a fixed `mnema-talk:local`, the app carries the updater labels,
  and there is a new optional `updater` service (profile `autoupdate`) with
  an internal network `mnema-updater`. Fetch the compose file of the tag
  (step 3); an old file keeps working but cannot run released images.
- **Released images** on GHCR for `linux/amd64` and `linux/arm64`
  (`:X.Y.Z`, `:X.Y`, `:latest`). Set `MNEMA_IMAGE` to use one instead of
  building on the server. If the package is private, `docker login ghcr.io`
  with a `read:packages` token first.
- **New variables** (all optional, defaults are safe):
  - `UPDATE_CHECK_ENABLED` (default `true`): the server asks
    `api.github.com` every 30 minutes for the latest release; set `false` for
    no outbound request.
  - `MNEMA_IMAGE` (compose default `mnema-talk:local`): the image the app
    runs.
  - `UPDATER_TOKEN` (default empty = off) and `UPDATER_URL` (default
    `http://mnema-updater:8080`): "Update now" through the sidecar. The token
    needs at least 32 characters (`openssl rand -hex 32`); a short or
    placeholder token stops startup. Read
    [SECURITY.md](../SECURITY.md#self-update-sidecar) before enabling it.
- **Version reporting.** The binary and the web app carry their version;
  `/api/health` returns it, and browsers on an older build show a reload
  banner (never an automatic reload).
- **Voice channels have a text chat** (panel under the Talk stage, deep link
  `/v/<id>/chat`). Messages, uploads, threads and read state for voice
  channels use the existing tables; no migration.
- **Stream audio behaviour change.** A shared screen's sound now travels on
  its own line instead of being mixed into the sharer's microphone. Viewers
  control it with the stage's volume slider (0–100 %, default 50 %,
  remembered per sharer); the member menu's volume and mute affect only the
  voice. Every new share starts at 1080p / 30 FPS; the sharer changes quality
  in the stream's gear menu. Make sure everyone reloads after the update (the
  reload banner asks them to): a tab still running the old web app sends its
  screen sound mixed into the microphone, so viewers can't control it
  separately.
- **Migrations:** none since 0.1.16 (latest is `0012_message_count.sql`), so
  rolling back to 0.3.x needs no database restore unless the changelog of
  the release you install says otherwise.

### 0.1.16 → 0.3.0

No configuration, compose or schema changes. 0.3.0 brings the sidebar
management (drag and drop, `Alt+↑/↓`, context menus, duplicate channel) and
the Talk stage (cameras on the stage, viewers of a share, grid, full screen,
picture-in-picture). Update the image or rebuild; nothing else to do.

### 0.1.15 or older → 0.1.16

This release tightened configuration and storage security. Check these
before updating, or the app (or SeaweedFS) will refuse to start:

- **Stricter configuration validation.** In `production`/`staging` every
  public example value from `.env.example` is refused: `JWT_SECRET`,
  `S3_ACCESS_KEY`/`S3_SECRET_KEY`, the database password inside
  `DATABASE_URL` (i.e. `POSTGRES_PASSWORD`), `METRICS_TOKEN`,
  `WEBRTC_TURN_SECRET`, and `ADMIN_INITIAL_PASSWORD` in any environment. Also
  enforced: booleans must be `true`/`false`/`1`/`0`, `LOG_LEVEL` one of
  `debug|info|warn|error` (or empty), `SESSION_EXPIRY_HOURS` 1–8760, `PORT`
  1–65535, `APP_ENV` one of `production|staging|development|test`, and every
  `TRUSTED_PROXY_CIDRS` entry a valid CIDR or IP. `PUBLIC_URL` and CORS
  origins are normalised (no trailing slash or default port).
- **`TRUSTED_PROXY_CIDRS` default is loopback only** (before: loopback plus
  all private ranges, `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`,
  `fc00::/7`). The compose file adds
  `172.16.0.0/12` (Docker's default bridge range). A reverse proxy on another
  LAN host or in a Docker network with a custom subnet must be listed
  explicitly, otherwise all clients share the proxy's IP for rate limits and
  login lockout.
- **SeaweedFS requires S3 credentials.** The `seaweedfs` service now writes
  an S3 identity from `S3_ACCESS_KEY`/`S3_SECRET_KEY` and the gateway accepts
  only that pair (before, it allowed anonymous access on the Docker
  network). Both must be set, use only letters, digits and `._~+/=-`, and the
  secret needs at least 8 characters; otherwise the container exits. Use the
  compose file of the release.
- **Migrations** `0011_username_ci_unique.sql` (usernames unique regardless
  of case) and `0012_message_count.sql`. 0011 fails, and startup stops, if
  two accounts differ only in case: rename one of them in the database and
  start again. From this release on each applied migration's checksum is
  recorded, and startup stops if an applied migration file was changed.
- **Backups.** `scripts/backup.sh` refuses a `BACKUP_DIR` inside a git
  checkout (the backup holds `.env`) unless `BACKUP_ALLOW_IN_REPO=1`, and
  stops SeaweedFS briefly for a consistent media copy (`SEAWEED_LIVE=1` for
  the old behaviour).
