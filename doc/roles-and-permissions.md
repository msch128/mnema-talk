# Roles and permissions

Mnema Talk has one community, one **Owner** and any number of **roles** below
the Owner. This page describes the model, where it is enforced and how to
extend it. The code is in [`internal/perm`](../internal/perm/) (model and
rules), [`internal/roles`](../internal/roles/handler.go) (HTTP) and the
enforcement points listed below.

## The Owner

The Owner is the one account with `users.role = 'admin'`: the account that
`ADMIN_USERNAME` (default `admin`) seeds on the first start. The database value
and the `role` field of the API stay `admin` for compatibility; the interface
calls it Owner.

- The Owner has **every permission, always**. No channel overwrite, role or
  setting restricts them, and they see every channel and Talk.
- **Nobody** can edit, restrict, demote, disable, kick, rename or overwrite the
  Owner, or change their roles, whatever they hold. Not even *Administrator*.
- Some powers are the Owner's alone and no permission grants them: the System
  tab (version, updates, self-update), the storage dashboard (media list,
  pruning, orphan cleanup), and setting or resetting **another account's
  password**.
- Ownership is not transferable through the interface. It stays a
  configuration and database matter, as before.

## Roles

A role has a name, a colour, a position, a set of permission bits and the flags
*hoist* (list its members separately) and *mentionable* (anyone may write
`@Role` to notify its members).

A role mention (`@Role`, also with spaces, case-insensitive) notifies the
members of the role who can view the channel when the role is *mentionable*,
or when the author holds *Mention @all and @here* (the Owner and Administrators
always do). Otherwise it stays plain text. A user named like the role wins:
`@name` then mentions the user, not the role. Bots are only notified when
named directly, never through a role, and `@everyone` is not a role mention
(use `@all`). Edits resolve the mentions again. `GET /api/mentions` is the
caller's inbox: mentions and replies to them in channels they can view.

- **`@everyone`** is implicit: every member has it, it sits at position 0 and
  cannot be renamed, moved or deleted. Only its permissions can be edited. A
  fresh install and every upgrade seed it with exactly what members could
  already do (see *Defaults*), so nothing changes until the Owner edits roles.
- A member's permissions are `@everyone` plus the union of their roles. The
  **position** orders roles: a higher number outranks a lower one.
- **Administrator** grants every permission and skips channel overwrites. The
  hierarchy still applies to it, and the Owner-only powers stay with the Owner.

### The managed Bot role

One more role is built in: **Bot** (`managed: true`), created by migration
0032 at the bottom of the hierarchy, just above `@everyone`, and listed
separately in the member list (hoisted) so bots read as a group. Every bot
holds it from its creation, and nobody else can.

| | |
|---|---|
| Held by | every bot, automatically; given to existing bots by the migration |
| Remove from a bot | not possible: `PUT /api/admin/bots/{id}/roles` keeps it, whether the list names it or not |
| Give to a person | refused (`PUT /api/admin/users/{id}/roles`), and the database refuses it too |
| Delete | refused (API and database) |
| Rename | refused; sending the same name is fine |
| Colour, hoist, mentionable, position | editable like any role by someone above it |
| Upload limit | none: bots upload under their own rules |
| Permissions | editable, but only through the central **bot invariant** (below) |
| Default permissions | none: a bot can do nothing until granted something |

Because the role affects every bot, the invariant covers it even before the
first bot exists: only the Owner can add *Administrator* or an admin-area
permission to it (or allow one in a channel for it), nobody can give it
permissions they lack themselves, and creating a bot is checked against what
the Bot role and its overwrites give it, so a manager cannot create a bot that
starts with more than they hold. New roles are placed just above the Bot role
while it is at the bottom, so whoever manages bots outranks it. The role editor
shows it with a lock icon and an explanation; the roles of a person never list
it, and a bot's role list shows it ticked and fixed. `GET /api/roles` marks it
with `managed`. Reordering lists it like any role, and a list without it (older
clients) moves it to the bottom.

### Each bot's own role

Like Discord's integration role, every bot also has a **managed role of its
own** (`managed: true`, `bot_user_id` set), created with the bot by migration
0033 (existing bots got an empty one) and held by that bot only. It is named
after the bot, the name follows a rename of the bot, and it is deleted with the
bot.

| | |
|---|---|
| Held by | exactly one bot, automatically; the database refuses any other holder |
| Remove from the bot | not possible, like the Bot role |
| Give to a person or another bot | refused (API and database) |
| Delete | refused while the bot exists (API and database); goes with the bot |
| Rename by hand | refused; the bot's name decides |
| Colour, permissions | editable like any role's by someone above it, permissions only through the central **bot invariant** |
| Hoist, mentionable | off; bots are grouped by the shared Bot role |
| Position | in the managed block at the bottom, next to the Bot role (same rank), so everyone who manages bots outranks it. It is not part of the order people arrange: `PUT /api/admin/roles/order` ignores it, and the role editor lists it apart under *Bot roles* |
| Upload limit | none, like the Bot role |
| Default permissions | none. The migration does **not** move a bot's existing grants into it, so no bot's effective permissions changed; grants live where they were (the shared Bot role, custom roles, overwrites) and can be moved by hand |

Use the shared Bot role for what every bot should have, the bot's own role for
what only this bot should have (a role can also carry channel overwrites, so
the own role is a convenient place for "this bot may post in #alerts").
`GET /api/roles` marks the role with `managed` and `bot_user_id`.

### Permissions

| Permission | What it allows | Overwritable per channel |
|---|---|---|
| Administrator | Everything below, ignores overwrites | no |
| View channels | See a channel or Talk and read its history | yes |
| Send messages | Post, reply, edit own messages | yes |
| Attach files | Upload files with a message | yes |
| Add reactions | Add a reaction (taking back your own always works); vote in a poll | yes |
| Manage messages | Delete other members' messages (with a reason, or a member's latest ones in bulk), pin messages, exempt from slow mode | yes |
| Mention @all and @here | `@all` and `@here` notify people | yes |
| Connect | Join a Talk (not needed to read or write the Talk's chat, that follows *View channels* and *Send messages*) | yes |
| Speak | Send audio in a Talk | yes |
| Video | Send camera and screen share in a Talk | yes |
| Disconnect members | Remove members from a Talk | yes |
| Manage channels | Create, edit, order, delete channels and categories; edit their overwrites | no |
| Manage roles | Create, edit, order, delete and assign roles below your own highest role | no |
| Manage invites | Create, list, revoke invite codes; also needed for a ban to revoke the banned member's invite | no |
| Manage webhooks | Reserved, unused: webhooks are managed by the Owner and Administrators (see [webhooks](webhooks.md)) | no |
| Manage members | Disable and re-enable accounts, end their sessions, edit their status line | no |
| Manage bots | Create bots, give them roles and tokens, set their intents (see [bots](bots.md)). Only the Owner can grant it | no |
| View audit log | Read the audit log ([moderation](moderation.md)) | no |
| Timeout members | Silence members below your highest role for a while ([moderation](moderation.md)) | no |
| Ban members | Ban and unban members below your highest role, list the bans | no |
| Manage emoji | Upload, rename and delete the custom emoji of the community (bit `1 << 20`) | no |
| Send direct messages | Start direct messages and write in them; reading one you are in never needs it. On for @everyone (migration 0050 adds it to existing databases); a timeout takes it, an administrator can withhold it from new members or onboarding roles (bit `1 << 22`) | no |
| Manage reports | Read and close member reports of messages, with the copy and context they kept (bit `1 << 21`, [moderation](moderation.md#blocking-and-reports)). Off for @everyone | no |
| Kick members | Sign members below your highest role out everywhere and drop them from their Talk, with no lasting sanction (bit `1 << 23`, [moderation](moderation.md#kicks)). Off for @everyone and every seeded role; Administrators and the Owner have it implicitly | no |
| Manage stage | Give and take talk power in a Stage and answer raised hands; holders can always speak there (bit `1 << 24`, [Talks](talks.md#stages)). Off for @everyone and every seeded role; Administrators and the Owner have it implicitly. It is channel-scoped, so it can be allowed for one Stage only | yes |
| Manage events | Create, edit and cancel community events, and upload or remove their cover images (bit `1 << 25`, [events](events.md)). Granted to roles holding *Timeout members* (seeded Moderator and Admin) by migration 0070; Administrators and the Owner have it implicitly | no |

The bit values are stored in the database and sent over the API; they never
change meaning (`perm.Catalog` and its test freeze them).

### Defaults

Member is `@everyone`. There is no User or Member role. The Owner is not a
role either: it is the one account with `users.role = 'admin'`.

| Who | What it is | Permissions |
|---|---|---|
| Member | `@everyone` (position 0, every member has it) | *View channels, Send messages, Attach files, Add reactions, Mention @all and @here, Connect, Speak, Video, Send direct messages* (`perm.DefaultEveryone`) |
| Moderator | Seeded role, hoisted, not managed, not mentionable | *Manage messages, Disconnect members, Timeout members, View audit log, Manage events* |
| Admin | Seeded role, hoisted, not managed, not mentionable, above Moderator when both are new | Moderator's permissions, plus *Manage channels, Manage roles, Manage invites, Manage members, Ban members* |
| Owner | The account `users.role = 'admin'`, not a role | Every permission, plus the Owner-only powers |

Neither seeded role has *Administrator*, *Manage bots* or *Manage webhooks*.
*Administrator* grants every permission and ignores channel overwrites, so it
is not how Admin is modelled.

Migration 0034 inserts the two roles on a fresh install and on an upgrade.
Nobody is assigned, including the Owner, so the upgrade does not grant those
permissions. `@everyone` is left as it was. A role already named Moderator or
Admin is skipped: its permissions, position and members stay, and roles that
already exist are not reordered. A missing name is inserted at
`max(position) + 1`, Moderator first and then Admin. They are ordinary roles,
so the Owner can rename or delete them.

Before roles existed every member could do exactly what `@everyone` holds,
and only the admin account could do the rest.

### Roles members choose themselves

A role can be opened to everyone: in the role editor, an admin with *Manage
roles* turns on **Members can assign this role to themselves** (off by default)
and may add a one-line description. Members then see it under *Settings >
Roles* (the section appears only while such roles exist) and switch it on or off
for their own account, without any admin involved.

The flag is only for harmless roles, and the server enforces it every time:

- never `@everyone`, a managed role or a bot's role;
- never a role with *Administrator*, any permission that opens the admin area
  (the same set the bot rules use), or a moderation power over others
  (*Manage messages*, *Manage Stages*, *Manage events*). Turning the flag on for
  such a role, or giving permissions like that to a flagged role, is refused with
  a clear sentence. If a flagged role somehow carries such a bit anyway, it is no
  longer listed and cannot be taken.
- the Owner has no roles to choose; bots cannot use it.

Taking or dropping a role is audited as `role.self_assign` / `role.self_remove`
(the member is the actor; asking for what one already has is not a change and
leaves no entry), rebuilds permissions like any other role change and broadcasts
`roles_changed`. A member's own changes are limited to 30 a minute. A Discord
import never sets the flag.

## Channel and category overwrites

A category or channel can allow or deny channel permissions for a **role**
(`@everyone` is a role) or a single **member**. Private channels are simply
channels where `@everyone` is denied *View channels* and some role is allowed.

Resolution, in this order:

1. The Owner has everything. Stop.
2. Base = `@everyone` | all permissions of the member's roles.
3. *Administrator* in the base: everything. Stop.
4. Apply the **category's** overwrites, then the **channel's**, each as:
   `@everyone` (deny, then allow); then all of the member's roles combined
   (their denies, then their allows); then the member's own (deny, then
   allow). The channel therefore wins over its category.
5. Without *View channels* nothing channel-scoped remains.

Deleting a category folds its overwrites into its channels first, so a private
category never turns its channels public.

## Hierarchy

- A member can only edit, delete or assign roles **strictly below** their
  highest role, and only change a member whose highest role is below theirs.
- Nobody can grant a permission they do not hold themselves (editing a role,
  creating one, or allowing something in an overwrite).
- A role can only be given by someone who holds all of its permissions, and
  nobody changes their own roles.
- Overwrites: every entry that is added, changed or removed needs authority
  over its subject (a role below yours or `@everyone`; a member below you) and
  the permissions it allows or denies. Changing or deleting a category, or
  moving a channel between categories that have overwrites, needs sight of
  every channel it affects (moving: the Administrator permission).
- Removing a member from a Talk needs *Disconnect members* and *View channels*
  in that Talk, from a server-wide role grant or from an overwrite on the Talk
  or its category (a deny withholds it there); a Talk you may not moderate
  answers like no Talk at all. The route is
  `POST /api/channels/{id}/voice/{userID}/disconnect`.
- Reordering only moves roles that are below your highest role, to places
  below it.
- Moderating a member (disable, enable, end sessions, edit the status line,
  remove from a Talk, time out, ban) needs the permission **and** a higher role
  than the target. The Owner can never be targeted, and nobody moderates
  themselves (timeout and ban).

## Where it is enforced

Everything is decided on the server; the interface only hides what a member
cannot do.

| Surface | Rule |
|---|---|
| `GET /api/channels` | Only viewable channels; a category shows if it is viewable or holds a viewable channel |
| Messages, threads, reactions, search, by-number, read state, notifications | *View channels*; posting *Send messages*; reacting *Add reactions*; deleting others' *Manage messages* |
| Polls (`/poll`, `POST /api/channels/{id}/polls`) | Posting *Send messages*; voting and reading your own votes *View channels* + *Add reactions* (so a timed-out member cannot vote); ending early the author, or *Manage messages* |
| Slash commands | Listing *View channels*; running *Send messages*. `/msg` also needs *Send direct messages*, `/timeout`, `/ban` and `/kick` the matching permission and a higher role than the target (each command's `Available` check, `internal/slashcmd`); a bot's commands also need the bot to view and send there |
| Direct messages | Membership of the conversation, not a channel permission: only its members read it, and nobody else, not even the Owner or a role with *Administrator*, can browse it. Starting and writing need *Send direct messages* and the other members' privacy setting; a block stops writing in a conversation of two, while in a group it leaves unread counts, mentions and last activity untouched for the blocker. Moderators see only what a reporter shares (*Manage reports*) |
| Blocks | Private to the blocker. The server drops the blocked member's messages from the blocker's lists, mentions, inbox and push; in any conversation (group direct messages included), unread counts, mention counts and last activity leave out messages by members the reader blocked (the mentions inbox itself excludes direct messages) |
| Saved messages and forwarding | A saved row is a reference and is checked on every read. Forwarding needs to see the original and to write in the target; the original is shown to each reader only as far as that reader may see it |
| Thread follow | Following needs *View channels*; a follower who loses access gets nothing |
| Pins | *Manage messages*; the pin records who pinned and when |
| Uploads and `GET /api/media/{id}` | *Send messages* + *Attach files* to upload; *View channels* of the message's channel to download (404 otherwise) |
| Mentions | `@all`/`@here` need *Mention @all and @here*; nobody who cannot view the channel is notified |
| `/api/admin/*` | Per route: *Manage channels*, *Manage roles*, *Manage invites*, *Manage members*, *Disconnect members*; system, storage and passwords are Owner-only |
| WebSocket | Channel events (messages, reactions, typing, who is in a Talk, media state) only reach viewers; the connect snapshot lists only viewable Talks |
| Talks | *View channels* + *Connect* to join (reading and writing a Talk's chat without joining needs only *View channels* and *Send messages*); the SFU drops audio without *Speak* and camera/screen without *Video*, also while a call is running |
| `GET /api/admin/roles/{id}/view-as` | *Manage roles*. A preview for a made-up member holding only @everyone and that role, computed by the same resolver (overwrites included) and intersected with what the caller can view: a channel or category the caller cannot see is never in it |
| Community events (`/api/events`) | Listing, viewing, RSVPing: public if free-text location; if located in a Talk channel, requires *View channels* on that Talk. Creating, editing, cancelling, uploading cover: *Manage events* |

## View the server as a role

In *Admin* > *Roles*, **View server as <role>** opens a read-only preview of the
sidebar for a made-up member who holds only @everyone and that role. It shows
the categories and channels that member can see, marks every Talk *can join* or
*can't join*, and lists, for the channel you select, which channel permissions
they hold there. The server computes it with the real permission resolver, so
category and channel overwrites count. A role with *Administrator* sees (and
may do) everything the caller sees, ignoring overwrites.

It is a preview of a role, never of a person: nobody is impersonated, no
session or setting changes, and it cannot show more than you can see yourself
(the result is intersected with your own view, so a hidden channel does not
appear even when the role could see it). It needs *Manage roles*. *Exit
preview* returns to the role editor.

## Community rules

The Owner and Administrators can write rules, a welcome text and a community
name (admin console, *Welcome & rules*; `GET|PUT /api/admin/onboarding`, audited).
The community name is what the server calls itself everywhere: the browser tab,
the name of the installed app (web app manifest), the header of the sidebar,
the sign-in screen, invite links, e-mails (subject, text and sender display name),
authenticator-app entries, the title of a test push and the server's tile and
window title in the desktop clients. While it is empty the host of `PUBLIC_URL`
stands in. It is public: `GET /api/health` and `GET /api/auth/options` carry
`community_name` (clients treat it as optional), and a change reaches open
sessions through the `community_updated` event.

### Community icon

The Owner and Administrators (the same who may change the name) can upload an
icon for the community (admin console, *Welcome & rules*;
`PUT|DELETE /api/admin/community/icon`, audited as `settings.community_icon` and
`settings.community_icon_remove`). The server decodes the picture (PNG, JPEG,
GIF without animation or WebP, at most 5 MB, at least 128x128 pixels; SVG is
refused), crops the centre square and writes it again as two PNGs of 512x512 and
128x128 pixels, so metadata is gone. The files live in object storage under
`community/icon/<version>/`; `instance_settings.community_icon_version` names
the current one (migration `0080`).

The icon is public, on the same footing as the community name, and it stays so
on an invite-only server: `GET /api/community/icon` (optional `?size=128|512`,
default 512) needs no session and answers 404 while none is set. The name next
to it is already readable without signing in (`GET /api/health`,
`GET /api/auth/options`, the sign-in page, link previews), and a community
icon is meant to be seen; it shows nothing about members or content. The
route has its own per-address rate limit. It carries an `ETag`, a
`Last-Modified` and `Cache-Control: public, max-age=300, must-revalidate`; a
request whose `v` parameter equals the current version is `immutable` for a
year. Clients learn the version from the optional `community_icon` field of
`GET /api/health` and `GET /api/auth/options` (left out while there is none)
and from `community_updated` (`community_icon`, empty after removal), and
refetch when it differs from the one they have.

The web app uses it as the tab icon, home-screen icon and manifest icons (the
server writes them into `index.html` and the manifest, so installed apps and
tabs follow), in the sidebar header and on the sign-in screen. The root page and
working invite pages use it as `og:image` in link previews; without an icon the
Mnema icon stays.
While rules exist, a member who has not accepted the current version
(`rules_pending` in `GET /api/permissions`, `GET /api/auth/me` and
`GET /api/onboarding`) loses what a timeout takes: *Send messages*, *Attach
files*, *Add reactions*, *Mention @all and @here*, *Connect*, *Speak* and *Video*.
They keep reading. It is not a permission anyone can grant or deny: it comes
from the same snapshot as a timeout, so REST, the WebSocket hub and the SFU all
agree, and writes answer `403 RULES_NOT_ACCEPTED` (a Talk join answers
`voice_kicked` with reason `rules_not_accepted`). `POST /api/onboarding/accept`
with the current `version` lifts it at once.

- The Owner and bots are exempt. Empty rules enforce nothing, so existing
  installs and clearing the rules never lock anyone out.
- The first rules written start at version 1 and every existing member has to
  accept them. Later edits keep acceptances unless the Owner ticks *require
  re-acceptance*, which raises the version.
- A member the server has not seen yet counts as not having accepted (the check
  fails closed).
- `GET /api/invites/{code}` is public and rate limited per IP; it answers only
  `{valid, community_name, expires_at?}`, and an expired, revoked, used-up or
  unknown code looks the same.

## Upload size

`MAX_UPLOAD_SIZE_MB` (default 50) is the operator's hard ceiling. On top of it
the Owner sets, in the interface only:

- a **global limit** (System tab, `PUT /api/admin/upload-limit`), at most the
  ceiling; unset means the ceiling;
- an optional **per-role limit** (role editor, `max_upload_mb`; empty = inherit).
  `@everyone` can have one too.

A member's effective limit is the highest limit among `@everyone` and their
roles that set one, otherwise the global limit, never above the ceiling. The
Owner always gets the ceiling. Webhooks have no role and use the global limit.
Avatars keep their own 5 MB cap. Only the Owner may set these values (a role
manager gets `403` for `max_upload_mb`); they are a resource limit, not a
permission. The effective limit is `max_upload_mb` in `GET /api/permissions`
and updates with `permissions_changed`; the web app refuses larger files
before sending them, the server enforces it per request (`413`).

## Live changes

Every change to roles, assignments, overwrites, channels or categories
rebuilds the in-memory permission state (it is a few small maps; a TTL of one
minute is only a backstop for edits made directly in the database) and then:

- sends `permissions_changed` to everyone affected, so clients refetch
  `GET /api/permissions` and `GET /api/channels`;
- broadcasts `roles_changed` after role changes, so clients refetch roles and
  members;
- removes members from a Talk they may no longer join (`voice_kicked` with
  `reason: "missing_permission"`), and narrows the media they may still send.

## API

- `GET /api/permissions`: the caller's server-wide permissions and, per
  viewable channel, their permissions there, plus the catalog of bits.
- `GET|PUT /api/admin/upload-limit` (Owner): the global upload limit and the ceiling.
- `GET|PUT /api/admin/onboarding` (Owner, Administrators): community name, welcome text, rules, start channel.
- `PUT|DELETE /api/admin/community/icon` (Owner, Administrators): the community icon. `GET /api/community/icon` (public): the icon, 404 while none is set.
- `GET /api/onboarding`, `POST /api/onboarding/accept`, `PUT /api/onboarding/tips`: a member's view of them and their acceptance and dismissed tips.
- `GET /api/invites/{code}` (public): the invite preview.
- `GET /api/roles`: all roles (visible to every member; they colour names).
- `GET /api/roles/self-assignable`: the roles members may choose;
  `PUT|DELETE /api/me/roles/{id}` take and drop one for the caller.
- `POST /api/admin/roles`, `PATCH|DELETE /api/admin/roles/{id}`,
  `PUT /api/admin/roles/order`, `PUT /api/admin/users/{id}/roles`.
- `GET|PUT /api/admin/channels/{id}/overwrites`,
  `GET|PUT /api/admin/categories/{id}/overwrites`.

The OpenAPI document ([`api/openapi.json`](../api/openapi.json)) has the details.

## Downgrades

> **Do not roll back below this release once roles or private channels exist.**
> Migration 0017 adds the tables that restrict access. A binary that does not
> know them (0.7.x before roles, or older) would ignore them and show every
> private channel to everyone. This release and later ones refuse to start on a
> database migrated by a newer version, but binaries from before that guard
> existed cannot. To go back, restore a backup taken before the update.

## Adding a permission

1. Append a bit to `internal/perm/perm.go` (never reorder), add it to
   `Catalog` and the frozen table in `snapshot_test.go`.
2. Enforce it server-side where it applies and test the enforcement point.
3. Add its name and description to `web/src/i18n` (`en.json`, `de.json`).
4. Decide whether `@everyone` should hold it; changing `DefaultEveryone`
   needs a migration for existing installs, because the default is data.
