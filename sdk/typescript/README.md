# Mnema Talk SDK for TypeScript

The official SDK for [Mnema Talk bots](../../doc/bots.md): the REST API with
its rate limits, the gateway with heartbeats, resume and reconnects, slash
commands, interactions, message components, modals and the add link.

- Node 24 or newer, ES modules, **no runtime dependencies** (Node's `fetch`,
  `WebSocket`, `FormData` and `Blob`).
- Types for every wire object; builders return plain JSON in Discord's shape,
  and plain objects work wherever builders do.
- The version follows the server's release version.

## Install

From a release (the tarball is a release asset):

```sh
npm install https://github.com/msch128/mnema-talk/releases/download/vX.Y.Z/mnema-talk-sdk-X.Y.Z.tgz
```

With pnpm, straight from the repository (the `prepare` script builds `dist/`;
pnpm 10 asks you to allow that build, for example with `pnpm approve-builds`):

```sh
pnpm add "github:msch128/mnema-talk#path:/sdk/typescript"
pnpm add "github:msch128/mnema-talk#vX.Y.Z&path:/sdk/typescript"   # a release tag
```

From a clone:

```sh
git clone https://github.com/msch128/mnema-talk.git
(cd mnema-talk/sdk/typescript && npm install)   # installs TypeScript and builds dist/
npm install ./mnema-talk/sdk/typescript
```

## Quick start

Create the bot and its token first ([Getting started](../../doc/bots.md#getting-started)).

```ts
import { Client, Intents, actionRow, button, ButtonStyle } from 'mnema-talk-sdk'

const bot = new Client({
  baseUrl: 'https://talk.example.org',
  token: process.env.MNEMA_TOKEN!,
  intents: Intents.GuildMessages,
})

bot.on('messageCreate', async (m) => {
  if (m.content === '!ping') {
    await bot.rest.createMessage(m.channel_id, {
      content: 'pong',
      components: [actionRow(button({ customId: 'again', label: 'Again', style: ButtonStyle.Primary }))],
    })
  }
})
bot.on('interactionCreate', async (i) => {
  if (i.isButton() && i.customId === 'again') await i.update({ content: 'pong again' })
})
bot.on('error', (err) => console.error(err))

await bot.connect()
```

`connect()` checks the token (`GET /users/@me`), asks `GET /gateway` for the
gateway address (or derives `wss://…/api/bot/v1/gateway` from `baseUrl`),
identifies and resolves with `READY`; `client.user` is the bot from it.
`client.close()` closes the gateway for good and leaves no timers behind.

### Examples

[`examples/`](examples) has three bots; each reads `MNEMA_URL` and
`MNEMA_TOKEN`:

```sh
npm install && npm run build
MNEMA_URL=https://talk.example.org MNEMA_TOKEN=mnb_... node examples/echo.ts
```

| File | Shows |
|---|---|
| [`echo.ts`](examples/echo.ts) | `MESSAGE_CREATE`, replying to `!echo <text>` |
| [`slash.ts`](examples/slash.ts) | registering `/ping` and `/roll` with options and choices, `reply`, `deferReply` + `editReply`, ephemeral answers |
| [`buttons.ts`](examples/buttons.ts) | buttons, a link button, a select, `update()`, a modal with `showModal()` and its submit |
| [`manifest.json`](examples/manifest.json) | the manifest of the slash command bot |

## Client

```ts
new Client({ baseUrl, token, intents, presence?, logger?, rest?, gateway? })
```

- `intents`: the sum of `Intents.Guilds` (1), `Intents.GuildMembers` (2),
  `Intents.GuildMessages` (512), `Intents.GuildMessageReactions` (1024), or a
  list of them. Interactions need none. Asking for more than the admin
  allowed closes with `4014`.
- `presence`: `{ status: 'online' | 'idle' | 'dnd' | 'invisible', activity?: { name, type } }`,
  sent in IDENTIFY. `client.setPresence(...)` changes it later (op 3, at most
  5 a minute, extra ones dropped by the server). Activity types are
  `ActivityType.Playing` (0), `Listening` (2), `Watching` (3), `Custom` (4),
  `Competing` (5).
- `logger`: anything with `debug`, `info`, `warn`, `error` (for example `console`).
- `rest`: `timeoutMs`, `maxRetries`, `localRateLimits`, `onRateLimit`, `fetch`.
- `gateway`: `url` (a fixed address), `reconnect`, `resumeWindowMs`, `webSocketFactory`.

### Events

`client.on(event, listener)`, `client.once(...)`, `client.off(...)`; the
listener types follow the event. A listener that throws or rejects is reported
on `error`.

| Event | Arguments | Dispatch | Intent |
|---|---|---|---|
| `ready` | `Ready` (`user`, `session_id`, `intents`, ...) | `READY` | |
| `resumed` | | `RESUMED` | |
| `messageCreate` | `Message` | `MESSAGE_CREATE` (also the bot's own messages) | `GuildMessages` |
| `messageUpdate` | `Message` | `MESSAGE_UPDATE` | `GuildMessages` |
| `messageDelete` | `{ id, channel_id, parent_id }` | `MESSAGE_DELETE` | `GuildMessages` |
| `reactionsUpdate` | `{ message_id, channel_id, reactions }` | `MESSAGE_REACTIONS_UPDATE` | `GuildMessageReactions` |
| `channelsUpdate` | | `CHANNELS_UPDATE`: fetch `rest.listChannels()` again | `Guilds` |
| `memberAdd` | `{ id, username, display_name, avatar_url }` | `GUILD_MEMBER_ADD` | `GuildMembers` |
| `interactionCreate` | `Interaction` | `INTERACTION_CREATE` | none |
| `raw` | `name, data, seq` | every dispatch | |
| `close` | `{ code, reason, reconnect }` | the gateway connection closed | |
| `error` | `Error` | fatal close codes (`GatewayError`), failing listeners, protocol errors | |
| `debug` | `string` | connection steps | |

### REST

`client.rest` (or `new RestClient({ baseUrl, token })` on its own):

| Method | Route |
|---|---|
| `getMe()` | `GET /users/@me` |
| `getGateway()` | `GET /gateway` |
| `listChannels()` | `GET /channels` |
| `listMessages(channelId, { limit, before, after, around })` | `GET /channels/{id}/messages` |
| `getMessage(channelId, messageId)` | `GET /channels/{id}/messages/{id}` |
| `createMessage(channelId, { content, embeds, components, allowed_mentions, message_reference, parent_id })` | `POST /channels/{id}/messages` |
| `editMessage(channelId, messageId, { content?, components? })` | `PATCH /channels/{id}/messages/{id}` |
| `deleteMessage(channelId, messageId)` | `DELETE /channels/{id}/messages/{id}` |
| `addReaction(channelId, messageId, emoji)`, `removeReaction(...)` | `PUT`, `DELETE .../reactions/{emoji}/@me` |
| `uploadFile(channelId, { file: Blob \| Buffer, name, content?, contentType?, allowed_mentions?, parent_id?, reply_to_id? })` | `POST /channels/{id}/upload` |
| `listMembers()`, `getMember(userId)` | `GET /members`, `GET /members/{id}` |
| `listCommands()`, `setCommands(list)`, `createCommand(cmd)`, `deleteCommand(id)` | `/commands` |
| `request(method, path, { query, body, form, signal })` | any route under `/api/bot/v1` |

`createMessage` and `editMessage` also take a string (the content). An error
answer throws a `RestError` with `status`, `code` (Discord's JSON code, for
example `10003` unknown channel, `50013` missing permissions), `message` (the
server's) and `body`.

### Slash commands

```ts
await client.commands.set([            // PUT /commands: replaces them all
  { name: 'roll', description: 'Rolls a die',
    options: [{ name: 'sides', description: 'How many sides', type: OptionType.Integer, required: true }] },
])
await client.commands.create(cmd)      // POST: adds or updates by name
await client.commands.list()
await client.commands.delete(id)
```

Names are 1 to 32 lowercase letters, digits, `-` or `_`; at most 50 commands,
10 options each, required options first, string options may list up to 25
`choices`. The SDK checks this before sending.

## Interactions

`interactionCreate` gives an `Interaction`:

| Field | |
|---|---|
| `id`, `token`, `type` (2 command, 3 component, 5 modal submit), `channelId`, `user` | always |
| `commandName`, `commandId`, `options` (`Map<string, string \| number \| boolean>`) | commands |
| `customId`, `componentType`, `values` (`string[]`), `resolved`, `message` | components |
| `customId`, `fields` (`Map<string, string>`), `message` (when opened from a component) | modal submits |
| `raw` | the payload as it arrived |

`isCommand()`, `isComponent()`, `isButton()`, `isSelect()`, `isModalSubmit()`
tell them apart. Answer within 15 minutes:

| Method | Callback | For |
|---|---|---|
| `reply({ content, embeds, components, allowed_mentions?, ephemeral? })` | 4 | all |
| `deferReply({ ephemeral? })` | 5, then `editReply` | all |
| `update({ content?, embeds?, components?, allowed_mentions? })` | 7 | components (and modal submits from one) |
| `deferUpdate()` | 6 | components |
| `showModal(modal(...))` | 9 | commands and components |
| `editReply({ content?, embeds?, components?, allowed_mentions? })`, `deleteReply()` | `PATCH`, `DELETE .../messages/@original` | after the first answer |
| `followUp({ content, embeds, components, allowed_mentions?, ephemeral? })` | `POST .../followups` (up to 5 responses in total) | after the first answer |

`ephemeral: true` sets flags `64`: only the member who caused the
interaction sees it, and it is never stored. It belongs to `reply`,
`deferReply` and `followUp`; `editReply` and `update` take no `ephemeral`
(the first response decides, and the server refuses flags on an edit).
`allowed_mentions` works as on `createMessage`: without it an answer notifies
the members it names, never `@all`, `@here` or a role. `i.user` is always
set: the server sends the member with every interaction. `client.user.handle`
(from READY) is the name members mention the bot by (`@bot-...`);
`client.user.username` is its display name.

## Components and modals

```ts
import { actionRow, button, linkButton, stringSelect, userSelect, roleSelect, channelSelect,
  modal, textInput, ButtonStyle, TextInputStyle } from 'mnema-talk-sdk'

const rows = [
  actionRow(
    button({ customId: 'deploy:v2.4', label: 'Deploy', style: ButtonStyle.Success }),
    button({ customId: 'cancel', label: 'Cancel', style: ButtonStyle.Secondary }),
    linkButton({ url: 'https://talk.example.org/changes', label: 'Changelog' }),
  ),
  actionRow(stringSelect({ customId: 'env', placeholder: 'Environment',
    options: [{ label: 'Staging', value: 'staging' }, { label: 'Production', value: 'prod' }] })),
]

const form = modal({ customId: 'feedback', title: 'Feedback', components: [
  textInput({ customId: 'subject', label: 'Subject', maxLength: 80 }),
  textInput({ customId: 'body', label: 'What happened?', style: TextInputStyle.Paragraph, required: false }),
] })
```

The builders check the documented limits and throw a `ValidationError` at
once: `custom_id` 1 to 100 characters and unique in a message, 5 buttons or
one select per row, 5 rows, 25 components, button labels up to 80, select
options 1 to 25 (labels and values up to 100, values unique), placeholders
up to 150, `min_values` 0 to 25 and `max_values` 1 to 25; a modal has a title
up to 45 and 1 to 5 text inputs (labels up to 45, lengths 0 to 4000). The
same checks run on plain objects passed to `createMessage`, `editMessage`
and the interaction answers. The full rules are in
[Message components](../../doc/bots.md#message-components) and
[Modals](../../doc/bots.md#modals).

## Rate limits

- **REST.** The server allows 50 requests per second per bot and 5 messages
  per 5 seconds per channel (posts and uploads). The client holds requests
  back before they would be refused (`localRateLimits: false` turns that
  off) and keeps a bucket per route, with the channel id as its major part.
  A `429` pauses its bucket (or, with `global: true`, every request) for
  `retry_after` seconds (the `Retry-After` header when the body has none) and
  sends the request again, up to `maxRetries` (3) times; then it throws a
  `RestError` with `retryAfter` and `global`. `onRateLimit` is told about
  every 429. Slow mode answers 429 the same way. Each attempt times out after
  `timeoutMs` (15 s); pass `signal` to `request` to cancel.
- **Gateway.** Frames from the bot are at most 4096 bytes (a bigger one throws
  `FrameTooLargeError`) and 120 a minute per connection: ordinary frames wait
  in a queue past 115, keeping room for heartbeats.

## Reconnects

- Heartbeats at the interval from HELLO, the first after a random part of it.
  A heartbeat without an ACK by the next one drops the connection (close code
  `4900`, the SDK's own) and reconnects.
- After a drop the SDK opens a new connection and sends RESUME with the
  `session_id` and the last `seq` while it is within the server's 2-minute
  window (`resumeWindowMs`, 110 s by default); the server replays what was
  missed and sends `RESUMED`. On INVALID_SESSION it identifies again on the
  same connection.
- Reconnects back off exponentially with jitter, from 1 s up to 60 s
  (`gateway: { reconnect: { minDelayMs, maxDelayMs } }`); after `4008`
  (rate limited) it waits at least `rateLimitedDelayMs` (30 s). `4000`,
  `4003`, `4005` and `4007` start a new session.
- `4004` (bad, revoked or expired token, disabled or banned bot), `4013` and
  `4014` (intents) are fatal: the SDK stops, `connect()` rejects with a
  `GatewayError` and `error` is emitted.

## Add link

```ts
import { addBotLink, encodeManifest } from 'mnema-talk-sdk'

const link = addBotLink('https://talk.example.org', {
  manifest_version: 1,
  name: 'Dice',
  intents: [],
  permissions: ['view_channel', 'send_messages'],
  commands: [{ name: 'roll', description: 'Rolls a die' }],
})
// https://talk.example.org/admin/bots/add#manifest=<base64url JSON>
```

`encodeManifest(manifest)` gives the base64url part alone. Both check the
manifest (version 1, name 1 to 24 characters, an `https` homepage, a `data:`
avatar, valid commands). See
[Adding a bot from a manifest](../../doc/bots.md#adding-a-bot-from-a-manifest).

## Development

```sh
npm install          # also builds dist/
npm run typecheck    # tsc --noEmit, strict, sources, tests and examples
npm run build        # dist/ with types
npm test             # offline: builders, REST against node:http, the gateway against a local WebSocket server
npm pack --dry-run   # dist/, README.md, LICENSE, package.json
```

Tests run the TypeScript sources directly (Node 24 strips the types).

### Contract tests

[`test/contract/`](test/contract) runs the SDK against a real server. It is
skipped (with the reason shown) unless `MNEMA_CONTRACT_URL` is set:

| Variable | |
|---|---|
| `MNEMA_CONTRACT_URL` | the server, `http://127.0.0.1:<port>` |
| `MNEMA_CONTRACT_TOKEN` | a bot token; the bot can view and send in the channel and may use `GUILDS`, `GUILD_MESSAGES` and `GUILD_MESSAGE_REACTIONS` |
| `MNEMA_CONTRACT_CHANNEL` | the channel id |
| `MNEMA_CONTRACT_MEMBER_COOKIE` | the `Cookie` header of a signed-in member who can view and send in the channel |

```sh
MNEMA_CONTRACT_URL=http://127.0.0.1:8080 MNEMA_CONTRACT_TOKEN=mnb_... \
MNEMA_CONTRACT_CHANNEL=... MNEMA_CONTRACT_MEMBER_COOKIE='...' npm run test:contract
```

They cover `getMe`, READY, a message and its `MESSAGE_CREATE`, a command the
member runs and the SDK answers, a button click the SDK `update()`s (checked
from the member's side), a modal opened on a click and submitted through the
member WebSocket's `interaction_modal`, a burst of messages that meets `429`
and is retried, and `close()`.

## License

[AGPL-3.0](LICENSE), like Mnema Talk.
