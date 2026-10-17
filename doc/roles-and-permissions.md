# Roles and permissions

Mnema Talk has one community, one **Owner** and any number of **roles** below
the Owner. This page describes the model, where it is enforced and how to
extend it. The code is in [`internal/perm`](../internal/perm/) (model and
rules), [`internal/roles`](../internal/roles/handler.go) (HTTP) and the
enforcement points listed below.

## The Owner

The Owner is the one account with `users.role = 'admin'`: the account that
`ADMIN_USERNAME` (default `Herzog`) seeds on the first start. The database value
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
*hoist* (list its members separately) and *mentionable* (reserved for role
mentions; stored, not used yet).

- **`@everyone`** is implicit: every member has it, it sits at position 0 and
  cannot be renamed, moved or deleted. Only its permissions can be edited. A
  fresh install and every upgrade seed it with exactly what members could
  already do (see *Defaults*), so nothing changes until the Owner edits roles.
- A member's permissions are `@everyone` plus the union of their roles. The
  **position** orders roles: a higher number outranks a lower one.
- **Administrator** grants every permission and skips channel overwrites. The
  hierarchy still applies to it, and the Owner-only powers stay with the Owner.

### Permissions

| Permission | What it allows | Overwritable per channel |
|---|---|---|
| Administrator | Everything below, ignores overwrites | no |
| View channels | See a channel or Talk and read its history | yes |
| Send messages | Post, reply, edit own messages | yes |
| Attach files | Upload files with a message | yes |
| Add reactions | Add a reaction (taking back your own always works) | yes |
| Manage messages | Delete other members' messages | yes |
| Mention everyone | `@all` and `@here` notify people | yes |
| Connect | Join a Talk | yes |
| Speak | Send audio in a Talk | yes |
| Video | Send camera and screen share in a Talk | yes |
| Disconnect members | Remove members from a Talk | yes |
| Manage channels | Create, edit, order, delete channels and categories; edit their overwrites | no |
| Manage roles | Create, edit, order, delete and assign roles below your own highest role | no |
| Manage invites | Create, list, revoke invite codes | no |
| Manage webhooks | Reserved for the webhooks feature | no |
| Manage members | Disable and re-enable accounts, end their sessions, edit their status line | no |

The bit values are stored in the database and sent over the API; they never
change meaning (`perm.Catalog` and its test freeze them).

### Defaults

`@everyone` holds *View channels, Send messages, Attach files, Add reactions,
Mention everyone, Connect, Speak, Video*. Before roles existed every member
could do exactly this, and only the admin could do the rest.

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
  in that Talk; a Talk you may not moderate answers like no Talk at all.
- Reordering only moves roles that are below your highest role, to places
  below it.
- Moderating a member (disable, enable, end sessions, edit the status line,
  remove from a Talk) needs the permission **and** a higher role than the
  target. The Owner can never be targeted.

## Where it is enforced

Everything is decided on the server; the interface only hides what a member
cannot do.

| Surface | Rule |
|---|---|
| `GET /api/channels` | Only viewable channels; a category shows if it is viewable or holds a viewable channel |
| Messages, threads, reactions, search, by-number, read state, notifications | *View channels*; posting *Send messages*; reacting *Add reactions*; deleting others' *Manage messages* |
| Uploads and `GET /api/media/{id}` | *Send messages* + *Attach files* to upload; *View channels* of the message's channel to download (404 otherwise) |
| Mentions | `@all`/`@here` need *Mention everyone*; nobody who cannot view the channel is notified |
| `/api/admin/*` | Per route: *Manage channels*, *Manage roles*, *Manage invites*, *Manage members*, *Disconnect members*; system, storage and passwords are Owner-only |
| WebSocket | Channel events (messages, reactions, typing, who is in a Talk, media state) only reach viewers; the connect snapshot lists only viewable Talks |
| Talks | *View channels* + *Connect* to join; the SFU drops audio without *Speak* and camera/screen without *Video*, also while a call is running |
| Native session API | Uses the same handlers and hub; the opaque ciphertext store checks view/send when given the permission service |

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
- `GET /api/roles`: all roles (visible to every member; they colour names).
- `POST /api/admin/roles`, `PATCH|DELETE /api/admin/roles/{id}`,
  `PUT /api/admin/roles/order`, `PUT /api/admin/users/{id}/roles`.
- `GET|PUT /api/admin/channels/{id}/overwrites`,
  `GET|PUT /api/admin/categories/{id}/overwrites`.

The OpenAPI document ([`api/openapi.json`](../api/openapi.json)) has the details.

## Adding a permission

1. Append a bit to `internal/perm/perm.go` (never reorder), add it to
   `Catalog` and the frozen table in `snapshot_test.go`.
2. Enforce it server-side where it applies and test the enforcement point.
3. Add its name and description to `web/src/i18n` (`en.json`, `de.json`).
4. Decide whether `@everyone` should hold it; changing `DefaultEveryone`
   needs a migration for existing installs, because the default is data.
