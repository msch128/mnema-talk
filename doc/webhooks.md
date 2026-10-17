# Webhooks

Mnema Talk has **incoming webhooks**: a URL per channel that scripts, CI
systems and bots post messages to, in Discord's execute format, and
**outgoing webhooks**, which tell your own service what happens (signed JSON
over HTTP). Outgoing webhooks are described from
[Outgoing webhooks](#outgoing-webhooks) on.

The OpenAPI document ([`api/openapi.json`](../api/openapi.json)) has every
route, field and error; this page explains the behaviour and the choices.

## Who may manage webhooks

The Owner always. Everybody else needs the permission **Manage webhooks** and
**View channels** in that channel (a channel you cannot view does not exist for
you, so neither do its webhooks). Because the webhook is an admin-area
feature, the API lives under `/api/admin`.

Creating a webhook also needs **Send messages** in the channel, because the
webhook posts there. A webhook acts with its creator's rights, checked again
at every post: it needs the creator to hold *Send messages* (and *Attach
files* when files are sent) in the channel right now, and the creator's
account to exist and be enabled; otherwise the post is refused with `403`
(code 50013). The Owner's webhooks always pass.

Regenerating a token gives the new URL to the caller, so it needs the same
rights as creating (including *Send messages*), and the caller becomes the
webhook's creator: posts are always checked against whoever holds the URL.

Hierarchy follows [roles and permissions](roles-and-permissions.md):

- A webhook created by the Owner can only be changed by the Owner.
- Otherwise a member may change, regenerate or delete a webhook of someone
  whose highest role is **not above** theirs (peers may look after each
  other's webhooks), and always their own.
- A webhook can mention `@all` and `@here` only if its **creator** holds
  *Mention everyone* in that channel at the moment of the post, so a webhook
  never gives anyone more power than its creator has.

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
  uploads: size limit `MAX_UPLOAD_MB`, type sniffed and checked against the
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
| `username` | Supported (1 to 80 characters, no control characters) |
| `avatar_url` | **Ignored.** Mnema Talk never loads pictures from other hosts; set the avatar on the webhook |
| `embeds` | Subset: `title`, `description`, `url`, `color`, `timestamp`, `fields`, `footer.text`, `author.name`/`url`, `image.url`, `thumbnail.url`. Discord's limits apply (10 embeds, 25 fields, 6000 characters in total). Pictures are shown as links, not loaded. `type`, `video`, `provider` and icon URLs are accepted and dropped |
| `allowed_mentions` | `parse` with `users` and `everyone`, and `users` (member IDs). Default **differs from Discord**: `@username` notifies, `@all`/`@here` do not unless `parse` contains `everyone` and the creator may mention everyone. `roles` is accepted and ignored |
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
`MAX_UPLOAD_MB` plus framing. Wrong ids and tokens are counted per client
address (30 a minute) and then answered with `429`, so a token cannot be
guessed by hammering the route (it has 256 random bits anyway).

## Outgoing webhooks

An outgoing webhook sends events of a channel (or of the whole server) to a
URL of yours. Members who may manage webhooks in a channel create them under
**Channel settings, Webhooks**, or through the API:

```
POST /api/admin/channels/{channelId}/outgoing-webhooks
     {"name": "CI", "url": "https://ci.example.org/mnema", "events": ["MESSAGE_CREATE"]}
```

The answer carries the **signing secret, shown once** (`whsec_...`); rotate
it with `POST /api/admin/outgoing-webhooks/{id}/secret`. Up to 10 per channel.
The Owner can also create **server-wide** ones
(`POST /api/admin/outgoing-webhooks`) that receive the subscribed events of
every channel and `MEMBER_ADD`; only the Owner manages those.

### Events

| Event | When | `d` |
|---|---|---|
| `MESSAGE_CREATE` | a message is posted in the channel (messages posted by an incoming webhook are not sent, so two webhooks cannot feed each other) | the message, as in `GET /api/channels/{id}/messages` |
| `MESSAGE_UPDATE` | a message is edited | the message |
| `MESSAGE_DELETE` | a message is deleted | `{id, channel_id, parent_id}` |
| `MESSAGE_REACTIONS_UPDATE` | someone reacts or takes a reaction back | `{message_id, channel_id, reactions}` |
| `VOICE_STATE_UPDATE` | a member joins or leaves a Talk of the channel | `{action: "join"\|"leave", channel_id, user...}` |
| `MEMBER_ADD` | a member registers (server-wide webhooks only) | the member's public profile |
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
timestamps that are too old (replay protection):

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
- The queue lives **in memory**: at most 1000 events wait, and further ones are
  dropped when receivers cannot keep up. Events still waiting or waiting for a
  retry are **lost on a restart** of the server. Outgoing webhooks are a
  notification channel, not a log; fetch the messages from the API if you
  must not miss any. The cost is no extra table, no extra service.
- At most 20 delivery attempts a second go out in total, 4 at a time.
- `GET /api/admin/outgoing-webhooks/{id}/deliveries` shows the last 50
  attempts (event, attempt number, HTTP status, a short error code, duration).
  **The receiver's response is never stored or shown**, only the status code,
  because it could come from a service inside your network.
- "Send test event" (`POST .../test`) sends a `PING` once, right away, and
  shows the result. It is limited to 6 a minute per webhook and does not count
  towards the switch-off.

### Who sees what

Changing a webhook's URL makes the editor its creator, because they decide
where the events go and so who may see them. Rotating the secret, renaming it
and switching it on or off never change the creator.

A webhook delivers with its creator's rights. Before **every** attempt
(retries included) the server checks that the creator's account is enabled
and that they still hold *Manage webhooks* and *View channels* in the
webhook's channel (the Owner for server-wide ones). If not, nothing is
delivered, the list shows `access_ok: false`, and delivery resumes when the
rights return. Events of channels the creator cannot see are never sent, and
a channel's webhook only gets that channel's events.

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
separated; empty by default). **This is a risk decision:** everybody who may
manage webhooks can then make the server send POST requests with a JSON body
to those addresses. List single hosts rather than whole networks, and prefer
a receiver that checks the signature. The metadata addresses and the reserved
ranges cannot be opened this way.

## Security notes

See the *Webhooks* paragraph in [SECURITY.md](../SECURITY.md). In short: the
URL is a bearer secret, treat it like a password; tokens are stored hashed and
compared in constant time; a webhook cannot sign in, appear as a member, or
mention beyond what its creator may; embeds are rendered as sanitized text
and links, nothing is fetched from the senders' hosts.
