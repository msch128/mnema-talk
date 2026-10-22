# Webhooks

Mnema Talk has **incoming webhooks**: a URL per channel that scripts, CI
systems and bots post messages to, in Discord's execute format, and
**outgoing webhooks**, which tell your own service what happens (signed JSON
over HTTP). Outgoing webhooks are described from
[Outgoing webhooks](#outgoing-webhooks) on.

The OpenAPI document ([`api/openapi.json`](../api/openapi.json)) has every
route, field and error; this page explains the behaviour and the choices.

## Who may manage webhooks

Webhooks are an **admin tool**, decoupled from roles and permissions: only the
**Owner** and members with **Administrator** can create or manage them, in any
channel, and nothing else opens them (not *Manage webhooks*, which is reserved
and unused, nor *Manage channels* or *Manage roles*). The API lives under
`/api/admin`.

A webhook belongs to its channel, not to a person. The administrator who made
it is kept as a record (`created_by`, and for outgoing ones `url_set_by` and
`url_set_at`, shown in the interface so stale webhooks are easy to spot). That
record grants nothing: the webhook keeps working if its maker loses a role,
is disabled or is deleted, and any administrator can change or delete it.
Regenerating a token or rotating a secret changes only that.

A webhook posts with **its own settings** and nothing else; whoever holds the
URL gets exactly these, so treat the URL like a password. Set them when
creating the webhook or later (`PATCH /api/admin/webhooks/{id}`):

| Setting | Default | Meaning |
|---|---|---|
| `allow_everyone` | off | `@all` and `@here` may notify (the sender must also ask for it with `allowed_mentions.parse: ["everyone"]`) |
| `allow_user_mentions` | on | `@username` notifies members, as Discord's `parse: ["users"]`; off turns every member mention into plain text |
| `allow_attachments` | on | a post may carry files; otherwise it is refused with `400` |
| `allow_embeds` | on | a post may carry embeds; otherwise it is refused with `400` |
| `allow_identity_override` | on | a post may set its own `username`; otherwise the webhook's name is used |

Roles: `@Role Name` notifies the role's members only when the request asks for it, with `allowed_mentions.parse: ["roles"]` (every role the webhook may mention) or `allowed_mentions.roles` (role IDs, UUIDs). Without `allowed_mentions`, or with `parse` leaving `roles` out, the text stays plain. A webhook may mention roles marked mentionable; its `allow_everyone` setting extends that to every custom role (not `@everyone` or the managed Bot roles). This is the webhook's own setting, not any user's rights. Only members who can view the channel are notified, and the per-message mention cap applies: a post whose `allow_everyone` is off notifies at most 50 members in total (named users first, then role members, in a fixed order; the rest stay unnotified, the text and role pills stay). With `allow_everyone` on there is no cap, since `@all` reaches everyone anyway. Members and bots follow the same rule, by their right to mention everyone. At most 100 distinct `@names` are read from one message.
Nobody who cannot see the channel is ever notified, whatever the settings say.
The limits that protect the server stay global: sizes, rate limits, sanitizing
of text and links, the file type checks.

## Create one

```
POST /api/admin/channels/{channelId}/webhooks      {"name": "CI"}
```

The answer carries `url`, for example
`https://talk.example.org/api/webhooks/<id>/<token>`. **The token is shown
once.** Only a SHA-256 hash is stored, so a lost URL cannot be shown again:
`POST /api/admin/webhooks/{id}/token` makes a new one and the old URL stops
working at once. A channel can have 15 webhooks.

Rename: `PATCH /api/admin/webhooks/{id}`. Avatar:
`POST /api/admin/webhooks/{id}/avatar` (multipart, field `avatar`, at most
5 MB). Delete: `DELETE /api/admin/webhooks/{id}`. List:
`GET /api/admin/channels/{channelId}/webhooks` (never contains tokens).

## Post a message

```sh
curl -X POST "$WEBHOOK_URL?wait=true" \
  -H 'Content-Type: application/json' \
  -d '{"content": "build **passed**", "username": "CI",
       "embeds": [{"title": "Run 42", "url": "https://ci.example/run/42",
                   "color": 65280, "fields": [{"name": "tests", "value": "812", "inline": true}]}]}'
```

- The secret in the URL is the only credential. **No cookie or Origin header
  is needed and cookies are ignored**: the route sits outside the session and
  outside the same-origin (CSRF) check. Browsers cannot call it from another
  site (CORS is not enabled for it); it is for servers and scripts.
- `?wait=true` answers `200` with the created message, otherwise `204`.
- `?thread_id=<message id>` posts into the thread of a root message of the
  same channel (Mnema Talk threads hang off a message, so this is the message
  ID, not a channel ID).
- Send JSON, or `multipart/form-data` with the JSON in a `payload_json` part
  and up to 10 files in further parts. Files take the same path as member
  uploads: size limit the global upload limit (webhooks have no role; at most `MAX_UPLOAD_SIZE_MB`), type sniffed and checked against the
  allowlist (a disguised file is refused with `415`). Plain form fields
  `content` and `username` work too when there is no `payload_json`.

### How the message looks

It is authored by the webhook, never by a member: its name (or the `username`
of the call) and avatar, with a **Webhook** badge. Behind the scenes each
webhook has a hidden bot account that cannot sign in and is not listed among
the members. Text is Markdown like any message and sanitized the same way.
Editing a webhook message through the interface is not possible; members with
*Manage messages* can delete it. Deleting the webhook keeps the messages it
posted.

### Discord compatibility

| Discord feature | Mnema Talk |
|---|---|
| `content` | Supported, up to 4000 characters (Discord: 2000) |
| `username` | Supported (1 to 80 characters, no control characters) unless the webhook's `allow_identity_override` is off |
| `avatar_url` | **Ignored.** Mnema Talk never loads pictures from other hosts; set the avatar on the webhook |
| `embeds` | Subset: `title`, `description`, `url`, `color`, `timestamp`, `fields`, `footer.text`, `author.name`/`url`, `image.url`, `thumbnail.url`. Discord's limits apply (10 embeds, 25 fields, 6000 characters in total). Pictures are shown as links, not loaded. `type`, `video`, `provider` and icon URLs are accepted and dropped |
| `allowed_mentions` | `parse` with `users`, `everyone` and `roles`, plus `users` (member IDs) and `roles` (role IDs). Default **differs from Discord**: `@username` notifies, `@all`/`@here` do not unless `parse` contains `everyone` and the webhook's `allow_everyone` setting is on (the settings can only narrow what a sender asks for). `parse` may also contain `roles` and `roles` lists role IDs (like Discord, `parse: ["roles"]` together with a `roles` list is rejected); see Mentions above. Roles are off unless asked for |
| `?wait` | Supported |
| `?thread_id` | Supported, as the ID of a root message |
| `multipart/form-data`, `payload_json`, files | Supported (`files[n]`; any part with a file name counts) |
| `tts`, `flags`, `components`, `poll`, `thread_name`, `attachments` metadata | Accepted and ignored |
| Edit / delete a message through the webhook, `GET` the webhook, `/slack`, `/github` compatible endpoints | Not supported |
| Webhook IDs and tokens | UUID and a 43-character random token (not Discord snowflakes) |

Unknown JSON fields are accepted (Discord clients send many); the management
API, in contrast, rejects unknown fields.

### Errors and limits

Errors of the execute route use Discord's shape, not the `{"error": ...}` of
the rest of the API:

| Status | `code` | When |
|---|---|---|
| 400 | 50006 | nothing to post (no content, embeds or files) |
| 400 | 50035 | invalid body or field, too many files, wrong `Content-Type` |
| 401 | 50027 | wrong token |
| 404 | 10015 | unknown webhook |
| 404 | 10003 | unknown `thread_id` |
| 413 | 40005 | body or file too large |
| 415 | 50035 | file type not allowed |
| 429 | none | rate limited: `{"message", "retry_after", "global": false}` and a `Retry-After` header |

Limits: about **5 requests per 2 seconds per webhook** and **30 per minute per
channel** (Discord's numbers), JSON bodies up to 1 MiB, multipart up to
the global upload limit plus framing. Wrong ids and tokens are counted per client
address (30 a minute) and then answered with `429`, so a token cannot be
guessed by hammering the route (it has 256 random bits anyway).

## Outgoing webhooks

An outgoing webhook sends events of a channel (or of the whole server) to a
URL of yours. Administrators create them under **Webhooks** in a channel's menu, or
through the API:

```
POST /api/admin/channels/{channelId}/outgoing-webhooks
     {"name": "CI", "url": "https://ci.example.org/mnema", "events": ["MESSAGE_CREATE"]}
```

The answer carries the **signing secret, shown once** (`whsec_...`); rotate
it with `POST /api/admin/outgoing-webhooks/{id}/secret`. Up to 10 per channel.
An outgoing webhook belongs to its channel and receives that channel's events
only; there are no server-wide webhooks. Its settings are the events it sends
and whether it is enabled. Deleting the channel deletes it.

### Events

| Event | When | `d` |
|---|---|---|
| `MESSAGE_CREATE` | a message is posted in the channel (messages posted by an incoming webhook are not sent, so two webhooks cannot feed each other) | the message, as in `GET /api/channels/{id}/messages` |
| `MESSAGE_UPDATE` | a message is edited | the message |
| `MESSAGE_DELETE` | a message is deleted | `{id, channel_id, parent_id}` |
| `MESSAGE_REACTIONS_UPDATE` | someone reacts or takes a reaction back | `{message_id, channel_id, reactions}` |
| `VOICE_STATE_UPDATE` | a member joins or leaves a Talk of the channel | `{action: "join"\|"leave", channel_id, user...}` |
| `PING` | the "send test event" button; cannot be subscribed to | `{message}` |

`d` is exactly the object the WebSocket sends for that event, so the
OpenAPI schemas of the messages apply. Typing, presence and similar
short-lived events are not offered.

### The request

`POST` with `Content-Type: application/json`:

```json
{
  "id": "6f1c...",
  "t": "MESSAGE_CREATE",
  "timestamp": "2026-10-17T09:30:00Z",
  "webhook_id": "b0a1...",
  "channel_id": "93de...",
  "d": { "id": "...", "content": "hello", "user_id": "...", "...": "..." }
}
```

`id` is unique per event and **repeated by retries**; use it (or the
`X-Mnema-Delivery` header, the same value) to deduplicate. More headers:

- `X-Mnema-Event`: the event type.
- `User-Agent: MnemaTalk-Webhook/<version>`.
- `X-Mnema-Signature: t=<unix seconds>,v1=<hex>`, where `v1` is the
  HMAC-SHA256 of `"<t>.<raw body>"` with your secret as the key.

Your endpoint answers with any `2xx` within 10 seconds; the body is ignored.

### Verify the signature

Recompute over the **raw** request body, compare in constant time, and refuse
timestamps that are too old (replay protection). The `t` in the signature
header is the time of **this attempt**, set when the request is sent, so the
tolerance holds for retries and for deliveries made after a restart too; the
`timestamp` in the body is when the event happened and can be hours older, so
do not apply the tolerance to it:

```python
import hmac, hashlib, time

def verify(secret: str, header: str, raw_body: bytes, tolerance=300) -> bool:
    parts = dict(p.split("=", 1) for p in header.split(","))
    t, v1 = parts["t"], parts["v1"]
    if abs(time.time() - int(t)) > tolerance:
        return False
    expected = hmac.new(secret.encode(), f"{t}.".encode() + raw_body, hashlib.sha256).hexdigest()
    return hmac.compare_digest(expected, v1)
```

```js
import { createHmac, timingSafeEqual } from 'node:crypto'

export function verify(secret, header, rawBody, toleranceSeconds = 300) {
  const { t, v1 } = Object.fromEntries(header.split(',').map(p => p.split('=')))
  if (Math.abs(Date.now() / 1000 - Number(t)) > toleranceSeconds) return false
  const expected = createHmac('sha256', secret).update(`${t}.`).update(rawBody).digest('hex')
  return expected.length === v1.length && timingSafeEqual(Buffer.from(expected), Buffer.from(v1))
}
```

### Retries, switch-off and the log

- A delivery that fails with a network error, a timeout, `5xx`, `408` or
  `429` is retried after 5 seconds, 30 seconds, 2, 10 and 30 minutes (a
  `Retry-After` of the receiver, up to 10 minutes, is honoured), so an event
  is tried up to 6 times in about 45 minutes. Other `4xx`, redirects and
  refused addresses are not retried.
- **Five events in a row** that failed for good switch the webhook off
  (status `auto_disabled`, visible in the list). Switching it on again
  (`PATCH ... {"enabled": true}`) starts over.
- **Pending deliveries are kept in PostgreSQL** (table `outgoing_queue`), so
  events waiting for a first attempt or for a retry **survive a restart or an
  update** of the server and are sent afterwards. Only the instant between an
  event happening and being written (milliseconds) is held in memory. Bounds:
  at most 500 pending deliveries per webhook and 5000 in total (the oldest are
  dropped first, counted in `mnema_webhook_events_dropped_total`), and a
  delivery not settled within 2 hours (past the last retry) is given up. A
  per-channel share of the in-memory buffer keeps one noisy channel from
  starving the others.
- **At least once, best-effort order.** If the server stops after a request
  went out but before it was recorded, the event is sent again after the
  restart, and retries of one event may arrive after later events of the same
  webhook. Do not rely on order or on exactly-once: **deduplicate on the
  delivery id** (`id` in the body, the same value as the `X-Mnema-Delivery`
  header; it never changes between attempts) and, if you need ordering, sort by
  `timestamp`. Outgoing webhooks are still a notification channel, not a log;
  fetch the messages from the API if you must not miss any.
- On shutdown the server stops starting new attempts, lets those in flight
  finish (up to 5 seconds) and leaves the rest in the database.
- At most 20 delivery attempts a second go out in total, 4 at a time.
- `GET /api/admin/outgoing-webhooks/{id}/deliveries` shows the last 50
  attempts (event, attempt number, HTTP status, a short error code, duration).
  **The receiver's response is never stored or shown**, only the status code,
  because it could come from a service inside your network.
- "Send test event" (`POST .../test`) sends a `PING` once, right away, and
  shows the result. It is limited to 6 a minute per webhook and does not count
  towards the switch-off.

### Who sees what

An outgoing URL receives its channel's events until an administrator changes
or deletes the webhook, whoever set the URL and whatever became of them, as in
Discord. Only administrators can set URLs, and each webhook only gets one
channel's events. Nothing is checked per delivery beyond the webhook still
existing. Rotating the secret, renaming and switching on or off change only
that.

### Target addresses (SSRF protection)

A URL typed by a member makes **this server** send a request, so the server
guards where requests may go:

- Only `http` and `https`, no credentials in the URL, at most 2048 bytes.
- The server resolves the name itself and refuses the **whole host** if any
  answer is not a public address: loopback, private ranges (`10/8`,
  `172.16/12`, `192.168/16`), link-local, carrier-grade NAT (`100.64/10`),
  multicast, unspecified, IPv6 unique-local and link-local, reserved and
  documentation ranges, NAT64/6to4/Teredo, and the cloud metadata addresses
  (`169.254.169.254`, `fd00:ec2::254`, ...). IPv4-mapped IPv6 forms are
  checked as the IPv4 address they carry.
- It then connects to **exactly the address it checked**, not to the name
  again, so a DNS answer that changes between the check and the connection
  (rebinding) is harmless. This also applies to every connection.
- **Redirects are not followed** (a `3xx` is a failed delivery), so a public
  URL cannot bounce the server to an internal one. No proxy from the
  environment is used. Responses are read up to 64 KiB and dropped.
- Failures are reported as short codes (`blocked_address`, `dns_error`,
  `connect_error`, `timeout`, `tls_error`, `http_500`, ...), never as the raw
  network error, so the log cannot be used to map a network.

If a webhook must reach a service on your own network, the operator can list
it in `WEBHOOK_ALLOW_PRIVATE_TARGETS` (addresses and CIDR ranges, comma
separated; empty by default). **This is a risk decision:** every administrator
can then make the server send POST requests with a JSON body
to those addresses. List single hosts rather than whole networks, and prefer
a receiver that checks the signature. The metadata addresses and the reserved
ranges cannot be opened this way.

## Security notes

See the *Webhooks* paragraph in [SECURITY.md](../SECURITY.md). In short: the
URL is a bearer secret, treat it like a password; tokens are stored hashed and
compared in constant time; a webhook cannot sign in, appear as a member, or
notify everyone unless its maker could; embeds are rendered as sanitized text
and links, nothing is fetched from the senders' hosts.
