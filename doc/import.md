# Importing the structure of a Discord server

Since 1.2 the Owner and members with the *Administrator* permission can copy the
**structure** of a Discord server into Mnema Talk: categories, text channels,
Talks and roles with their permission overwrites. It works from a Discord
**server template** link. The server advertises it as `import.discord` in the
`features` list of `GET /api/health`; a web app hides the interface when the
name is missing.

What an import brings in is what a template contains and nothing more: no
messages, members, emoji, stickers, webhooks or bots.

## How to use it

1. In Discord: *Server Settings*, *Server Template*, create the template and
   copy its link (`discord.new/<code>` or `discord.com/template/<code>`; the bare
   code works too).
2. In Mnema Talk: *Admin*, *Import*. Paste the link and choose *Preview*. Nothing
   is written yet: the preview lists every category, channel and Talk that would
   be created, every role with the permissions it would get, and every Discord
   permission that is dropped.
3. Choose *Review and import* and go through **three confirmations**. Each says
   in plain words what happens:
   1. *This adds X categories, Y channels and Z roles at the end of your server.
      Nothing existing is changed, renamed or removed.*
   2. The permissions each new role gets, and what is dropped.
   3. Type the community name (from *Welcome & rules*) or `IMPORT`.
4. The import runs in **one transaction**: either everything is created or
   nothing is. One audit entry (`import.discord`) records the counts and the
   template code.

Previewing and importing need the *Administrator* permission (or the Owner). The
roles *Manage roles* and *Manage channels* are not enough, because an import
creates both at once. Each administrator may run ten previews or imports a
minute, since a preview makes a request to Discord.

## Strictly additive

An import never changes, renames, reorders or removes anything that exists:

- Categories are appended after the last existing category. Channels without a
  category are appended after the last existing one. Channels that belong to a
  template category go into the new category with the same name, never into an
  existing one.
- Roles are created **below every existing role** (the existing roles move up by
  the number of new roles; their order does not change), so a new role is never
  above the highest role of the administrator who imports. Imported roles never
  hold *Administrator* and never hold a permission the importing administrator
  does not hold. *Manage bots* is never imported.
- Discord's `@everyone` permissions are **not applied** to Mnema's `@everyone`.
  The preview shows them for information only.
- Names are kept. Channel and category names need not be unique here, so they
  are never changed. A role whose name is already taken (or reserved, like
  `everyone` and `here`) gets a number suffix, "Moderator (2)"; role names are
  also cut to 32 characters.
- The plan is computed again when you confirm, from a fresh download. If the
  template or the roles on this server changed since the preview, the import is
  refused with a conflict and you preview again. What you confirmed is exactly
  what is created.

## What becomes what

| Discord | Mnema Talk |
|---|---|
| Category | Category |
| Text channel, announcement channel | Text channel |
| Voice channel | Talk; the member limit is kept (at most 999) |
| Stage channel | Stage if the server has `talks.stage`, otherwise a normal Talk |
| Forum channel | Forum channel if the server has `channels.forum`, otherwise a text channel |
| Channel topic | Channel description (shortened to 255 characters) |
| Slow mode | Slow mode, capped at the largest slow mode this server allows |
| Threads, directories, media channels | Skipped (listed in the preview) |
| Age restriction | Not imported (noted in the preview) |
| Role: name, colour, "show separately", mentionable | The same |
| Role order | Kept relative to each other |
| Permission overwrites of a role | Overwrites of the new role on the new category or channel |
| Overwrite of `@everyone` on a channel | Overwrite of Mnema's `@everyone` **on the new channel only**, so a private channel stays private |
| Overwrites for single members | Ignored (a template has none) |

### Permissions

Discord permission bits are mapped by this table. Everything not in it is
dropped and named in the preview.

| Discord | Mnema Talk |
|---|---|
| `VIEW_CHANNEL` | View channels |
| `SEND_MESSAGES` | Send messages |
| `ATTACH_FILES` | Attach files |
| `ADD_REACTIONS` | Add reactions |
| `MANAGE_MESSAGES` | Manage messages |
| `MENTION_EVERYONE` | Mention @all and @here |
| `CONNECT` | Connect to Talks |
| `SPEAK` | Speak |
| `STREAM` | Share video and screen |
| `MANAGE_CHANNELS` | Manage channels |
| `MANAGE_ROLES` | Manage roles |
| `MANAGE_GUILD_EXPRESSIONS` | Manage emoji |
| `VIEW_AUDIT_LOG` | View audit log |
| `KICK_MEMBERS` | Kick members |
| `BAN_MEMBERS` | Ban members |
| `MODERATE_MEMBERS` | Timeout members |

**Dropped on purpose:**

- `ADMINISTRATOR` is never imported. The preview says so whenever a template role
  had it. Give the role the permissions it needs, or Administrator yourself, in
  *Roles* afterwards.
- `CREATE_INSTANT_INVITE` is dropped: *Manage invites* opens the admin area here,
  which is much more than creating an invite on Discord.
- `MANAGE_GUILD`, `MANAGE_WEBHOOKS` and the other Discord settings have no
  counterpart; webhooks are an admin tool that no role grants.
- `READ_MESSAGE_HISTORY` is covered by *View channels*. Voice moderation
  (`MUTE_MEMBERS`, `DEAFEN_MEMBERS`, `MOVE_MEMBERS`), nicknames, threads, events,
  embeds, external emoji, soundboard, applications and so on do not exist here.
- A permission that is not a channel permission is dropped from overwrites.
- Bits Discord added after this table was written are dropped and shown as
  `UNKNOWN_BIT_<n>`.

## Undo

Every import is recorded together with the exact ids of what it created
(*Admin*, *Import*, *Earlier imports*). **Undo import** removes those objects and
nothing else, in one transaction:

- Objects you already deleted by hand since are skipped.
- A created category that now holds a channel that is not part of the import
  stays, with that channel. It is listed when you undo.
- A created channel that has **messages** by now needs a second, separate
  confirmation. Undoing deletes the channel and its messages and attachments for
  good.
- Members who were given an imported role lose it. Roles close the gap they leave,
  so the order of the others stays.
- An import can be undone once. Undoing is audited (`import.discord_undo`).

## Safety of the download

The server fetches `https://discord.com/api/v10/guilds/templates/<code>` and
nothing else. A pasted link only contributes its template code (letters, digits,
`-` and `_`); the host is fixed. The request uses the same guard as outgoing
webhooks: public addresses only, no proxy from the environment, a 10 second
timeout, **redirects are never followed**, and the answer is read up to 2 MB. A
template that is larger, has more than 600 channels or 300 roles, or is not a
template, is refused. Members only ever see short, fixed error texts, never the
network error. Discord requires no token for this call.

## API

Admin routes below `/api/admin/import/discord` (Administrator only; see
`api/openapi.json`):

| Route | Purpose |
|---|---|
| `POST /preview` | `{template}` returns the plan with a `hash`. Writes nothing and is not audited. |
| `POST /apply` | `{template, plan_hash, confirm_additive, confirm_roles, confirm_text}` imports. All three confirmations are required. |
| `GET /imports` | The recorded imports, newest first. |
| `GET /imports/{id}` | One import and what an undo would do now. |
| `POST /imports/{id}/undo` | `{accept_message_loss}`; `true` is required when created channels have messages. |

Database: migration 0071 adds `discord_imports` and `discord_import_objects`.
The object ids carry no foreign key on purpose, so a manual deletion never blocks
or breaks an undo.
