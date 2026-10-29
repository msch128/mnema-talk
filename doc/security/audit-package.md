# Security audit package

This is the single entry point for an external security review of Mnema Talk.
It collects scope, architecture, threat model, cryptographic design, the attack
surface, the controls, what the project has already found, what it knowingly
accepts and where it would like a second pair of eyes. It summarises and links;
the normative description of each control stays in [SECURITY.md](../../SECURITY.md)
and the other documents linked below. Nothing here is a claim of an external
audit: none has taken place yet ([product status](../product-status.md)).

Contents:
[1 Scope and versions](#1-scope-and-versions) ·
[2 Architecture and data flows](#2-architecture-and-data-flows) ·
[3 Threat model](#3-threat-model) ·
[4 Cryptography](#4-cryptography) ·
[5 Attack surface](#5-attack-surface) ·
[6 Security controls](#6-security-controls) ·
[7 Results of internal reviews](#7-results-of-internal-reviews) ·
[8 Known limitations and accepted risks](#8-known-limitations-and-accepted-risks) ·
[9 Building and running a test instance](#9-building-and-running-a-test-instance) ·
[10 Suggested test focus](#10-suggested-test-focus)

## 1. Scope and versions

| Item | Value |
|---|---|
| Product | Mnema Talk, a self-hosted, invite-only chat and voice server for small communities |
| Audit target | Release **1.2.0** (branch `release/1.2.0`; the tag is the reference once it exists) |
| Previous releases | 1.1.0 (direct messages, blocks, reports, forwarding), 1.0.x (first stable line) |
| Repository | <https://github.com/msch128/mnema-talk> (public, source available to the reviewers) |
| Server | Go (module `go 1.27`), chi 5.5, pgx 5.11, Pion WebRTC 4.2, `golang.org/x/crypto`, `webpush-go` 1.4 |
| Web app | Vue 3 single-page app (TypeScript), `ts-mls` 1.6.4 for MLS, Vite build, embedded in the server binary |
| Data stores | PostgreSQL 18, S3-compatible object storage (SeaweedFS in the compose stack) |
| Optional services | coturn (TURN), update sidecars (`updater`, `recreator`, off by default) |
| Desktop client | Separate repository (<https://github.com/msch128/mnema-talk-desktop>, Tauri shell, **Beta**, installers unsigned at OS level) running a signed web bundle built from this repository |

**In scope:** the server (`cmd/server`, `internal/`), the web app (`web/`),
the signed web bundle format and its verification (`internal/webbundle`,
`desktop-trust/`), the compose deployment (`docker-compose.yml`, `Dockerfile`,
`deploy/`), the release pipeline (`.github/workflows/`, signing, provenance),
the self-update sidecar (`cmd/updater`, `internal/updaterd`).

**Out of scope, or only on request:** the desktop shell's Rust code and its
installer signing (separate repository, see [section 8](#8-known-limitations-and-accepted-risks)),
third-party services (push services, GitHub, GIF provider), the operator's
reverse proxy and host, social engineering of the Owner, and denial of service
by resource exhaustion on an instance you do not operate.

New in 1.2 (the diff since 1.1.0 is the freshest, least-reviewed code): Create-a-Talk
lobbies and temporary Talks with leaders, Stages (talk power, raise hand),
community events with RSVPs and reminders, status expiry, reminders, scheduled
send, anonymous polls, slow-mode presets, per-channel auto-delete, forum
channels, own session and device list with inactivity sign-out, "view server as
role", automod (words, links, raid, new accounts) with a report queue, per-member
moderation history and private notes, search filters and notification settings,
Discord template import, operator status page, and the removal of media frame
format version 2.

## 2. Architecture and data flows

One Go binary serves the REST API, a WebSocket hub, a WebRTC selective
forwarding unit (SFU) and the embedded web app. Component list:
[architecture](../architecture.md).

```
 Browser / desktop shell            Operator's reverse proxy (TLS)
        |  HTTPS, WSS                      |
        v                                  v
 +-----------------------------------------------------------+
 | mnema-talk                                                |
 |  chi router -> middleware -> REST handlers                |
 |  ws hub  (events, presence, voice signalling, MLS relay)  |
 |  sfu     (Pion, forwards sealed RTP, UDP)                 |
 |  web     (embedded SPA)                                   |
 +--------+---------------------+----------------+-----------+
          v                     v                v
     PostgreSQL            object storage     outbound: Web Push services,
   (all state, audit)      (uploads, avatars)  webhooks, link previews,
                                               GitHub release check
```

| Flow | Path | Protection | Server sees |
|---|---|---|---|
| REST | Browser to `/api/...` | TLS (operator), `HttpOnly` session cookie, origin check (CSRF), per-user and per-IP rate limits, body limits | Everything (text is server-readable by design) |
| WebSocket | `/api/ws`, one per session | Cookie auth on upgrade, origin check, re-checked every two minutes, per-event payload caps and flood limits, per-recipient permission filter | Everything except media payloads and MLS contents |
| Voice/video transport | UDP between browser and SFU | DTLS-SRTP | Encrypted RTP, plus metadata (who, when, sizes, layers) |
| Media E2EE | Inside the RTP payload, in the browser | MLS-derived AES-128-GCM key, Ed25519 signature per frame, replay window | Ciphertext and codec headers (authenticated, readable) |
| MLS handshake | Over the WebSocket as opaque messages | MLS signatures; server checks only framing and size | Key packages, welcomes, commits (public), who sent them |
| Web Push | Server to browser vendor push service | RFC 8291 encryption, VAPID; endpoint guarded by the SSRF resolver | Subscription endpoints and push contents before encryption |
| Storage | App to S3 API; media only through the app, never direct | Content sniffing, avatar/banner/emoji re-encode, per-account quotas; access check on every download | Plain files (attachments are stored as uploaded) |
| Outbound | Webhooks, link previews, update check, push | `internal/ssrf` resolve-vet-connect guard, no redirects, size caps | Targets chosen by members or admins |
| Desktop shell | Tauri app to the instance through its own proxy | Signed web bundle (Ed25519, pinned key, security floor), session cookie kept out of the page, strict CSP | Same as a browser |
| Update sidecar | Admin console to `updater` to `recreator` | Off by default, shared token, cosign signature and provenance check of the new image | n/a |

Further reading: [chat data flow, direct messages, search and forwarding](../architecture.md),
[Talks and Stages](../talks.md), [moderation, audit and automod](../moderation.md),
[roles and permissions](../roles-and-permissions.md), [webhooks](../webhooks.md),
[bots](../bots.md), [desktop trust model](../desktop-trusted-client.md).

## 3. Threat model

### 3.1 Actors

| Actor | Capabilities assumed |
|---|---|
| Anonymous internet client | Reach the public routes (health, legal, invite preview, sign-in, registration with an invite code, webhook execute with a secret) |
| Invited member | Valid session; default `@everyone` permissions; can upload, search, call, create DMs, create webhooks only if an admin allows |
| Member with roles | Additional permissions below their highest role; may not grant what they lack or touch members above them |
| Administrator / role with Administrator | Everything except Owner protections |
| Owner | The one `ADMIN_USERNAME` account; holds every permission, cannot be restricted by anybody |
| Bot / webhook holder | Token or URL secret; scoped to what a bot role or one channel allows |
| Other member as call peer | Inside the trust boundary for media they can read; limited by the group rules ([4.3](#43-what-a-group-member-can-do)) |
| Network attacker | Can observe and tamper with traffic between client and proxy only if TLS is broken or absent |
| Malicious or compromised server | **Out of scope for media confidentiality and code integrity in browsers** (the server serves the web app); in scope for what the desktop client and the safety code can still show |
| Malicious operator of the host | Out of scope (owns the database and keys) |
| Supply-chain attacker | Dependencies, CI, release signing, container images, update path |

### 3.2 Assets

Account credentials and sessions; the permission model's integrity (who may see
or do what); message and file confidentiality against non-members (text is
**not** hidden from the server or the Owner); direct-message privacy against
moderators and other members; media confidentiality against an honest server
and anyone on the path (E2EE); audit-log integrity; instance secrets
(`JWT_SECRET`, S3 and database credentials, VAPID key, TOTP encryption key
derived from `JWT_SECRET`); the signing keys of web bundles, desktop updates
and release images.

### 3.3 Trust boundaries

1. Internet to reverse proxy to app (TLS ends at the operator's proxy; client IPs are taken from `X-Forwarded-For` only from listed proxies).
2. Browser page to the server it was loaded from (same-origin; the server is trusted to serve code, except in the desktop client).
3. Member to member inside a call (MLS group; membership is asserted by the server, see [4.4](#44-authentication-of-group-members)).
4. App to PostgreSQL and object storage (private compose network; their ports are not published).
5. App to the outside (outbound requests pass the SSRF guard).
6. Release pipeline to operators (cosign signatures, provenance, pinned digests in compose).
7. Web bundle signer to desktop clients (offline root key, pinned bundle and updater keys).

## 4. Cryptography

### 4.1 What is end-to-end encrypted, and what is not

| Data | Protection |
|---|---|
| Microphone, camera, screen video, screen audio | End-to-end encrypted in the browser with MLS-derived keys; signed per frame |
| Text messages, attachments, avatars, DMs, polls, forum posts | **Not end-to-end encrypted, by design.** Readable by the server and the Owner's database access. TLS in transit; DMs are hidden from moderators by the permission model, not by cryptography. Decision is final: no text E2EE is planned |
| Metadata (who speaks when, frame sizes, layers, membership, timing) | Visible to the server |
| Web Push payloads | Encrypted for the browser (RFC 8291); the server knows the plaintext |
| Passwords | bcrypt; sessions are signed JWTs with a per-user version and per-token id; TOTP secrets AES-256-GCM encrypted under a key derived from `JWT_SECRET`; recovery codes keyed HMAC |

### 4.2 Media encryption

* **Group protocol.** MLS (RFC 9420) in the browser with `ts-mls` 1.6.4, one
  cipher suite: `MLS_128_DHKEMX25519_AES128GCM_SHA256_Ed25519`. One group per
  voice channel; the group id is the channel's UUID. The server relays
  handshakes as opaque messages and checks only framing and size.
* **Key derivation.** Every epoch, the media key is `MLS-Exporter("mnema-media-v1", groupId, 16)`,
  an AES-128-GCM key held in a non-extractable `CryptoKey`. The safety code is a
  hash (domain `mnema-safety-v1`) of the epoch authenticator, shown as 20 digits
  so members can compare it over another channel.
* **Frame format version 3** (the only version in 1.2):

  ```
  'M' '3' | epoch u32 | stream u32 | counter u32 | line u8 | nonce (12) |
  AES-GCM ciphertext + tag (16) | Ed25519 signature (64)
  ```

  AAD = channelId `00` senderId, epoch, stream, counter, line byte, clear bytes
  (codec headers that stay readable). Signed bytes = domain
  `mnema-media-frame-v3` || len(AAD) u32 || AAD || header and ciphertext. The
  line byte (source in the high nibble, screen layer in the low bits) binds a
  unit to the line it was made for; a receiver opens a unit only if it names the
  source the arriving line carries. The nonce is 96 fresh random bits per
  frame. The signature is verified before decryption, with the sender's MLS
  leaf signature key (an id with two leaves verifies nothing). The replay
  window (64 units per `(epoch, stream)`) is marked only after verification and
  decryption.
* **Other signed messages** over the handshake channel, each with its own
  domain string and the sender's leaf key: heartbeat (`mnema-media-heartbeat-v1`,
  epoch and increasing time, about every 5 minutes) and capability
  (`mnema-media-capability-v1`, being phased out with version 2).
* **Key rotation.** An epoch lasts at most 30 minutes (random jitter of two
  minutes per epoch): the committer (lowest member id) advances it with an
  update-path commit; every other member renews its own leaf on the same
  schedule. A member silent for 20 minutes is removed. The previous epoch's
  key opens frames for 30 seconds after a switch within the same group, then
  its keys and verifiers are deleted. After a removal the sender seals under
  the new epoch at once; after an add or rotation it keeps the old epoch for
  1.5 seconds so receivers have the commit.
* **Commit rules checked by every receiver** (after the MLS signature verifies):
  only the current committer may add or remove; an add must name an identity
  in the room whose key package the relay delivered and that holds no leaf;
  a remove must target a member the roster no longer lists, one silent for the
  absence limit, or one the same commit replaces; no commit may change another
  leaf's identity, signature key or encryption key. Details:
  [SECURITY.md, Voice and screenshare](../../SECURITY.md#security-model-summary).

### 4.3 What a group member can do

A member of a call can read the media of that call (it holds the key). It
cannot impersonate another member (frames are signed per leaf), remove a present
member, add a second leaf for an identity, or move units between a sender's
lines. A malicious committer keeps the powers of the role: removing a member
that looks silent to it, delaying joins, stopping the call.

### 4.4 Authentication of group members

MLS credentials are `basic` credentials holding the user id; there is no
certificate authority and no binding to a long-term identity key. Membership is
decided by the roster the **server** asserts plus a key package the server
relays. An active or compromised server can therefore insert itself as a member,
swap a present member's leaf for its own, or steer a newcomer into a group of
its own, and read the media. The safety code and the "who holds the keys" list
exist so members can notice this; they do not help against a server that also
serves modified web app code. This is the documented boundary, not a bug: see
[8](#8-known-limitations-and-accepted-risks).

### 4.5 Other cryptographic components

| Component | Design |
|---|---|
| Web bundle signing | Ed25519 over the bundle manifest; key pinned in the desktop client; offline root key can revoke a leaked CI key; security floor refuses older bundles ([desktop trust model](../desktop-trusted-client.md)) |
| Desktop self-update | Tauri updater, minisign signature against a pinned key distinct from the bundle key, HTTPS from github.com |
| Release images | cosign signature and SLSA provenance, verified by the update sidecar against the pinned workflow identity before anything is pulled |
| Outgoing webhooks | HMAC-SHA256 over `t.body`, secret stored separately from the queue |
| Web Push | RFC 8291 `aes128gcm`, VAPID (RFC 8292), private key in the database |
| Session cookies | `HttpOnly`, `SameSite=Lax`, `Secure` and `__Host-` prefix on HTTPS |

## 5. Attack surface

Everything below is generated by `scripts/gen-attack-surface.py` from
`api/openapi.json` (kept complete by `TestOpenAPICoversRoutes`) and the event
registry (`internal/events/testdata/events.golden.json`). Run the script again
after the routes change; `--check` fails when this section is stale. Only the
authentication gate is listed per route. Which permission a route needs
(for example *Manage channels* or *Manage reports*) is in its OpenAPI
description and in [roles and permissions](../roles-and-permissions.md); the
permission checks live in the handlers and middleware, never in the client.

Public (unauthenticated) surface at a glance: `GET /api/health`,
`GET /api/legal`, `GET /api/invites/{code}`, sign-in and registration routes,
password-reset request and confirmation, and `POST /api/webhooks/{id}/{token}`
(secret in the URL, no cookie). The bot gateway and `/api/bot/v1` authenticate
with a bot token. `/api/metrics` needs a bearer token and exists only when
`METRICS_TOKEN` is set; `/api/docs` and `/api/openapi.json` exist only when
`API_DOCS_ENABLED` is set. Media is served only through the app, after a
permission check on the channel.

<!-- attack-surface:begin -->

### REST routes

261 operations on 209 paths, grouped by API tag.


#### Admin (49)

| Method | Path | Authentication | Summary |
|---|---|---|---|
| GET | `/api/admin/audit-log` | Session + admin-area permission | List audit log entries |
| GET | `/api/admin/bans` | Session + admin-area permission | List bans |
| POST | `/api/admin/categories` | Session + admin-area permission | Create a category |
| PATCH | `/api/admin/categories/{id}` | Session + admin-area permission | Rename a category |
| DELETE | `/api/admin/categories/{id}` | Session + admin-area permission | Delete a category |
| POST | `/api/admin/channels` | Session + admin-area permission | Create a channel |
| PATCH | `/api/admin/channels/{id}` | Session + admin-area permission | Rename a channel, change its topic or member limit |
| DELETE | `/api/admin/channels/{id}` | Session + admin-area permission | Delete a channel |
| PUT | `/api/admin/channels/{id}/auto-delete` | Session + admin-area permission | Set a channel's auto-delete |
| POST | `/api/admin/channels/{id}/duplicate` | Session + admin-area permission | Duplicate a channel |
| POST | `/api/admin/import/discord/apply` | Session + admin-area permission | Import a Discord server structure |
| GET | `/api/admin/import/discord/imports` | Session + admin-area permission | List Discord imports |
| GET | `/api/admin/import/discord/imports/{id}` | Session + admin-area permission | Read a Discord import |
| POST | `/api/admin/import/discord/imports/{id}/undo` | Session + admin-area permission | Undo a Discord import |
| POST | `/api/admin/import/discord/preview` | Session + admin-area permission | Preview a Discord server import |
| GET | `/api/admin/invites` | Session + admin-area permission | List invites |
| POST | `/api/admin/invites` | Session + admin-area permission | Create an invite |
| DELETE | `/api/admin/invites/{id}` | Session + admin-area permission | Delete an invite |
| PUT | `/api/admin/layout` | Session + admin-area permission | Apply category order and channel placement |
| GET | `/api/admin/media` | Session + admin-area permission | List all media, newest first |
| GET | `/api/admin/media/orphans` | Session + admin-area permission | Count orphaned media objects |
| POST | `/api/admin/media/orphans/cleanup` | Session + admin-area permission | Delete orphaned media objects |
| POST | `/api/admin/media/prune` | Session + admin-area permission | Delete old chat attachments |
| GET | `/api/admin/media/stats` | Session + admin-area permission | Storage totals |
| DELETE | `/api/admin/media/{id}` | Session + admin-area permission | Delete a media object |
| GET | `/api/admin/status` | Session + admin-area permission | Operator status |
| GET | `/api/admin/system` | Session + admin-area permission | System status |
| POST | `/api/admin/system/check` | Session + admin-area permission | Check for a new release now |
| POST | `/api/admin/system/mail/test` | Session + admin-area permission | Send a test e-mail |
| GET | `/api/admin/system/ops` | Session + admin-area permission | Operator metrics |
| GET | `/api/admin/system/update` | Session + admin-area permission | Update status |
| POST | `/api/admin/system/update` | Session + admin-area permission | Update the server now |
| GET | `/api/admin/users` | Session + admin-area permission | List all accounts |
| POST | `/api/admin/users/{id}/ban` | Session + admin-area permission | Ban a member |
| DELETE | `/api/admin/users/{id}/ban` | Session + admin-area permission | Lift a ban |
| POST | `/api/admin/users/{id}/disable` | Session + admin-area permission | Disable an account |
| POST | `/api/admin/users/{id}/enable` | Session + admin-area permission | Re-enable an account |
| POST | `/api/admin/users/{id}/kick` | Session + admin-area permission | Remove a user from voice |
| POST | `/api/admin/users/{id}/login-lockout/clear` | Session + admin-area permission | Clear an account's login lockout |
| POST | `/api/admin/users/{id}/password` | Session + admin-area permission | Set a user's password |
| POST | `/api/admin/users/{id}/password-reset` | Session + admin-area permission | Generate a temporary password |
| POST | `/api/admin/users/{id}/sessions/revoke` | Session + admin-area permission | Revoke all sessions of a user |
| PUT | `/api/admin/users/{id}/status` | Session + admin-area permission | Edit or clear a member's status line |
| POST | `/api/admin/users/{id}/timeout` | Session + admin-area permission | Time out a member |
| DELETE | `/api/admin/users/{id}/timeout` | Session + admin-area permission | End a member's timeout |
| POST | `/api/admin/users/{id}/two-factor/reset` | Session + admin-area permission | Reset a member's two-factor sign-in |
| POST | `/api/channels/{channelID}/tags` | Session | Create a forum tag |
| PATCH | `/api/channels/{channelID}/tags/{tagID}` | Session | Update a forum tag |
| DELETE | `/api/channels/{channelID}/tags/{tagID}` | Session | Delete a forum tag |

#### Auth (15)

| Method | Path | Authentication | Summary |
|---|---|---|---|
| POST | `/api/auth/email/verify` | None (public) | Confirm an e-mail address |
| POST | `/api/auth/login` | None (public) | Sign in |
| POST | `/api/auth/login/2fa` | None (public) | Finish a sign-in with the second factor |
| POST | `/api/auth/logout` | None (public) | Sign out |
| POST | `/api/auth/logout-all` | Session | Sign out everywhere |
| GET | `/api/auth/me` | Session | Current user |
| GET | `/api/auth/options` | None (public) | Sign-in options |
| PUT | `/api/auth/password` | Session | Change own password |
| POST | `/api/auth/password-reset/confirm` | None (public) | Set a new password with a reset link |
| POST | `/api/auth/password-reset/request` | None (public) | Ask for a password reset mail |
| POST | `/api/auth/register` | None (public) | Register with an invite code |
| GET | `/api/auth/sessions` | Session | List my sessions |
| POST | `/api/auth/sessions/revoke-others` | Session | Sign out all my other sessions |
| DELETE | `/api/auth/sessions/{id}` | Session | Sign out one of my sessions |
| GET | `/api/invites/{code}` | None (public) | Preview an invite |

#### Bot API (19)

| Method | Path | Authentication | Summary |
|---|---|---|---|
| GET | `/api/bot/v1/channels` | Bot token | List the bot's channels |
| GET | `/api/bot/v1/channels/{channelID}/messages` | Bot token | List channel messages |
| POST | `/api/bot/v1/channels/{channelID}/messages` | Bot token | Post a message |
| GET | `/api/bot/v1/channels/{channelID}/messages/{messageID}` | Bot token | Get a message |
| PATCH | `/api/bot/v1/channels/{channelID}/messages/{messageID}` | Bot token | Edit the bot's own message |
| DELETE | `/api/bot/v1/channels/{channelID}/messages/{messageID}` | Bot token | Delete a message |
| PUT | `/api/bot/v1/channels/{channelID}/messages/{messageID}/reactions/{emoji}/@me` | Bot token | Add the bot's reaction |
| DELETE | `/api/bot/v1/channels/{channelID}/messages/{messageID}/reactions/{emoji}/@me` | Bot token | Remove the bot's reaction |
| POST | `/api/bot/v1/channels/{channelID}/upload` | Bot token | Post a message with an attachment |
| GET | `/api/bot/v1/commands` | Bot token | List the bot's commands |
| POST | `/api/bot/v1/commands` | Bot token | Register a command |
| PUT | `/api/bot/v1/commands` | Bot token | Replace the bot's commands |
| DELETE | `/api/bot/v1/commands/{commandID}` | Bot token | Delete a command |
| GET | `/api/bot/v1/gateway` | Bot token | Get the gateway address |
| POST | `/api/bot/v1/interactions/{interactionID}/{token}/callback` | Bot token | Respond to an interaction |
| POST | `/api/bot/v1/interactions/{interactionID}/{token}/followups` | Bot token | Send a follow-up to an interaction |
| GET | `/api/bot/v1/members` | Bot token | List members |
| GET | `/api/bot/v1/members/{userID}` | Bot token | Get a member |
| GET | `/api/bot/v1/users/@me` | Bot token | Get the bot itself |

#### Bots (15)

| Method | Path | Authentication | Summary |
|---|---|---|---|
| GET | `/api/admin/bots` | Session + admin-area permission | List bots |
| POST | `/api/admin/bots` | Session + admin-area permission | Create a bot |
| GET | `/api/admin/bots/{id}` | Session + admin-area permission | Get a bot |
| PATCH | `/api/admin/bots/{id}` | Session + admin-area permission | Change a bot |
| DELETE | `/api/admin/bots/{id}` | Session + admin-area permission | Delete a bot |
| GET | `/api/admin/bots/{id}/access` | Session + admin-area permission | Show what a bot can reach |
| POST | `/api/admin/bots/{id}/avatar` | Session + admin-area permission | Set a bot's avatar |
| GET | `/api/admin/bots/{id}/commands` | Session + admin-area permission | List a bot's commands |
| PATCH | `/api/admin/bots/{id}/commands/{commandID}` | Session + admin-area permission | Enable or disable a command |
| DELETE | `/api/admin/bots/{id}/commands/{commandID}` | Session + admin-area permission | Delete a command |
| PUT | `/api/admin/bots/{id}/roles` | Session + admin-area permission | Set a bot's roles |
| GET | `/api/admin/bots/{id}/tokens` | Session + admin-area permission | List a bot's tokens |
| POST | `/api/admin/bots/{id}/tokens` | Session + admin-area permission | Issue a token |
| DELETE | `/api/admin/bots/{id}/tokens/{tokenID}` | Session + admin-area permission | Revoke a token |
| POST | `/api/admin/bots/{id}/tokens/{tokenID}/regenerate` | Session + admin-area permission | Regenerate a token |

#### Channels (15)

| Method | Path | Authentication | Summary |
|---|---|---|---|
| PUT | `/api/categories/{id}/notifications` | Session | Set category notification level |
| GET | `/api/channels` | Session | List categories and channels |
| GET | `/api/channels/{channelID}/first-unread` | Session | First unread message of a channel |
| PUT | `/api/channels/{channelID}/notifications` | Session | Set channel notification level |
| GET | `/api/channels/{channelID}/posts` | Session | List forum posts |
| POST | `/api/channels/{channelID}/read` | Session | Mark a channel read |
| GET | `/api/channels/{channelID}/tags` | Session | List forum tags |
| POST | `/api/channels/{channelID}/unread` | Session | Mark unread from a message |
| POST | `/api/dms/{id}/talk` | Session | Start a Talk from a direct message conversation |
| GET | `/api/notification-settings` | Session | Notification settings |
| PUT | `/api/notification-settings/server` | Session | Set server notification default |
| GET | `/api/read-state` | Session | Unread summary of all channels |
| POST | `/api/read-state/read-all` | Session | Mark everything read |
| PATCH | `/api/talks/{id}` | Session | Rename a temporary Talk or set its member limit |
| POST | `/api/talks/{id}/invites` | Session | Invite a conversation to a Talk |

#### Commands (2)

| Method | Path | Authentication | Summary |
|---|---|---|---|
| GET | `/api/channels/{channelID}/commands` | Session | List the slash commands of a channel |
| POST | `/api/channels/{channelID}/interactions` | Session | Run a slash command |

#### Direct messages (5)

| Method | Path | Authentication | Summary |
|---|---|---|---|
| GET | `/api/dms` | Session | List direct messages |
| POST | `/api/dms` | Session | Start a direct message |
| GET | `/api/dms/{id}` | Session | Get a direct message |
| POST | `/api/dms/{id}/members` | Session | Add a member to a group direct message |
| DELETE | `/api/dms/{id}/members/{user}` | Session | Leave a group direct message |

#### Emoji (6)

| Method | Path | Authentication | Summary |
|---|---|---|---|
| POST | `/api/admin/emoji` | Session + admin-area permission | Add a custom emoji |
| PATCH | `/api/admin/emoji/{id}` | Session + admin-area permission | Rename a custom emoji |
| DELETE | `/api/admin/emoji/{id}` | Session + admin-area permission | Delete a custom emoji |
| GET | `/api/emoji` | Session | List custom emoji |
| GET | `/api/emoji/{id}/image` | Session | Custom emoji image |
| GET | `/api/emoji/{id}/still` | Session | Custom emoji still frame |

#### Events (10)

| Method | Path | Authentication | Summary |
|---|---|---|---|
| GET | `/api/events` | Session | List community events |
| POST | `/api/events` | Session | Create a community event |
| GET | `/api/events/{id}` | Session | Get a community event |
| PATCH | `/api/events/{id}` | Session | Update a community event |
| POST | `/api/events/{id}/cancel` | Session | Cancel a community event |
| POST | `/api/events/{id}/cover` | Session | Upload event cover image |
| DELETE | `/api/events/{id}/cover` | Session | Remove event cover image |
| PUT | `/api/events/{id}/rsvp` | Session | Set RSVP response for an event |
| DELETE | `/api/events/{id}/rsvp` | Session | Delete RSVP response for an event |
| GET | `/api/events/{id}/rsvps` | Session | List RSVPs for an event |

#### Media (4)

| Method | Path | Authentication | Summary |
|---|---|---|---|
| POST | `/api/channels/{channelID}/upload` | Session | Post a message with an attachment |
| GET | `/api/link-preview` | Session | Link card for a URL |
| GET | `/api/link-preview/image` | Session | Proxied preview image |
| GET | `/api/media/{id}` | Session | Download a media object |

#### Messages (32)

| Method | Path | Authentication | Summary |
|---|---|---|---|
| GET | `/api/channels/{channelID}/messages` | Session | List channel messages |
| POST | `/api/channels/{channelID}/messages` | Session | Post a message |
| POST | `/api/channels/{channelID}/messages/bulk-delete` | Session | Delete a member's latest messages in a channel |
| PUT | `/api/channels/{channelID}/messages/{messageID}` | Session | Edit own message |
| DELETE | `/api/channels/{channelID}/messages/{messageID}` | Session | Delete a message |
| GET | `/api/channels/{channelID}/messages/{messageID}/edits` | Session | Edit history of a message |
| PUT | `/api/channels/{channelID}/messages/{messageID}/pin` | Session | Pin a message |
| DELETE | `/api/channels/{channelID}/messages/{messageID}/pin` | Session | Unpin a message |
| GET | `/api/channels/{channelID}/pins` | Session | List a channel's pinned messages |
| POST | `/api/channels/{channelID}/posts` | Session | Create a forum post |
| PATCH | `/api/channels/{channelID}/posts/{messageID}` | Session | Update a forum post |
| POST | `/api/channels/{channelID}/scheduled` | Session | Schedule a message |
| GET | `/api/me/reminders` | Session | List my reminders |
| POST | `/api/me/reminders` | Session | Remind me of a message |
| DELETE | `/api/me/reminders/{reminderID}` | Session | Cancel or dismiss a reminder |
| GET | `/api/me/saved` | Session | List saved messages |
| PUT | `/api/me/saved/{messageID}` | Session | Save a message |
| DELETE | `/api/me/saved/{messageID}` | Session | Remove a saved message |
| GET | `/api/me/scheduled` | Session | List my scheduled messages |
| PATCH | `/api/me/scheduled/{scheduledID}` | Session | Edit a scheduled message |
| DELETE | `/api/me/scheduled/{scheduledID}` | Session | Delete a scheduled message |
| GET | `/api/mentions` | Session | Mentions inbox |
| GET | `/api/messages/by-number/{number}` | Session | Get a message by its link number |
| GET | `/api/messages/{messageID}/forward` | Session | Read the original of a forwarded message |
| POST | `/api/messages/{messageID}/forward` | Session | Forward a message |
| POST | `/api/messages/{messageID}/reactions` | Session | Toggle a reaction |
| POST | `/api/messages/{messageID}/report` | Session | Report a message |
| GET | `/api/messages/{messageID}/thread` | Session | Get a thread |
| GET | `/api/search` | Session | Search messages |
| GET | `/api/threads/following` | Session | Threads the caller follows |
| PUT | `/api/threads/{threadID}/follow` | Session | Follow a thread |
| DELETE | `/api/threads/{threadID}/follow` | Session | Unfollow a thread |

#### Moderation (12)

| Method | Path | Authentication | Summary |
|---|---|---|---|
| GET | `/api/admin/automod` | Session + admin-area permission | List automod rules |
| POST | `/api/admin/automod/rules` | Session + admin-area permission | Create an automod rule |
| PUT | `/api/admin/automod/rules/{id}` | Session + admin-area permission | Replace an automod rule |
| DELETE | `/api/admin/automod/rules/{id}` | Session + admin-area permission | Delete an automod rule |
| GET | `/api/admin/reports` | Session + admin-area permission | List reports |
| GET | `/api/admin/reports/{id}` | Session + admin-area permission | Get a report |
| DELETE | `/api/admin/reports/{id}` | Session + admin-area permission | Delete a report |
| POST | `/api/admin/reports/{id}/resolve` | Session + admin-area permission | Close a report |
| GET | `/api/admin/users/{id}/moderation` | Session + admin-area permission | A member's moderation history |
| POST | `/api/admin/users/{id}/moderation/notes` | Session + admin-area permission | Add a moderator note |
| PUT | `/api/admin/users/{id}/moderation/notes/{noteID}` | Session + admin-area permission | Edit your moderator note |
| DELETE | `/api/admin/users/{id}/moderation/notes/{noteID}` | Session + admin-area permission | Delete a moderator note |

#### Onboarding (5)

| Method | Path | Authentication | Summary |
|---|---|---|---|
| GET | `/api/admin/onboarding` | Session + admin-area permission | Read the onboarding settings |
| PUT | `/api/admin/onboarding` | Session + admin-area permission | Set the onboarding settings |
| GET | `/api/onboarding` | Session | Read the onboarding state |
| POST | `/api/onboarding/accept` | Session | Accept the community rules |
| PUT | `/api/onboarding/tips` | Session | Store dismissed tips |

#### Polls (4)

| Method | Path | Authentication | Summary |
|---|---|---|---|
| GET | `/api/channels/{channelID}/messages/{messageID}/poll` | Session | Get a poll with my votes |
| POST | `/api/channels/{channelID}/messages/{messageID}/poll/end` | Session | End a poll early |
| PUT | `/api/channels/{channelID}/messages/{messageID}/poll/votes` | Session | Vote in a poll |
| POST | `/api/channels/{channelID}/polls` | Session | Post a poll |

#### Push (6)

| Method | Path | Authentication | Summary |
|---|---|---|---|
| GET | `/api/push/config` | Session | Web Push settings |
| PUT | `/api/push/preferences` | Session | Set the push preview |
| GET | `/api/push/subscriptions` | Session | List my push devices |
| POST | `/api/push/subscriptions` | Session | Turn push on for this device |
| DELETE | `/api/push/subscriptions/{id}` | Session | Turn push off for a device |
| POST | `/api/push/test` | Session | Send a test push |

#### Realtime (2)

| Method | Path | Authentication | Summary |
|---|---|---|---|
| GET | `/api/webrtc/config` | Session | ICE servers for voice |
| GET | `/api/ws` | Session cookie on upgrade, origin check | WebSocket event stream |

#### Roles (14)

| Method | Path | Authentication | Summary |
|---|---|---|---|
| GET | `/api/admin/categories/{id}/overwrites` | Session + admin-area permission | Permission overwrites of a category |
| PUT | `/api/admin/categories/{id}/overwrites` | Session + admin-area permission | Replace the permission overwrites of a category |
| GET | `/api/admin/channels/{id}/overwrites` | Session + admin-area permission | Permission overwrites of a channel |
| PUT | `/api/admin/channels/{id}/overwrites` | Session + admin-area permission | Replace the permission overwrites of a channel |
| POST | `/api/admin/roles` | Session + admin-area permission | Create a role |
| PUT | `/api/admin/roles/order` | Session + admin-area permission | Reorder roles |
| PATCH | `/api/admin/roles/{id}` | Session + admin-area permission | Edit a role |
| DELETE | `/api/admin/roles/{id}` | Session + admin-area permission | Delete a role |
| GET | `/api/admin/roles/{id}/view-as` | Session + admin-area permission | View the server as a role |
| GET | `/api/admin/upload-limit` | Session + admin-area permission | Read the global upload limit |
| PUT | `/api/admin/upload-limit` | Session + admin-area permission | Set the global upload limit |
| PUT | `/api/admin/users/{id}/roles` | Session + admin-area permission | Set a member's roles |
| GET | `/api/permissions` | Session | Own permissions |
| GET | `/api/roles` | Session | List roles |

#### System (5)

| Method | Path | Authentication | Summary |
|---|---|---|---|
| GET | `/api/docs` | None (public) | API reference |
| GET | `/api/health` | None (public) | Health check |
| GET | `/api/legal` | None (public) | Operator details and privacy-policy facts |
| GET | `/api/metrics` | Bearer METRICS_TOKEN | Prometheus metrics |
| GET | `/api/openapi.json` | Session | This API description |

#### Users (27)

| Method | Path | Authentication | Summary |
|---|---|---|---|
| GET | `/api/me/blocks` | Session | List blocked members |
| PUT | `/api/me/blocks/{userID}` | Session | Block a member |
| DELETE | `/api/me/blocks/{userID}` | Session | Unblock a member |
| GET | `/api/members` | Session | List members |
| POST | `/api/users/me/2fa/disable` | Session | Turn off two-factor sign-in |
| POST | `/api/users/me/2fa/enable` | Session | Turn on two-factor sign-in |
| POST | `/api/users/me/2fa/recovery-codes` | Session | Make new recovery codes |
| POST | `/api/users/me/2fa/setup` | Session | Start two-factor setup |
| GET | `/api/users/me/account` | Session | Own sign-in security |
| POST | `/api/users/me/avatar` | Session | Upload avatar |
| DELETE | `/api/users/me/avatar` | Session | Remove avatar |
| PUT | `/api/users/me/banner` | Session | Set the profile banner |
| DELETE | `/api/users/me/banner` | Session | Remove the profile banner |
| POST | `/api/users/me/delete` | Session | Delete my account |
| PUT | `/api/users/me/email` | Session | Set the account's e-mail address |
| POST | `/api/users/me/email/remove` | Session | Remove the account's e-mail address |
| POST | `/api/users/me/email/resend` | Session | Send the confirmation mail again |
| GET | `/api/users/me/export` | Session | Status of my data export |
| POST | `/api/users/me/export` | Session | Request an export of my data |
| GET | `/api/users/me/export/download` | Session | Download my data export |
| PUT | `/api/users/me/locale` | Session | Set UI language |
| PUT | `/api/users/me/presence` | Session | Choose presence |
| GET | `/api/users/me/privacy` | Session | Read the privacy settings |
| PUT | `/api/users/me/privacy` | Session | Set the privacy settings |
| PUT | `/api/users/me/profile` | Session | Update display name and bio |
| PUT | `/api/users/me/status` | Session | Set own status line |
| GET | `/api/users/{userID}` | Session | Get a user profile |

#### Webhooks (14)

| Method | Path | Authentication | Summary |
|---|---|---|---|
| GET | `/api/admin/channels/{id}/outgoing-webhooks` | Session + admin-area permission | List a channel's outgoing webhooks |
| POST | `/api/admin/channels/{id}/outgoing-webhooks` | Session + admin-area permission | Create an outgoing webhook |
| GET | `/api/admin/channels/{id}/webhooks` | Session + admin-area permission | List a channel's webhooks |
| POST | `/api/admin/channels/{id}/webhooks` | Session + admin-area permission | Create a webhook |
| PATCH | `/api/admin/outgoing-webhooks/{id}` | Session + admin-area permission | Change an outgoing webhook |
| DELETE | `/api/admin/outgoing-webhooks/{id}` | Session + admin-area permission | Delete an outgoing webhook |
| GET | `/api/admin/outgoing-webhooks/{id}/deliveries` | Session + admin-area permission | Recent deliveries of an outgoing webhook |
| POST | `/api/admin/outgoing-webhooks/{id}/secret` | Session + admin-area permission | Rotate an outgoing webhook's secret |
| POST | `/api/admin/outgoing-webhooks/{id}/test` | Session + admin-area permission | Send a test event |
| PATCH | `/api/admin/webhooks/{id}` | Session + admin-area permission | Change a webhook |
| DELETE | `/api/admin/webhooks/{id}` | Session + admin-area permission | Delete a webhook |
| POST | `/api/admin/webhooks/{id}/avatar` | Session + admin-area permission | Set a webhook's avatar |
| POST | `/api/admin/webhooks/{id}/token` | Session + admin-area permission | Regenerate a webhook's token |
| POST | `/api/webhooks/{id}/{token}` | Secret in URL | Execute a webhook |

### WebSocket events

**Server to client (55):** `block_updated`, `channels_changed`, `dm_created`, `dm_member_added`, `dm_member_removed`, `emoji_changed`, `event_delete`, `event_reminder`, `event_update`, `forum_tags_changed`, `interaction_response`, `kicked`, `media_handshake`, `member_joined`, `member_timeout`, `message_create`, `message_delete`, `message_reaction`, `message_update`, `moderation_notice`, `permissions_changed`, `pins_changed`, `poll_update`, `pong`, `presence_snapshot`, `presence_update`, `read_state`, `reminder_update`, `roles_changed`, `saved_updated`, `scheduled_message_update`, `screen_layers`, `screen_layers_wanted`, `screen_quality`, `screen_viewers`, `server_info`, `stage_hand`, `stage_talk_power`, `system_update`, `talk_leader`, `talk_moved`, `talk_reaction`, `thread_follow_updated`, `typing`, `user_stats`, `user_update`, `voice_kicked`, `voice_mute_state`, `voice_rooms`, `voice_snapshot`, `voice_speaking`, `voice_state_update`, `webrtc_candidate`, `webrtc_media_state`, `webrtc_offer`


**Client to server (20):** `media_handshake`, `ping`, `presence_idle`, `stage_hand`, `stage_talk_power`, `talk_reaction`, `typing`, `voice_join`, `voice_leave`, `voice_mute_state`, `voice_speaking`, `webrtc_answer`, `webrtc_camera_stop`, `webrtc_candidate`, `webrtc_diag`, `webrtc_layers`, `webrtc_request_keyframe`, `webrtc_screenshare_start`, `webrtc_screenshare_stop`, `webrtc_subscribe`

<!-- attack-surface:end -->

## 6. Security controls

| Control | Where | Summary |
|---|---|---|
| Permission engine | `internal/perm`, [roles and permissions](../roles-and-permissions.md) | In-memory snapshot resolving `@everyone`, roles, category and channel overwrites and the hierarchy; Owner shortcut; timeouts and rules acceptance folded in; fails closed when the snapshot cannot load. Enforced in REST, the WebSocket hub (per-recipient delivery), search, push, mention inbox, export, media download and the SFU (speak/video) |
| DM isolation | `perm.Snapshot.Channel`, `dm_members` | Membership is the only rule, evaluated before the Owner shortcut; search re-tests membership in SQL; DM events never reach webhooks or the bot gateway |
| Sessions | `internal/auth` | Per-user `token_version` and per-token id revocation; own device list and inactivity sign-out (1.2); sockets re-checked every two minutes |
| Rate limits | `internal/httpx`, per feature | Per-IP and per-user limits on REST, uploads, previews, search (two concurrent per user, 5 s statement timeout), login lockout tiers, WebSocket frame/event/handshake budgets per connection and per account, voice and Talk creation gates |
| Origin / CSRF | `httpx.CSRFExcept` | State-changing requests and upgrades need an allowed `Origin`; exceptions are the webhook execute route and the bot API, which ignore cookies |
| CSP and headers | `httpx.SecurityHeaders` | No inline script or style (`style-src-attr 'none'`), no eval, `frame-ancestors 'none'`, `nosniff`, referrer policy; e2e specs fail on any violation |
| SSRF guard | `internal/ssrf` | Resolve once, vet every address (public unicast only, also IPv4-mapped), connect to the vetted address, no redirects, no proxies; used by webhooks, link previews, push endpoints and the Discord template fetch |
| Content handling | `internal/media`, `imgproc` | Content sniffing, risky types served only as downloads, filename sanitising; avatars, banners and emoji decoded, bounded, re-encoded; pixel and frame budgets before decode |
| Markdown rendering | `web/src/lib/markdown` | Tree of plain nodes, one HTML-producing module with attribute allow-lists; single `v-html` sink |
| Audit log | `internal/audit` | Append-only (database trigger), visibility classes per action (`VisAuditors`, `VisChannel`, `VisPrivileged`), no secrets stored, scrub-only update path for account erasure |
| Account erasure and export | `internal/auth/account_*` | Transactional, foreign-key-walking test, tombstone row guarded by a trigger |
| Automod (1.2) | `internal/automod` | Word, link, raid and new-account rules, hits go to the report queue; DM hits store no text |
| Secrets | `internal/config` | Environment only; production refuses weak secrets and wide trusted-proxy ranges; gitleaks in CI and pre-commit |
| Supply chain | `.github/workflows`, `scripts/` | Actions pinned by commit SHA, compose images pinned by digest, cosign and provenance, `govulncheck`, `npm audit`, third-party notices |
| Fuzzing | `make fuzz-smoke` | Go fuzz targets for the search builder, mentions, durations, URL guard and version parser run in CI for seconds each; seed corpora run with every `go test` |

## 7. Results of internal reviews

All reviews so far are internal. They are listed so you can see what was
looked at and what was found, and so you can spend your time elsewhere.

### 7.1 1.0 six-lane review (2026-10-22)

Lanes: authentication and sessions, permissions, content and media,
integrations (webhooks, bots, link previews), realtime and voice, supply chain.
Result: **0 Critical, 0 High**; every Medium was fixed with a test that fails
without the fix. Deferred past 1.0: frame version 2 (now removed in 1.2) and
regroup collusion (Info). Closed in 1.1: image signature check in the updater,
binding of legacy bot tokens. Source: [SECURITY.md](../../SECURITY.md#10-internal-security-review).
Not covered by that review (reviewed per change by automated security review and
a verification pass): e-mail/SMTP, two-factor sign-in, account deletion and
export, Web Push and the installable app.

### 7.2 1.1 review

Read-only review of the 1.1.0 diff (direct messages, blocks, reports, forwarding,
saved messages, GIF proxy, avatar and banner re-encoding, migrations
0049 to 0058) in five areas: authorization/IDOR, information leaks,
media processing and GDPR, migrations, input validation/rate limits/SQL. Result:
no exploitable finding. It was done by an automated model-based reviewer and
spot-checked by the maintainers, so treat it as lower assurance than a human
review.

### 7.3 1.2 review (diff v1.1.0 to a 1.2 integration snapshot)

Read-only review of the new routes, WebSocket events, stores and migrations,
with one targeted test run through a build overlay. No Critical or High
findings. The status column follows the 1.2 security-fix pull request. Items are
described by class, without exploit detail.

| ID | Severity | Area | Class | Status at time of writing |
|---|---|---|---|---|
| M1 | Medium | Automod links | Block-list mode can be bypassed by input the renderer links but the rule does not parse | Fixed in the 1.2 security-fix pull request, with tests |
| M2 | Medium | Events | Broadcast payload carries a viewer-specific field of the acting member | Fixed in the 1.2 security-fix pull request, with tests |
| M3 | Medium | Erasure/export | A new `messages` column is missing from account erasure and data export | Fixed in the 1.2 security-fix pull request, with tests |
| L1 | Low | Automod | Some user-supplied text fields (forum titles and tags, poll options) are not screened | Fixed in the 1.2 security-fix pull request, with tests |
| L2 | Low | Automod | Word matching does not normalise all Unicode look-alikes and invisible characters | Fixed in the 1.2 security-fix pull request, with tests |
| L3 | Low | Scheduled send | Burst of due messages bypasses the request limiter | Accepted, documented in SECURITY.md |
| L4 | Low | Polls | Live tallies allow inference in very small audiences (by design, documented hardening) | Broadcast tally fixed; counts shown by REST documented as accepted |
| L5 | Low | Forum | Author edit rights on title and tags are looser than send rights | Fixed in the 1.2 security-fix pull request, with tests |
| L6 | Low | Events | Retraction of hidden events can be missed on two quick permission changes (REST stays correct) | Fixed in the 1.2 security-fix pull request, with tests |
| L7 | Low | Talks | Presence queue overflow or timer race can mis-remove or keep a Talk | Fixed in the 1.2 security-fix pull request, with tests |
| L8 | Low | Discord import | Duplicate ids in a template can make preview and apply disagree | Accepted, documented in SECURITY.md |
| L9 | Low | Discord import | Undo has a small race with new messages | Fixed in the 1.2 security-fix pull request, with tests |
| L10 | Low | Privacy | Minor leftovers after erasure/export (names in import records, notes, filenames of removed attachments in search) | Fixed in the 1.2 security-fix pull request, with tests |
| L11 | Low | Moderation | Timeline readable with *Manage reports* only, wider than audit classes | Accepted, documented in SECURITY.md |
| L12 | Info | WebSocket | Per-user gates reset on reconnect, bounded by the connect limiter | Accepted, documented in SECURITY.md |

The review also records what it verified clean: Talks, lobbies, leaders and
invites; Stages; community events on hidden Talks; reminders and scheduled
send; sessions; view-as-role; operator status; Discord import authorisation,
SSRF pinning and the never-mapped Administrator bit; notification settings;
auto-delete; search SQL (all user input travels as parameters); and no new
`v-html` sinks.

### 7.4 Fuzzing and malformed-input review of the media paths (1.2, work package L)

* **Go fuzz targets** (seeded, `make fuzz-smoke`, each also run for 60 s locally
  without a finding): search query builder (never panics; the SQL text depends
  only on the query's shape, never on user bytes; LIKE wildcards always
  escaped), search time parser and window check, mention scanner, moderation
  duration shorthand, SSRF URL validation (a literal refused address is never
  accepted) and allow-list parser, release version parser, automod host and
  domain normalisation (IDNA, userinfo, ports, trailing dots), fail-closed link
  rules and word matching, Discord template link parsing and plan building
  (never panics, never maps Administrator or an owner-only power, only creates,
  deterministic hash), the search day and time-zone parser, status expiry and
  the slow-mode limit. The link-rule target reproduced finding M1 before its
  fix.
* **Web property tests** (fixed seed): random, truncated, extended, bit-flipped
  and old-format media units, capability and heartbeat messages and MLS
  handshake wires are refused without throwing.
* **Finding W1 (fixed).** `decodeMlsMessage` throws on some malformed input and
  `MediaGroup.receive` did not catch it, so a bad handshake wire rejected the
  call's inbox drain and skipped the sync after it. Any member (or the relay)
  could make a call's key exchange stall by repeating this. Availability only; no
  confidentiality impact. Fixed with a regression test.
* **Review of MLS, rotation and signature-domain paths** (read-only, by the
  maintainers; no crypto change made):

| ID | Result |
|---|---|
| R1 | Signature domains are distinct (`frame-v2`, `frame-v3`, `heartbeat-v1`, `capability-v1`, `safety-v1`), none is a prefix of another, and the version magic byte is inside the signed bytes. No cross-protocol or cross-version signature confusion found. A version 2 and a version 3 AAD can coincide byte for byte for suitable clear bytes, but the signature domain and the signed magic keep the units apart. |
| R2 | Heartbeat signed bytes are `domain \|\| groupId \|\| userId \|\| body` without length prefixes. Safe today because the group id is 16 bytes and a user id is validated to a fixed 36-character form; a future variable-length field would need prefixes. Recommended hardening, no current exploit. |
| R3 | Downgrade: while version 2 existed, a server withholding capability messages could keep an epoch on version 2 (no line binding). Version 2 is no longer opened in 1.2 (such units are only counted); no `acceptV2` path remains in the frame code. |
| R4 | A removed member's frames under the previous epoch still open at receivers for up to 30 seconds (documented; trade-off for media continuity). The removed member also keeps the old key; the server must stop forwarding. |
| R5 | An unauthenticated "commit for a newer epoch" from one of the two lowest members makes a receiver start over (rate limited with backoff). A server can do this anyway; availability only. Already listed as RT-6. |
| R6 | Rival-welcome handling can pull a member out of a group into one built on a key package it offered; rate limited per sender with doubling backoff; requires holding that key package (the relay can). Documented. |
| R7 | The relay-asserted sender id of a handshake wire is not itself authenticated. Commits and welcomes are MLS-signed; checks bind the signer leaf to the claimed sender for commits. For welcomes the code accepts the group if the claimed sender is the expected committer and the welcome decrypts under the offered key package; it does not additionally require the welcome's signer to equal the claimed sender. Auditors should confirm this cannot admit a member into a group the claimed committer did not create (the group's own signatures cover this inside `ts-mls`; worth a targeted test). |
| R8 | AES-GCM with random 96-bit nonces under a per-epoch key shared by all members stays below the NIST 2^32 limit by a wide margin at the sizes documented in SECURITY.md; this relies on rotation, which needs at least two members (a lone sender keeps its key until someone joins; nobody else holds it). |
| R9 | Heartbeats and capability messages are bound to the group and user id; a captured heartbeat can only be replayed if its time exceeds the last accepted one, so withholding and later delivering the newest heartbeat can delay a removal by the withheld interval. No privilege gain. |
| R10 | Key lifetime hooks (`__MNEMA_MEDIA_TIMING__`) are compiled out of release builds (`__MNEMA_E2E_HOOKS__` is false), so a page cannot shorten lifetimes in production. |

## 8. Known limitations and accepted risks

* **Text is server-readable.** By design and final. Do not report as a finding.
* **Active or compromised server versus media E2EE** (section 4.4): can insert a member, swap a leaf, split a call, drop messages, see metadata. The safety code is the only in-product detection. Browsers receive their code from the server, so a malicious server can serve a modified app.
* **Desktop client:** removes the server-served-code risk by running a signed bundle, but installers are unsigned at OS level (no Authenticode, notarisation or package signature); the bundle and updater signing keys are the trust anchors; Beta status.
* **Frame metadata:** codec headers (H.264 parameter sets and slice headers, VP8 payload header) are readable but authenticated; sizes and timing are visible to the SFU.
* **Replay by an active SFU:** a layer stream a receiver has not seen yet starts a fresh replay window, so an active SFU can feed old recorded frames of that layer.
* **Rate limiter and lockout state is in memory** and cleared on restart.
* **Regroup nuisance (RT-6):** two colluding members or the server can make a quiet listener regroup; backoff-limited.
* **Audit log keeps deleted and edited message text** for moderation; operators must name it in their privacy notice. Direct messages are not audited.
* **Attachments keep metadata** (stored as uploaded); only avatars, banners and emoji are re-encoded.
* **Self-update sidecar:** the Docker socket is root on the host; the sidecar is off by default and described in [SECURITY.md](../../SECURITY.md#self-update-sidecar).
* **No external audit, no bug bounty.** Reporting: [SECURITY.md](../../SECURITY.md#reporting-a-vulnerability).

## 9. Building and running a test instance

Use a throwaway machine. Please do not test against instances you do not run.

```sh
git clone https://github.com/msch128/mnema-talk && cd mnema-talk
git checkout release/1.2.0            # or the 1.2.0 tag once it exists
cp .env.example .env
# Set at least: PUBLIC_URL (for example http://127.0.0.1:8080), JWT_SECRET (32+ random bytes),
# POSTGRES_PASSWORD, S3_ACCESS_KEY, S3_SECRET_KEY, ADMIN_INITIAL_PASSWORD.
# For a local plain-HTTP test use APP_ENV=development (listens on loopback only);
# set API_DOCS_ENABLED=true to get /api/docs and /api/openapi.json,
# and METRICS_TOKEN to get /api/metrics.
make docker                           # builds mnema-talk:local
make up                               # docker compose up -d (app, postgres, seaweedfs; coturn and the update sidecars are optional)
curl -s http://127.0.0.1:8080/api/health
```

Sign in as the Owner (`Admin` by default, password from
`ADMIN_INITIAL_PASSWORD`), create invite codes and register more accounts to test
roles and cross-account cases. A published image can be used instead of building
(`MNEMA_IMAGE=ghcr.io/msch128/mnema-talk:<version>`); [deployment](../deployment.md#fresh-install)
lists the steps CI runs. For development without the image: `make dev` starts
PostgreSQL and SeaweedFS in Docker and runs the server with `go run`; the web
app builds with `make web` (or the Vite dev server in `web/`).

Useful commands: `make test` (unit), `make test-integration` (needs Docker or
`TEST_DATABASE_URL`), `make test-web`, `make fuzz-smoke` (`FUZZTIME=60s` for a
longer run), `make lint`, `make vuln`, `make openapi-check`. Call and camera
end-to-end specs run in CI only (they need media devices); they are not required
to review the code.

The mail path needs an SMTP server (`SMTP_HOST`); without it no mail is sent.
Web Push needs a browser and HTTPS. Roles and overwrites for permission tests are set up in the admin console
after the first sign-in.

## 10. Suggested test focus

Ordered by where a mistake would cost most.

1. **Permission enforcement parity.** For each new 1.2 feature (Talks and leaders,
   Stages, events on hidden Talks, forum posts, reminders, scheduled send,
   sessions, view-as-role), check the same decision is made in REST, the
   WebSocket hub (events must not reach members who cannot view), search, push,
   export and the SFU. Try IDOR on every `{id}` route with an account that lacks
   view access, and compare 403 versus 404 answers.
2. **Direct-message isolation.** Owner and Administrators must not read or learn
   existence of conversations they are not in; check search, mentions, unread
   counts, sequence numbers, push, reports, export and webhooks/bots.
3. **Media E2EE.** Frame parser and replay window (`web/src/lib/media/frame.ts`,
   `replay.ts`, `mediaFrameWorker.ts`); MLS commit vetting
   (`groupVet.ts`, `group.ts`, `groupTree.ts`); confusion between handshake
   message types; behaviour with hostile members (second leaf, forged
   remove/add, early removal), reloads and races; signature domains (R1, R2);
   welcome sender binding (R7). The tests under `web/src/lib/media/*.test.ts`
   are a map of what is already pinned.
4. **Authentication.** Sign-in lockout and the known-device cookie, TOTP
   brute-force budget, reset and confirmation tokens, session revocation
   paths (password change, log out everywhere, admin revoke, inactivity), bot token binding to the issuer.
5. **Injection and rendering.** Markdown renderer and sanitiser, the CSP
   (every admin panel), filenames and Content-Disposition, link-card and GIF
   proxy handling, search filter parsing, automod normalisation (L2, M1),
   slash-command argument parsing, Discord template import (additive, no
   Administrator bit, SSRF pinning).
6. **SSRF and outbound requests.** Webhook URLs, link previews, push
   endpoints, Discord template fetch; DNS rebinding, IPv4-mapped IPv6, redirect
   handling, alternative IP notations.
7. **Uploads and storage.** Content sniffing versus served type, image
   decoders (avatar, banner, emoji, event covers: bomb limits), quotas, path
   handling, download authorisation after a permission change.
8. **Desktop trust chain.** Bundle manifest verification, security floor and
   rollback protection, proxy cookie handling, the Tauri updater, revocation
   statements ([desktop trust model](../desktop-trusted-client.md)); the shell
   itself is in the separate repository.
9. **Supply chain and release.** Workflow permissions and pinning, image
   signing and provenance verification in the update sidecar, compose defaults
   (published ports, network separation).
10. **Data lifecycle.** Account erasure and export completeness for every new
    column and table (M3, L10), retention jobs, auto-delete, audit scrubbing.
