# Webhooks

Mnema Talk has **incoming webhooks**: a URL per channel that scripts, CI
systems and bots post messages to, in Discord's execute format. (Outgoing
webhooks, which tell your own service about events, are documented further
down once they ship.)

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

## Security notes

See the *Webhooks* paragraph in [SECURITY.md](../SECURITY.md). In short: the
URL is a bearer secret, treat it like a password; tokens are stored hashed and
compared in constant time; a webhook cannot sign in, appear as a member, or
mention beyond what its creator may; embeds are rendered as sanitized text
and links, nothing is fetched from the senders' hosts.
