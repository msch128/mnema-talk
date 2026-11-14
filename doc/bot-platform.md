# The bot platform: design note

This note records what the bot platform of Mnema Talk lacked compared with
what a Discord bot developer expects, what was built for it, and what was
left out on purpose, with the reasons. The reference for bot developers is
[bots.md](bots.md); this page is the why.

## What existed

Bot accounts with roles, overwrites and the Bot role, `Authorization: Bot`
tokens with issuer binding, the REST API under `/api/bot/v1`, a gateway with
Discord's opcodes, intents, resume and presence, slash commands with
`INTERACTION_CREATE`, callback types 4 and 5, follow-ups and ephemeral
replies, and the management API behind the admin area's *Bots* tab.

## What was missing

| Expected by a Discord bot developer | State before |
|---|---|
| Adding someone else's bot from its README in a few clicks | Create by hand, then roles, overwrites, commands and a token, each in its own screen |
| Buttons, link buttons, select menus | `components` accepted and ignored |
| Modals (forms) | none |
| Component and modal interactions (types 3 and 5), deferred update and update message (callback types 6 and 7), modal (9) | only commands (type 2) and callbacks 4 and 5 |
| Editing or deleting the original response (`@original`) | none |
| Editing a bot message's components | edits took content only |
| An official SDK with examples | two hand-written snippets in bots.md |

## What is built

### 1. Adding a bot from a manifest

Mnema Talk is one community per server, so there is no global application
directory and no OAuth authorize screen on a central site. The shape that
fits is a **manifest**: a small JSON document the bot's author publishes (in
the README, or as a link), which describes what the bot is and what it asks
for. The Owner or a member with *Manage bots* opens it in **Admin area > Bots
> Add from manifest** (pasted, or arriving through an add link), sees exactly
what it asks for, decides where it may work, confirms, and gets the token
once.

- **The add link** is `https://<your server>/admin/bots/add#manifest=<base64url JSON>`.
  The manifest travels in the URL fragment, which browsers never send to the
  server: it does not end up in access logs or proxies, and opening the link
  does nothing on its own. A bot's README gives the manifest and a one-line
  way to build the link for one's own server (the SDK has `addBotLink()`).
- **No manifest URL is fetched.** The server does not download manifests or
  avatars from addresses a member typed: that would be a new outbound request
  path from the admin area (SSRF surface, a way to probe the server's
  network, a tracking pixel for the bot's author) for a document that fits in
  a link anyway. The avatar comes inside the manifest as a small data URL
  (PNG, JPEG or WebP, at most 256 KB) and goes through the ordinary avatar
  upload, with its checks.
- **What a manifest may ask for**: a name, a description, a homepage, an
  avatar, gateway intents, channel permissions, and slash commands. The
  permissions are limited to what a bot uses in channels (View channels,
  Send messages, Attach files, Add reactions, Manage messages, Mention
  everyone). A manifest cannot ask for Administrator or any admin-area
  permission: those stay a separate, deliberate step of the Owner (bots.md,
  *Every way a bot can gain permissions*), so an install link can never be
  a way to talk someone into handing out power.
- **Where it may work** is the installer's choice, not the manifest's: the
  confirm step lists the channels the installer can see, none ticked, and an
  **Access** choice. With **Selected channels** (the default) the requested
  permissions become allow overwrites of the bot's **own** managed role in
  exactly the ticked channels; the bot gets no server-wide permission. With
  **All channels** they are held server-wide by the own role (see "Give a bot
  access to every channel" below).
- **Who may grant what** does not change. The install runs in one
  transaction under the permission lock, through the bot invariant: nobody
  but the Owner can give a bot anything elevated, or anything the installer
  does not hold in that channel. The installer may untick any permission or
  intent the manifest asks for, never add one. Writing overwrites here is new
  for a member who holds *Manage bots* but not *Manage channels*; it is safe
  because the overwrites belong to the new bot's own role, which nobody else
  holds and which cannot be given to anyone, so they change nothing for any
  member, and the bot invariant caps them at what the installer may do in
  each channel. They do override a deny on the shared Bot role (or another
  role the bot holds) in a ticked channel, because a member's role allows
  apply after its role denies; only a deny set on the bot itself beats them.
  That was already possible by giving the bot a role with channel
  overwrites; bots.md says it, and the confirm screen says it next to the
  channel list. The install is in the audit log (`bot.installed`).
- **Defaults on the confirm screen**: Mention everyone, Manage messages and
  anything else that pings or moderates start unticked; the manifest's name,
  description and homepage are shown quoted, as the bot's own words, never
  like text of the app; a command that has a built-in's name is marked as
  the bot's, listed next to the built-in.
- **The result** is an ordinary bot: everything can be changed in the Bots
  tab afterwards. The token is shown once, as with every token.

### Give a bot access to every channel

Admins asked for the simple choice other platforms have: a bot that sees and
answers everywhere, without a detour through the role editor. The add dialog
and the bot's settings have one **Access** choice, **Selected channels** or
**All channels**.

- **How it works.** *All channels* writes the kept permissions onto the bot's
  own managed role as ordinary server-wide permissions, instead of one
  overwrite per channel, so channels created later are covered too. Switching
  back removes them and writes overwrites for the chosen channels.
  `PUT /api/admin/bots/{id}/access` does the same for an existing bot;
  `POST /api/admin/bots/install` takes `access` and stays compatible (absent
  means `channels`).
- **Private channels.** The decision: they stay private. Bots do not inherit
  `@everyone`, so a plain server-wide grant would reach every private channel
  (that is why the first version offered per-channel grants only). A bot given
  access to all channels follows one more rule: where `@everyone` alone does
  not leave *View channels* in place (on the channel, its category or
  server-wide), the bot does not see the channel either, until the admin lists
  it explicitly, which gives the bot's own role an allow overwrite there. The
  marker is a column, `bots.access_all` (migration 0107), not something derived
  from the role's bits, so no existing bot changes: a bot whose own role was
  given server-wide permissions in the role editor keeps seeing private
  channels. The rule only ever takes View away; no other part of `@everyone`
  applies to bots, so such a bot may write in a read-only channel of
  `@everyone`.
- **Who may grant what.** Unchanged. Only the manifest's channel permissions
  can be chosen, so the choice can never carry Administrator or an admin-area
  bit, and the existing bot invariant decides the rest: a non-Owner cannot give
  a bot a permission they lack in any channel it reaches, and cannot change a
  bot that reaches a channel they cannot view.

### 2. Message components and modals

Buttons (primary, secondary, success, danger, link), string selects, user,
role and channel selects in action rows on bot messages, interaction
responses and follow-ups; modals with short and paragraph text inputs as a
response to a command or a component. Discord's shapes, numbers and limits,
so that code written for Discord's components ports with few changes. Clicks
and submits reach the bot as `INTERACTION_CREATE` types 3 and 5; the bot
answers with callback 4, 5, 6 (deferred update), 7 (update the message) or 9
(open a modal), and can edit or delete the original response.

Rules that keep it inside the security model:

- **Only bot accounts set components**, only on their own messages, and only
  through the bot API. Webhooks and members cannot.
- **Clicking needs View channels** in the channel (as reading the message
  does) and is refused while the member is timed out, like reacting. Read-only
  channels keep working, which is where role pickers and polls usually live.
- **The server validates every click** against the stored message: the
  `custom_id` must be on the message and not disabled, a select's values must
  be among its options (a string select) or be members, roles and channels
  the member may see (the other selects). The bot never receives a value the
  web app could not have offered.
- **A channel the bot cannot view never reaches it**: a channel select value
  the bot cannot view is refused, so a click cannot tell a bot about a
  channel it is not allowed to know.
- **The bot must still be able to view the channel** when a member clicks,
  and must be enabled and connected (`409` otherwise, as for commands).
- Clicks and modal submits share a limit of 30 a minute per member, on top of
  the 20 command runs.

### 3. Interactions and restarts

Interactions (the id and token a bot answers with) stay in the server's
memory, valid for 15 minutes, as before. That holds with components: a
button on a week-old message works, because its `custom_id` is stored with
the message in the database, and every click creates a **fresh** interaction
with a new token. A restart therefore only loses the interactions that were
in flight at that moment (the bot's answer gets `404`; the member clicks
again), and the forms that were open (submitting answers `404`, the web app
says the form expired). Persisting tokens would mean storing answerable
secrets for a quarter of an hour to save a click in the rare case of a
restart; not worth it. What is not stored is said in bots.md: ephemeral
notes, and so their buttons, live for 15 minutes in memory only.

### 4. SDKs

Two official SDKs live in this repository, versioned with the server:

- **TypeScript**, `sdk/typescript` (package `mnema-talk-sdk`): Node 24+, ESM,
  no runtime dependencies (Node's own `fetch` and `WebSocket`), typed events,
  a REST client with rate-limit handling, a gateway client with heartbeats,
  resume and backoff, command registration, interaction helpers (`reply`,
  `deferReply`, `editReply`, `update`, `showModal`, ...) and builders for
  components and modals.
- **Go**, `sdk/go`, its own module `github.com/msch128/mnema-talk/sdk/go`,
  with the same feature set; its one dependency is `github.com/gorilla/websocket`,
  which the server uses as well.

Each has three examples (an echo bot, a slash command bot, a buttons and
modal bot), a README and contract tests that run against a real server
started by the repository's integration test helpers in CI, so an SDK
cannot drift from the server.

Installing: nothing is on npm or behind a module proxy tag yet.

- The TypeScript SDK is attached to every release as a tarball with its
  SHA-256 next to it
  (`npm install https://github.com/msch128/mnema-talk/releases/download/vX.Y.Z/mnema-talk-sdk-X.Y.Z.tgz`).
  The `sdk-package` job of `release.yml` builds it from the tagged commit in
  a sealed container, like the web bundle, and stamps the release's version
  into the package as it packs. The version is not kept in the repository's
  `package.json` through release-please, because the release commit may
  touch only `version.txt`, `CHANGELOG.md` and the manifest, and that guard
  of the release stays as it is. npm cannot install a package from a
  subdirectory of a git repository; pnpm and yarn can (see the SDK's README).
- The Go module is fetched from the repository at a commit or a branch
  (`go get github.com/msch128/mnema-talk/sdk/go@<commit>`, or `@main`, which
  resolves to a pseudo-version). Version tags of the form `sdk/go/vX.Y.Z`
  are left for later: pushing a second tag from the release workflow is a
  new kind of write under the tag ruleset that cannot be rehearsed outside a
  real release.

### 5. Compatibility

Everything is additive to `/api/bot/v1`: new fields (`components`), new
interaction and callback types, new routes (`@original`), new member routes
and new WebSocket events (`interaction_modal`; `interaction_response` gains
`id`, `interaction_id` and `components`). Error shapes, codes and rate limits
stay Discord's. No breaking change (doc/api-stability.md).

### 6. Command permissions and interaction member permissions

Discord compatibility for command access control: commands accept
`default_member_permissions` (a decimal string bitfield of required Mnema
permission bits, `null` = everyone, `"0"` = admin-only). The server filters
the composer picker and enforces permissions at invocation time (refusing with
`403` and the missing permission name). The interaction payload includes
`member.permissions` with the invoking member's effective channel permissions at
invocation time as a decimal string bitfield (Owner: all bits). Bits use
Mnema's permission catalogue ([Roles and permissions](roles-and-permissions.md)),
not Discord's.

## What is not built, and why

- **A public bot directory or a central application registry.** One
  community per server, no federation: there is nobody to run it, and it
  would make every server depend on a third party.
- **OAuth2 for bots (authorize, scopes, redirect).** The installer is an
  admin on their own server; a manifest plus the confirm step gives the same
  "see what it asks for, then grant" without a redirect dance or client
  secrets.
- **Fetching manifests by URL**: see above.
- **Mentionable selects (type 7)**: a mixed user and role picker adds a
  combined search for little gain; user and role selects cover it.
- **Select menus inside modals, file uploads in modals, Discord's newer
  layout components (sections, containers, media galleries)**: text inputs
  are what forms need; the rest can follow on demand.
- **Autocomplete for command options (interaction type 4)**: worth doing,
  but it needs a low-latency round trip from the composer to the bot and
  back on every keystroke; a separate step.
- **Persisting interactions**: see above.
- **Voice for bots, direct messages to bots**: out of scope as before
  (bots.md).
- **AI features inside Mnema Talk**: none. Mnemo, the AI helper bot, is a
  separate project that uses the SDK and is run by whoever wants it.
