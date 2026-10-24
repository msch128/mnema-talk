# Operations

Running Mnema Talk with `docker compose`: configuration, reverse proxy, TURN,
storage, backups, monitoring and troubleshooting. For the update procedure
see [upgrade.md](upgrade.md); for the security model see
[SECURITY.md](../SECURITY.md).

## Configuration reference

All settings are environment variables, read and validated in
[`internal/config`](../internal/config/). In the compose
stack they come from `.env`; [`.env.example`](../.env.example) lists all of
them with placeholders.

General rules:

- An empty value counts as unset: the default applies.
- Booleans accept `true`/`false` and `1`/`0` (Go `strconv.ParseBool`);
  anything else stops startup.
- In `production` and `staging`, every public example value from
  `.env.example` (and any value starting with `replace_with_`) is refused as
  a secret.
- [`docker-compose.yml`](../docker-compose.yml) passes each variable to the
  app explicitly. A variable missing from its `environment:` list (for
  example `BIND_ADDR`) does not reach the container; some values are fixed
  there (see "Compose" below).

### Server

| Variable | Default | Meaning and validation |
|---|---|---|
| `APP_ENV` | `development` (compose: `production`) | `production`, `staging`, `development` or `test`; `prod`/`dev` are aliases. `production`/`staging` enforce strong secrets, enable HSTS and log at `info`. |
| `PUBLIC_URL` | `http://localhost:8080` | Absolute `http(s)` URL browsers use. `https://` turns on `Secure` and `__Host-` cookies. Trailing slash and default ports are dropped so it matches the browser's `Origin`. |
| `CORS_ALLOWED_ORIGINS` | `PUBLIC_URL` | Comma-separated origins allowed for API and WebSocket requests (CSRF origin check). Add `http://localhost:3000` for the Vite dev server. |
| `TRUSTED_PROXY_CIDRS` | `127.0.0.0/8,::1/128` (compose: the same) | CIDRs or IPs whose `X-Forwarded-For` is trusted. Each entry must parse. See [Reverse proxy](#reverse-proxy-and-trusted_proxy_cidrs). |
| `PORT` | `8080` | HTTP port, 1–65535. In compose the container always listens on 8080; `PORT` there is the **host** port of the mapping. |
| `BIND_ADDR` | `127.0.0.1` in development, else `0.0.0.0` | Listen address (outside compose). `APP_ENV=development` refuses any non-loopback address. |
| `PUBLISH_ADDR` | `127.0.0.1` | Compose only: host address the app port is published on. |
| `LOG_LEVEL` | empty | `debug`, `info`, `warn` or `error`. Empty = `info` in production/staging, `debug` otherwise. `debug` adds voice connection details (offered addresses, browser diagnostics) and turns on Pion's ICE log unless `PION_LOG_DEBUG` is set. |

### Sessions and admin

| Variable | Default | Meaning and validation |
|---|---|---|
| `JWT_SECRET` | none | Signs session cookies. In production/staging at least 32 characters and not a placeholder (`openssl rand -hex 32`). In development an empty value means a random per-process secret (sessions reset on restart). Changing it logs everyone out. |
| `SESSION_EXPIRY_HOURS` | `720` | Session lifetime, 1–8760. An upper bound: a session that is not used for the inactivity limit (see "Sessions and inactivity sign-out") ends earlier. |
| `INITIAL_LANGUAGE` | `de` | `de` or `en`: the language of the default categories and channels ("Text channels", "Voice hangouts", #general, #media, Lounge, Gaming 4K) on a brand-new installation. Used only when the database is still untouched (one account, no messages, the seeded names); an existing installation is never renamed. Any other value is rejected at startup. |
| `ADMIN_USERNAME` | `Herzog` | The Owner account (all-powerful, cannot be restricted by anyone; see [roles-and-permissions.md](roles-and-permissions.md)), created on first start when none exists. |
| `ADMIN_INITIAL_PASSWORD` | empty | Only used for that first start, and required then in production. In development, empty = a random password is generated and logged **once**. A placeholder value is rejected. |

### Database

| Variable | Default | Meaning and validation |
|---|---|---|
| `DATABASE_URL` | none (required) | PostgreSQL connection URL. Compose builds it from `POSTGRES_*` and the `postgres` service. In production a placeholder password in it is rejected. |
| `POSTGRES_USER` / `POSTGRES_PASSWORD` / `POSTGRES_DB` | `mnema` / none / `mnema_talk` | Compose only: initialise the `postgres` container and build `DATABASE_URL`. Changing them later does not change an existing database. |
| `POSTGRES_HOST_PORT` | `5432` | Dev only: loopback port published by `docker-compose.dev.yml`. |
| `TEST_DATABASE_URL` | none | Integration tests only: use this database instead of starting one with Docker. |

### Object storage (S3)

| Variable | Default | Meaning and validation |
|---|---|---|
| `S3_ENDPOINT` | `http://localhost:8333` | S3 API endpoint. Compose fixes it to `http://seaweedfs:8333`. |
| `S3_REGION` | `us-east-1` | Region for request signing. |
| `S3_BUCKET` | `mnema-media` | Bucket; created at startup if missing. |
| `S3_ACCESS_KEY` / `S3_SECRET_KEY` | none (compose: access key `mnema_admin`) | Required in production/staging, no placeholders. With SeaweedFS also the gateway's only identity, see [SeaweedFS S3 authentication](#seaweedfs-s3-authentication). |
| `S3_FORCE_PATH_STYLE` | `true` | Path-style bucket URLs (needed for SeaweedFS/MinIO). Compose fixes it to `true`. |
| `S3_HOST_PORT` | `8333` | Dev only: loopback port published by `docker-compose.dev.yml`. |

### Media and features

| Variable | Default | Meaning and validation |
|---|---|---|
| `MAX_UPLOAD_SIZE_MB` | `50` | The hard ceiling for a single upload, 1–2048, and the initial limit. The Owner can lower it in the System tab and per role (see [roles](roles-and-permissions.md#upload-size)), never raise it above this value. Your reverse proxy must allow at least this body size. |
| `UPLOAD_QUOTA_MB_PER_DAY` | `2048` | What one account (member, bot or webhook) may upload in any rolling 24 hours; further uploads answer 429. Deleted files still count. `0` turns the budget off. |
| `MEDIA_MAX_TOTAL_MB` | `0` | Pauses all uploads (503) once the stored media reaches this size. `0` = no cap. |
| `MEDIA_MIN_FREE_MB` / `MEDIA_DISK_PATH` | `1024` / empty | Pauses uploads (503) while the filesystem at `MEDIA_DISK_PATH` has less than this much free. The app can only see disks it has mounted: with the bundled SeaweedFS container, mount its data volume read-only into the app and point `MEDIA_DISK_PATH` at it, or watch the volume from outside. Empty path = off. |
| `EMOJI_MAX` | `200` | How many custom emoji the community may hold, 0-2000. `0` allows no new ones; existing ones keep working. Each is at most 256 KiB in object storage (`emoji/`). |
| `MEDIA_RETENTION_DAYS` | `0` | `0` = never delete media automatically. A positive number deletes media older than that many days. Negative values are rejected. |
| `EDIT_HISTORY_RETENTION_DAYS` | `0` | `0` = keep the earlier versions of edited messages (at most 50 per message; members with Manage messages can read them). A positive number deletes versions replaced longer ago than that many days. Negative values are rejected. |
| `AUDIT_RETENTION_DAYS` | `0` | `0` = keep the audit log forever. A positive number deletes entries older than that many days (checked every six hours); see [moderation](moderation.md#immutability-and-retention). Negative values are rejected. |
| `LINK_PREVIEWS_ENABLED` | `true` | The server fetches public pages for link cards (never private addresses); linked sites see the server's IP. |
| `PUSH_ENABLED` | `true` | Web Push notifications for members' devices while Mnema Talk is closed ([Web Push](#web-push-and-the-installable-app)). `false` turns them off: no key, no subscriptions API, nothing sent. |
| `PUSH_PREVIEWS` | `user` | `user`: each member chooses whether a push carries the start of the message (default on); `off`: pushes never carry message text. Any other value is rejected at startup. |
| `PUSH_VAPID_SUBJECT` | empty | Contact in the VAPID header (RFC 8292): an `https://` URL or a `mailto:` address. Empty uses `PUBLIC_URL` when that is `https://`, else a placeholder address (fine for testing; browsers' push services accept it). |
| `WEBHOOK_ALLOW_PRIVATE_TARGETS` | empty | Comma-separated addresses or CIDR ranges (for example `192.168.1.20,10.0.0.0/24`) that outgoing webhooks may contact besides public addresses. Empty = public addresses only. Cloud metadata and reserved ranges stay closed. Anyone who may manage webhooks (the Owner and Administrators) can then make the server send requests to those addresses, so list only what you need. Web Push and link previews ignore it. |
| `API_DOCS_ENABLED` | `false` | Swagger UI at `/api/docs` and `/api/openapi.json` for signed-in members. |

### Voice (WebRTC)

| Variable | Default | Meaning and validation |
|---|---|---|
| `WEBRTC_UDP_PORT_MIN` / `WEBRTC_UDP_PORT_MAX` | `50000` / `50050` | UDP range of the SFU; `1 <= MIN <= MAX`. Compose publishes the same range; forward it in your router. |
| `WEBRTC_UDP_MUX_PORT` | `WEBRTC_UDP_PORT_MIN` | The one UDP port all media peers share (like LiveKit or Discord voice servers), separately bound on each local IPv4/IPv6 interface. Must be inside `MIN`–`MAX`, which Compose already publishes. `0` = legacy mode: every peer takes its own port(s) from the range, so the default 51 ports fit only about 25 people in calls at once (logged at startup). Requires restart. |
| `WEBRTC_NAT_1TO1_IP` | empty | Comma-separated IPs or host names announced to browsers. Behind a home router list the public address (or a dynamic-DNS name, re-resolved every 5 minutes) **and** the server's LAN IP. In production, an empty value with no TURN configured logs a warning at startup. |
| `WEBRTC_STUN_URLS` | empty | Optional STUN servers. Empty = no third-party STUN (named in the privacy policy when set). |
| `WEBRTC_TURN_URLS` | empty | Optional TURN URLs, e.g. `turn:turn.example.com:3478?transport=udp,turn:turn.example.com:3478?transport=tcp`. |
| `WEBRTC_TURN_SECRET` | empty | Shared secret with coturn (`use-auth-secret`). At least 16 characters when `WEBRTC_TURN_URLS` is set; no placeholder in production. |
| `WEBRTC_TURN_REALM` | compose: `turn.example.com` | Read by the coturn container only, not by the app. |

### Outgoing e-mail

Optional. Without `SMTP_HOST` the server sends no e-mail and nothing else
changes. With it, members can confirm an e-mail address and reset a forgotten
password by mail; the Owner sees the setup under Admin > System and can send a
test mail there. The settings live in the environment only, never in the
database or its backups.

| Variable | Default | Meaning and validation |
|---|---|---|
| `SMTP_HOST` | empty | The mail server. Empty = no mail. |
| `SMTP_PORT` | `587` (`465` with `SMTP_TLS=tls`) | Port of the mail server. |
| `SMTP_TLS` | `starttls` | `starttls` (upgrade is required; the server refuses to send in the clear when it is not offered), `tls` (implicit TLS) or `none` (no encryption: refused in production and together with credentials). Certificates are verified. |
| `SMTP_USERNAME`, `SMTP_PASSWORD` | empty | Sign-in to the mail server (PLAIN, only over TLS). Set both or neither. |
| `SMTP_PASSWORD_FILE` | empty | Reads the password from this file (first line) instead of `SMTP_PASSWORD`; mount it into the container. Setting both is an error. |
| `SMTP_FROM` | empty | Sender address, e.g. `Mnema Talk <noreply@example.org>`. Required with `SMTP_HOST`. |

Check the setup with Admin > System > "Send test mail" (Owner, at most five per
ten minutes, audited without the recipient). A failed test names the stage
(`connect`, `tls`, `auth`, `sender`, `recipient`, `data`); server replies and
credentials are never shown. The links in the mails point to `PUBLIC_URL`.
Mails are plain text, in the member's language (German or English).
Every mail to a member (address confirmation, password reset, second-factor
alert) picks its language by the same rule: the language stored on the account
(the UI language, `de` or `en`); if the account has none yet, the first shipped
language in the `Accept-Language` header of the request that triggered the mail;
otherwise English, the instance default. The alert mail has no member request
behind it and so uses the stored language or the default.

### Monitoring

| Variable | Default | Meaning and validation |
|---|---|---|
| `METRICS_TOKEN` | empty | Enables `GET /api/metrics` with `Authorization: Bearer <token>`; empty = 404. At least 24 characters when set. |

### Updates

| Variable | Default | Meaning and validation |
|---|---|---|
| `UPDATE_CHECK_ENABLED` | `true` | Ask GitHub every 30 minutes for the latest release (admin System tab). `false` = no outbound request. |
| `MNEMA_IMAGE` | compose: `mnema-talk:local` | Image the `app` service runs; the app reads it to tell whether a re-pull can reach the latest release. See the tag table in the [deployment guide](deployment.md#updates). |
| `UPDATER_TOKEN` | empty | Turns on "Update now" through the updater sidecar (profile `autoupdate`). At least 32 characters of letters, digits and `._~+/=-`, no placeholder. Empty = feature off, `UPDATER_URL` ignored. |
| `UPDATER_URL` | `http://mnema-updater:8080` | Where the app reaches the sidecar: `http(s)` without credentials, query or fragment. |
| `UPDATER_IMAGE_REPOSITORY`, `UPDATER_CERT_IDENTITY_REGEXP`, `UPDATER_CERT_ISSUER`, `UPDATER_SOURCE_URL`, `UPDATER_TRUSTED_ROOT` | empty = this project's release images and workflow | Read by the updater sidecar only, for a fork that publishes its own signed images: the image repository it may pull from, an anchored (`^...$`) pattern for the signing workflow's identity, its OIDC issuer, the repository URL recorded in the build provenance and (optional) a mounted `trusted_root.json`. See [verifying a release image](#verifying-a-release-image). |

### Legal and privacy page

| Variable | Default |
|---|---|
| `LEGAL_OPERATOR_NAME` | `Community Operator` |
| `LEGAL_OPERATOR_EMAIL` | `admin@example.com` |
| `LEGAL_OPERATOR_COUNTRY` | empty |
| `LEGAL_PROJECT_NOTICE` | empty |

Shown in the privacy policy (`GET /api/legal`). Set your real details in
`.env` only. While the country or the project notice is empty, the legal page
shows a default in the member's language ("Germany" and "Private,
non-commercial project" in English). Before 1.1 the defaults were German
texts for both.

### Container resources (compose only)

Read by `docker-compose.yml`, not by the app. All services also rotate their
logs at 3 x 10 MB.

| Variable | Default | Meaning |
|---|---|---|
| `APP_MEM_LIMIT` | `1g` | Memory cap of the app container, including its RAM-backed `/tmp`. Uploads stream to S3 in 5 MB parts, so each upload in flight needs about 5 MB, whatever `MAX_UPLOAD_SIZE_MB` is. |
| `APP_TMP_SIZE` | `64m` | Size of the app's `/tmp` tmpfs (scratch only; uploads no longer pass through it). |
| `POSTGRES_MEM_LIMIT` | `1g` | Memory cap of PostgreSQL. |
| `SEAWEEDFS_MEM_LIMIT` | `1g` | Memory cap of SeaweedFS. |

### Script variables

| Script | Variables |
|---|---|
| [`scripts/backup.sh`](../scripts/backup.sh) | `BACKUP_DIR` (default `$HOME/mnema-talk-backups`), `KEEP_DAYS` (14), `BACKUP_ALLOW_IN_REPO=1` |
| [`scripts/health-check.sh`](../scripts/health-check.sh) | `MNEMA_URL` (default `http://127.0.0.1:8080`), `HEALTH_TIMEOUT` (5 s), `ALERT_WEBHOOK_URL` |

## Reverse proxy and `TRUSTED_PROXY_CIDRS`

Terminate TLS in a reverse proxy and forward HTTP and WebSocket traffic
(`/api/ws`) to the app's port. Voice and video do **not** go through the
proxy; they use the UDP range directly.

Caddy (passes `X-Forwarded-For` and WebSocket upgrades by default):

```caddy
chat.example.com {
    reverse_proxy app-host:8080
}
```

nginx:

```nginx
map $http_upgrade $connection_upgrade {
    default upgrade;
    ''      close;
}

server {
    listen 443 ssl;
    server_name chat.example.com;
    # ssl_certificate ...;

    client_max_body_size 60m;          # >= MAX_UPLOAD_SIZE_MB (the ceiling), plus framing

    location / {
        proxy_pass http://app-host:8080;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $connection_upgrade;
        proxy_read_timeout 300s;        # WebSocket stays open
    }
}
```

The app takes the client IP from `X-Forwarded-For` only when the request
comes from loopback or an entry in `TRUSTED_PROXY_CIDRS` (right-most
untrusted hop). Rate limits and the login lockout depend on that address.

- **App outside Docker, proxy on the same host**: the default (loopback) is
  enough.
- **Compose, proxy on the host or in a container**: requests arrive from the
  Docker bridge (gateway address for a proxy on the host, container IP for a
  proxy container), not from loopback. Set `TRUSTED_PROXY_CIDRS` to that one
  address. Without it X-Forwarded-For is ignored and the log says so.
- **Proxy on another LAN host or a custom Docker subnet**: set
  `TRUSTED_PROXY_CIDRS` to its address or network, e.g. `192.168.0.20`.
- Never list ranges untrusted clients can connect from, and don't expose port
  8080 directly while private ranges are trusted: anyone connecting from a
  trusted range could set their own `X-Forwarded-For`. The compose file
  publishes the app on `127.0.0.1` by default (`PUBLISH_ADDR`). Production
  and staging refuse to start when a trusted range is wider than an IPv4
  `/24` or an IPv6 `/64` and the listener is not loopback. Other environments
  still log that warning.

### `PUBLIC_URL` and Secure cookies

`PUBLIC_URL` must be the `https://` URL users open; otherwise the CSRF origin
check rejects their requests and cookies lose `Secure`. With an `http://`
`PUBLIC_URL` (including the `http://localhost:8080` default) the session cookie
is also sent without the `__Host-` prefix, while HSTS is still sent in
production, so someone on the network path of a first plain-HTTP visit can
read or replace the cookie. In production and staging the server therefore
logs a startup warning and shows "PUBLIC_URL is not https" under Admin >
System; it does not refuse to start, so a trusted-LAN install over `http://`
keeps working. Behind a TLS-terminating reverse proxy, set `PUBLIC_URL` to the
public `https://` address.

## Sessions and inactivity sign-out

Each browser sign-in is a session. Members see their own sessions under
*Account & security* > *Where you are signed in*: a coarse device label
("Firefox · Linux", parsed from the user agent; neither the raw user agent nor
an IP address is stored), when the session was created and last used (accurate
to a few minutes), whether its device gets push notifications, and which one is
this device. A member can sign one out, which also deletes its push devices, or
sign out all others. Only a member's own sessions are ever listed or ended
(`GET /api/auth/sessions`, `DELETE /api/auth/sessions/{id}`,
`POST /api/auth/sessions/revoke-others`). The admin action "end sessions" on a
member (`POST /api/admin/users/{id}/sessions/revoke`) is unchanged and ends all
of them.

A session that makes no authenticated request for more than the **inactivity
limit** is signed out. It is inactivity, not age: every request or page load
keeps the session alive, and `SESSION_EXPIRY_HOURS` still caps the total. The
Owner and Administrators set the limit under *Admin* > *Welcome & rules* (the community
settings; `session_inactivity_days` of `PUT /api/admin/onboarding`), 1 to 365
days, default 7. A change applies to running sessions at once.

- A request from an over-limit session is refused (401) immediately; a sweeper
  (every 10 minutes and at start) then ends it for good, closes its WebSockets
  and deletes its push devices. Rows of sessions that expired or were replaced
  by a password change are dropped at the same time.
- The last use is written at most every 5 minutes per session, so the check
  costs no write per request. Downtime counts: after a server that was off for
  longer than the limit starts, sessions idle for that long are signed out.
- Sessions that were issued before the 1.2 upgrade start their inactivity
  clock the first time they make a request afterwards. Nobody is signed out by
  the upgrade itself. A pre-upgrade session that has not made a request since
  is not listed yet, so "sign out all other sessions" does not reach it; use
  "Sign out everywhere" (or the admin action) to end those too.
- The desktop client's native sessions are separate and not affected.

## Web Push and the installable app

Mnema Talk is an installable web app (manifest, icons and a service worker at
`/sw.js`). Members can install it from the browser (Account menu > App &
notifications) and turn on push notifications per device, so they hear about
messages while the app is closed. On iPhone and iPad this works for the
installed app only (iOS 16.4 or newer: Share > Add to Home Screen).

- **Who is notified.** For each new message, every member with a subscribed
  device who may view the channel *at that moment* (the permission snapshot of
  the send, so a member who lost access stops at once), is not the author,
  has not blocked the author,
  is not on Do Not Disturb or Focus, and whose effective notification level
  for the channel allows it: all messages, or mentions (including `@all`,
  `@here`, roles and replies to their messages) only; mutes, timed mutes and
  category settings apply exactly as in the app (a mute still lets through
  a message addressed to the member by name, role or reply, not `@all` or
  `@here`; Do Not Disturb silences even those). The text of a push is plain: Markdown marks are removed,
  `||spoilers||` read "Spoiler" and custom emoji read `:name:`. An
  attachment-only message reads "Attachment", and a forwarded message with an
  empty comment reads "Forwarded message". A burst in one channel is
  merged: the first message goes out at once, the rest of the next five
  seconds as one push ("3 new messages"); a mention is never held back. A
  device that is offline gets only the newest push of a channel (push topic),
  mentions separately; a push expires after one hour (a day for mentions).
  A thread reply goes only to the thread's followers (the root's author,
  people who replied, people mentioned in the reply and anyone who followed it
  by hand), unless the member is addressed by name, role or reply.
  A message in a direct message conversation counts as a mention for push and
  is sent urgently; its push names the sender and never a channel.
- **What a push contains.** Channel name (for a direct message only the sender) and sender are always in it; the start
  of the message (140 characters) only if the member keeps "Show message text"
  on and `PUSH_PREVIEWS` is `user`. The payload is encrypted for the member's
  browser (RFC 8291, `aes128gcm`), so Google, Mozilla, Apple or Microsoft's push
  service sees only that something was sent, to which device, and its size.
  The page does not show its own notification while push is on and the tab is
  in the background, so nothing is shown twice.
- **The key.** The server generates a VAPID key pair (P-256) on first start
  and keeps it in the table `push_vapid`: subscriptions are bound to its public
  half, so it has to survive restarts and restores, and it travels with the
  database backup. It is never logged and no endpoint returns the private
  half. Whoever can read the database can read all messages anyway; the key
  adds nothing to that. If the key row is lost or replaced, all browsers keep
  their subscription but the push services refuse the server (403) and the
  subscriptions are removed after ten failures; members turn push on again.
- **Outgoing requests.** The server posts only to the address a browser
  registered for itself, and only after checking it twice: at registration it
  must be an `https://` URL on port 443 whose host is not a private, loopback,
  link-local or reserved address; at every send the host is resolved by the
  server itself and every address must be public (the guard outgoing webhooks
  use, with no allowlist: `WEBHOOK_ALLOW_PRIVATE_TARGETS` does not apply), the
  connection goes to the address it vetted, redirects are not followed and the
  answer is discarded. A member cannot use the server as a request proxy:
  the body is encrypted binary and no response text is ever shown.
- **Sessions.** A device belongs to the session that registered it. When that
  session is signed out, expires, or is revoked (password change or reset, "log
  out everywhere", admin revoke, disabled account), its device stops receiving
  pushes at once and is deleted within a minute. A session signed out for
  inactivity, or from "Where you are signed in", loses its devices in the same
  step.
- **Housekeeping.** A push service answering 404 or 410 removes the device at
  once; any other refusal or failure counts, and ten in a row remove it. An
  account has at most 10 devices. Deleting a member removes theirs. Signing out
  in the browser removes that browser's device first; registering the same
  browser under another account moves it.
- **Operations.** `mnema_push_deliveries_total{outcome}` counts sends (see the
  metrics table). Requests to push services need outbound HTTPS from the app
  container. With `PUSH_ENABLED=false` there is no key, no subscriptions API
  and no outgoing request. Behind a reverse proxy, pass `/sw.js` and
  `/manifest.webmanifest` through unchanged and do not cache `/sw.js`.

## TURN (coturn)

Needed only for members whose network blocks the UDP range (strict
firewalls, some VPNs, Opera with its VPN or WebRTC protection on).

1. In `.env`:
   ```sh
   WEBRTC_TURN_URLS=turn:turn.example.com:3478?transport=udp,turn:turn.example.com:3478?transport=tcp
   WEBRTC_TURN_SECRET=<openssl rand -hex 32>
   WEBRTC_TURN_REALM=turn.example.com
   ```
2. Start with the profile: `docker compose --profile turn up -d`.
3. Forward `3478/udp`, `3478/tcp` and `49152-49200/udp` to the host.

The coturn container runs with host networking and refuses to start with a
secret shorter than 16 characters. The app hands each signed-in client a
time-limited credential (valid 12 hours) from `GET /api/webrtc/config`; the
secret never leaves the server. The relay refuses private, loopback,
link-local and reserved peers (so it can't reach your LAN or Docker
networks) with one exception: a private IP listed in `WEBRTC_NAT_1TO1_IP`
(the voice server's LAN address) is allowed and used as relay address.
The compose file also caps the relay with coturn's `--user-quota=8` (relayed
sessions per credential) and `--total-quota=48` (all sessions; the port range
holds 49), so one member cannot occupy every relay port. If you widen the
range `49152-49200`, raise `--total-quota` with it. The credential's lifetime
(12 hours) is deliberately longer than a long call: coturn checks the expiry on
every allocation refresh and a running call cannot renew its credential, so a
shorter lifetime would cut relayed calls. The web app fetches a fresh
credential on a voice join when its copy is older than an hour. A credential
therefore outlives a sign-out for up to 12 hours but can only use the relay,
within the quotas above.
Configured TURN servers are listed in the privacy policy.

## SeaweedFS S3 authentication

The `seaweedfs` service writes an S3 identity file from `S3_ACCESS_KEY` /
`S3_SECRET_KEY` at every start and runs the gateway with it, so the gateway
accepts only those credentials (the same ones the app uses). Without an
identity file SeaweedFS would allow anonymous access to every bucket from
the Docker network.

- Allowed characters: letters, digits and `._~+/=-`; the secret needs at
  least 8 characters. Otherwise the container exits with an error in its log
  (`docker compose logs seaweedfs`).
- To rotate the keys, change both in `.env` and run `docker compose up -d`;
  app and gateway pick up the new pair together.
- PostgreSQL and SeaweedFS are not published by the production compose file;
  only `docker-compose.dev.yml` publishes them on `127.0.0.1` for local
  development.

## Backups and restore

Everything stateful is the PostgreSQL volume, the SeaweedFS volume and
deployment configuration. Run the scripts from the deployment directory (where
`docker-compose.yml` and `.env` are):

```sh
# Backup: database, media, env and compose.yaml into BACKUP_DIR/<timestamp>/
BACKUP_DIR=/srv/backups/mnema ./scripts/backup.sh

# Check integrity and SQL import in a disposable PostgreSQL container
./scripts/restore.sh /srv/backups/mnema/20261005-033000 --verify

# Restore data: stops writers, replaces database/media, resumes prior services
./scripts/restore.sh /srv/backups/mnema/20261005-033000          # asks first
./scripts/restore.sh /srv/backups/mnema/20261005-033000 --yes    # no prompt
```

- Backup stops the app (including retention) and then SeaweedFS before the
  database snapshot and volume copy. Active calls disconnect during this
  maintenance window. Other programs must not write to these stores during
  the backup. `SEAWEED_LIVE=1` is refused because it cannot guarantee this boundary.
- Success and failure resume only services that were running before backup.
  Restore resumes them after success; a failure after replacing data leaves
  app and SeaweedFS stopped to avoid serving a partial restore. Resolve the
  failure before restarting them. A failed SeaweedFS restart also keeps the app
  stopped. PostgreSQL must already be running.
- New backup directories contain a format manifest, image identities,
  migration filenames/checksums, `env`, rendered uninterpolated `compose.yaml`,
  SHA-256 checksums and a `COMPLETE` marker
  written after validation. Restore validates all required files, checksums
  and archives before stopping services or replacing data. Checksums detect
  damage; they do not authenticate an untrusted backup's SQL or configuration.
- A backup contains `.env` with all secrets; the script refuses a
  `BACKUP_DIR` inside a git checkout unless `BACKUP_ALLOW_IN_REPO=1`.
- The scripts serialize maintenance using `.mnema-maintenance.lock` in the
  deployment directory. An uncatchable kill may leave this lock; confirm no
  maintenance process is running before removing it manually.
- Retention removes only validated complete old backups after a successful
  backup and service restart. Incomplete/legacy directories require manual
  review and cleanup. Copy backups off the machine and encrypt offsite copies.
- `--verify` imports into a network-isolated disposable PostgreSQL 18 container
  with no live volumes or published ports, and removes it and its disposable
  database volume on success/failure. Allow disk space for the imported database.
  It checks SQL import and archive integrity. It does **not** prove login,
  attachment/avatar retrieval or device recovery: test those on a separately
  restored deployment with fresh volumes before relying on a restore point.
- Pre-manifest backups need explicit `--allow-legacy` alongside `--verify` or
  `--yes`; they still need readable SQL/media archives and `env`. Their original
  snapshot consistency and checksums cannot be retroactively established.
- Restore preserves the current `.env` and compose files. Review the backed-up
  `env` and `compose.yaml` manually:
  blindly replacing database credentials or storage endpoints can make the
  restored deployment inaccessible or target the wrong stores. Client content
  recovery remains separate from a server backup.
- Nightly via cron, see the backup section above.

## Verifying a release image

Release images on GHCR (`ghcr.io/msch128/mnema-talk`, every tag of a release)
are signed keylessly with [cosign](https://github.com/sigstore/cosign) by the
release workflow, using its GitHub OIDC identity; no long-lived key exists.
The image also carries BuildKit provenance and an SBOM. To check a pulled
image before running it:

```sh
cosign verify ghcr.io/msch128/mnema-talk:1.0 \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity-regexp '^https://github\.com/msch128/mnema-talk/\.github/workflows/release\.yml@'
```

A passing check shows that this repository's release workflow produced the
digest; it says nothing about the code in it beyond that. Pinning the digest in
`MNEMA_IMAGE` (`ghcr.io/msch128/mnema-talk@sha256:...`) keeps a verified image
from changing under a tag. Images released before signing was added carry no
signature.

**"Update now" runs this check for you** (since 1.1). The updater sidecar
resolves the tag to a digest, verifies the cosign signature against the
workflow identity above and the BuildKit provenance of every platform image,
checks that the signed release is the one you confirmed and newer than the
running one (an older signed image is refused, so a moved tag cannot roll the
app back), then pulls exactly that digest and restarts the app from it. If anything is
missing or does not match, nothing is pulled or restarted; the System tab
shows that the update was refused, and the updater's log (`docker compose logs
updater`) has the reason. Details and limits are in
[SECURITY.md](../SECURITY.md#self-update-sidecar).

- The updater needs outbound HTTPS to the image registry and to Sigstore's
  TUF mirror (`tuf-repo-cdn.sigstore.dev`); it has its own network for that. An
  installation that cannot reach the mirror can mount a `trusted_root.json`
  into the updater and set `UPDATER_TRUSTED_ROOT` to its path (the operator
  then keeps it current).
- The `autoupdate` profile has two services. The `updater` runs from the same
  image as the app (`MNEMA_IMAGE`) with another entrypoint; it verifies,
  pulls and moves the local tag. The `recreator` (Watchtower, no pulling, no
  route to the internet) only restarts the app from that local image. After
  moving to 1.1, run `docker compose --profile autoupdate up -d` once to
  start both. The sidecar's own settings (`UPDATER_LISTEN`,
  `UPDATER_DOCKER_SOCKET`, `UPDATER_SCOPE`, `UPDATER_RECREATOR_URL`,
  `UPDATER_TUF_CACHE`) are fixed by `docker-compose.yml`; leave them alone.
- Images released before signing was added carry no signature and are
  refused; update those by hand. A digest-pinned `MNEMA_IMAGE` is never
  touched by "Update now".
- To test the check yourself: pull a signed image, retag an unsigned one as
  the tag your app runs in a throwaway stack, and press "Update now"; the
  refusal and the log line show the gate working.

## Renewing the desktop trust statement

`desktop-trust/trust.json` is signed by the offline root key and valid for 180
days. After `expires_at` the desktop clients keep running their current bundle
but install no new bundle and no app update (security fixes included) until a
fresh statement exists. Nothing renews it automatically and the key never
leaves the owner's machine, so a scheduled workflow watches it:

- `.github/workflows/trust-expiry.yml` runs every Monday and fails while fewer
  than 60 days remain (`webbundle trust verify -min-valid-days 60`). A failed
  run is the alert: treat it as a task due within the next few weeks, not as a
  flaky check. Re-run it by hand with *Run workflow*.
- Check the same thing locally with
  `go run ./cmd/webbundle trust verify -min-valid-days 60`.

Runbook when it fails:

1. On the machine that holds the offline root key, from an up-to-date `main`:
   `go run ./cmd/webbundle trust sign -key-file <root key> -out desktop-trust
   -prev desktop-trust/trust.json -renew` (details and the confirmation prompt:
   [desktop-trusted-client.md](desktop-trusted-client.md#renewing-the-statement-every-3-months)).
   Read the printed list of allowed and revoked keys before confirming.
2. Open a pull request with only `desktop-trust/` and merge it.
3. Re-run the workflow; it must pass. Clients pick the new statement up at
   their next start or six-hourly check.
4. Add the next renewal date to your release checklist: the 60-day warning is a
   backstop, not a schedule.

Never put the root key in CI or in a GitHub secret; the workflow only reads the
committed statement and public key.

## Health and metrics

- **`GET /api/health`** (public): `200` with `status` (`ok`), `version`,
  `web_client_api` (the contract number the desktop client checks) and
  `features`, when the database answers and object storage is reachable,
  otherwise `503`
  (`database unavailable` / `file storage unavailable`). The result is cached
  for 2 seconds, so it can't be used to flood the database. `features` lists
  the optional features this build has, so a newer web app can hide what an
  older server lacks. In 1.1 it holds `blocks`, `dm`, `reports`, `saved`,
  `forward`, `search.filters.v2`, `threads.follow`, `slash.builtins`, `profile.banner` and `profile.avatar_remove`
  (1.2 adds `status.expiry`);
  names never change meaning ([api-stability.md](api-stability.md)). A 1.0.x
  server sends no list. The compose
  healthcheck and [`scripts/health-check.sh`](../scripts/health-check.sh)
  (cron, Unraid notification, optional webhook) use it.
- **`GET /api/metrics`** (Prometheus text format), only when `METRICS_TOKEN`
  is set. Every metric is listed under [Monitoring and
  alerts](#monitoring-and-alerts) below, with scrape config and alert rules.
- **Admin → System** shows version and commit, database and migration state,
  storage, voice/SFU and server resources, the update check and a live
  operator view (uptime, CPU, memory, traffic, errors, ICE failures, charts
  of the last hour). It reads `GET /api/admin/system/ops` (admin session
  only), not the bearer endpoint.

## Monitoring and alerts

### Scrape it

Set `METRICS_TOKEN` (24+ characters, `openssl rand -hex 24`) in `.env` and
restart. Without it the endpoint answers 404.

```yaml
# prometheus.yml
scrape_configs:
  - job_name: mnema-talk
    metrics_path: /api/metrics
    scheme: https
    scrape_interval: 15s
    authorization:
      credentials: <METRICS_TOKEN>      # or credentials_file: /etc/prometheus/mnema-token
    static_configs:
      - targets: ["chat.example.com"]
rule_files:
  - mnema-alerts.yml
```

Quick check by hand: `curl -H "Authorization: Bearer $METRICS_TOKEN" https://chat.example.com/api/metrics`.
Keep the token out of the reverse proxy's access logs and do not expose
`/api/metrics` to the internet more than needed: allow only your Prometheus
host at the proxy if you can. All series are bounded: labels are route
patterns, status classes, fixed reasons and states, never user or channel ids,
and each labelled family stops at 256 series.

### Minimal setup without Grafana

One Prometheus container is enough: it has an expression browser, graphs and
the alert list.

```yaml
# docker-compose.monitoring.yml (separate stack, start with docker compose -f ... up -d)
services:
  prometheus:
    image: prom/prometheus:latest   # pin a version or digest in real use
    command: ["--config.file=/etc/prometheus/prometheus.yml", "--storage.tsdb.retention.time=30d"]
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml:ro
      - ./mnema-alerts.yml:/etc/prometheus/mnema-alerts.yml:ro
      - ./mnema-token:/etc/prometheus/mnema-token:ro
      - prometheus-data:/prometheus
    ports: ["127.0.0.1:9090:9090"]
volumes:
  prometheus-data:
```

Use `credentials_file: /etc/prometheus/mnema-token` in the scrape config and
put the token in `./mnema-token` (mode 600). Open `http://127.0.0.1:9090/graph`
and `/alerts` over an SSH tunnel. To get notified, add Alertmanager or any
receiver that speaks its webhook format (ntfy, Gotify, email); without one,
[`scripts/health-check.sh`](../scripts/health-check.sh) from cron still covers
"is it up" with Unraid or webhook notifications.

### Example alert rules

The rules are in [`deploy/monitoring/mnema-alerts.yml`](../deploy/monitoring/mnema-alerts.yml)
(copy it next to `prometheus.yml` as `mnema-alerts.yml`). CI validates it with
`promtool check rules`, and a test fails when a rule names a metric that
`/api/metrics` does not export. Adjust the `MnemaHealthFailing` probe address and
the `MnemaMemoryHigh` threshold to your deployment. The rules:

| Alert | Fires when |
|---|---|
| `MnemaDown` | The scrape fails for 2 minutes (or the token is wrong). |
| `MnemaStorageDown` | Object storage is unreachable for 5 minutes. |
| `MnemaHealthFailing` | The blackbox probe of `/api/health` fails for 3 minutes. |
| `MnemaDatabasePoolExhausted` | Every database connection is busy for 5 minutes. |
| `MnemaMemoryHigh` | Resident memory stays above 800 MB (80% of the default limit) for 15 minutes. |
| `MnemaGoroutineLeak` | More than 5000 goroutines for 15 minutes. |
| `MnemaServerErrors` | More than one 5xx every 10 seconds for 10 minutes. |
| `MnemaSlowRequests` | The 95th percentile API latency (uploads and media excluded) is above one second for 15 minutes. |
| `MnemaIceFailures` | More than 20% of voice connections fail ICE over 30 minutes. |
| `MnemaStorageErrors` | More than 5 object storage errors in 15 minutes. |
| `MnemaBackupTooOld` | No successful backup for 36 hours (see Backup age). |

**Backup age.** The server does not know about your backups (they run on the
host), so it exports nothing for it. Let the backup job report instead, with
node_exporter's textfile collector (`--collector.textfile.directory=/var/lib/node_exporter`):

```sh
scripts/backup.sh && cat > /var/lib/node_exporter/mnema_backup.prom <<EOF
# TYPE mnema_backup_last_success_timestamp_seconds gauge
mnema_backup_last_success_timestamp_seconds $(date +%s)
EOF
```

### What each metric means

Counters only go up (use `rate()` or `increase()`); gauges are current values.

| Metric | Type | Meaning |
|---|---|---|
| `mnema_up` | gauge | Always 1 while the process answers. |
| `mnema_build_info{version,revision,go_version}` | gauge | Always 1; the labels identify the running build. |
| `process_start_time_seconds` | gauge | Process start, for uptime (`time() - ...`) and restart detection. |
| `process_cpu_seconds_total` | counter | User plus system CPU. `rate()` of it = cores in use. |
| `process_resident_memory_bytes` | gauge | Resident memory (Linux only; absent elsewhere). |
| `go_goroutines`, `go_memstats_alloc_bytes`, `go_memstats_sys_bytes`, `go_memstats_heap_inuse_bytes`, `go_memstats_heap_objects` | gauge | Go runtime: goroutines and heap. |
| `go_gc_cycles_total`, `go_gc_pause_seconds_total` | counter | Garbage collections and their total stop-the-world time. |
| `mnema_http_requests_total{route,class}` | counter | Requests by chi route pattern (`/api/channels/{channelID}/messages`) and status class `1xx`..`5xx`. Unknown paths are `unmatched`. |
| `mnema_http_request_duration_seconds{route}` | histogram | Latency per route pattern (WebSocket upgrades are counted but not timed). |
| `mnema_http_requests_in_flight` | gauge | Requests being served now. |
| `mnema_ratelimit_hits_total{limiter}` | counter | Requests refused with 429, by limiter: `api_user` (600 a minute per member), `upload`, `link_preview`, `poll_create` (10 a minute), `poll_vote`, `pin` (60 pins or unpins a minute), `emoji_upload` (10 per 10 minutes), `edit_history`, `export_download`, `dm_start` (new conversations), `block`, `report`, `saved`, `forward`, `thread_follow`, `slash_nick`, `test_mail`, the bot, webhook, gateway, push, e-mail and password-reset limiters, `ws_user` and `ws_ip`, and `other` for the few without a name. |
| `mnema_ws_online_users`, `mnema_ws_connections` | gauge | Distinct online users and open WebSockets. |
| `mnema_ws_connections_total` | counter | Accepted realtime connections (reconnect churn). |
| `mnema_ws_events_sent_total`, `mnema_ws_events_received_total` | counter | Event throughput to and from clients. |
| `mnema_ws_dropped_total{reason}` | counter | `slow_client` (connection cut because its buffer was full), `flood_frame`, `flood_event` (per-connection budget exceeded). |
| `mnema_mls_handshakes_total{result}` | counter | Voice-room encryption handshakes: `relayed`, `rejected` (malformed or wrong room), `rate_limited`. `mnema_mls_handshake_bytes_total` is the relayed size. |
| `mnema_voice_rooms`, `mnema_voice_participants` | gauge | Calls as members see them (including the reconnect grace period). |
| `mnema_sfu_rooms`, `mnema_sfu_peers` | gauge | Rooms with media and media connections. |
| `mnema_sfu_tracks{kind}` | gauge | Published tracks, `audio` and `video`. |
| `mnema_sfu_video_rooms`, `mnema_sfu_screen_shares`, `mnema_sfu_cameras` | gauge | Rooms with video and the screen shares and cameras forwarded. |
| `mnema_sfu_ingress_bytes_total`, `..._packets_total` | counter | RTP received from publishers. |
| `mnema_sfu_egress_bytes_total`, `..._packets_total` | counter | RTP sent to viewers (each viewer counts), retransmissions included. `rate(...)*8` = bits per second. |
| `mnema_sfu_nack_requests_total`, `mnema_sfu_nack_packets_total` | counter | Retransmission requests from viewers and the packets they asked for: a measure of packet loss on the downlinks. |
| `mnema_sfu_keyframe_requests_total` | counter | PLI and FIR feedback from viewers. |
| `mnema_sfu_layer_switches_total` | counter | Simulcast layer changes of screen share viewers. |
| `mnema_sfu_bandwidth_estimate_reports_total{range}` | counter | Bandwidth estimates taken for viewers whose screen layer the server chooses, by range (`lt0.5m` ... `ge16m`, bits per second); `rate()` per range shows how the downlinks are distributed. |
| `mnema_sfu_bandwidth_layer_moves_total` | counter | Moves to a lower layer caused by the bandwidth estimate (those caused by loss, round trip time or keyframe requests are only in `mnema_sfu_layer_switches_total`). |
| `mnema_sfu_ice_transitions_total{state}` | counter | `connected`, `failed`, `disconnected` ICE transitions. A rising `failed` share means clients cannot reach the UDP port or TURN. |
| `mnema_sfu_ingress_dropped_total{reason}` | counter | Publisher packets dropped over the ingress limit: `rate` (a sender above its budget) or `closed` (the peer was already closing). |
| `mnema_sfu_peer_joins_total` | counter | Media connections created. |
| `mnema_db_connections_total\|acquired\|idle\|max` | gauge | Database pool. `mnema_db_acquires_total`, `..._waited_total`, `..._canceled_total`, `mnema_db_acquire_seconds_total` count acquisitions, those that had to wait, those abandoned and the time spent. |
| `mnema_storage_up` | gauge | 1 when object storage answers (the same check as `/api/health`), else 0. |
| `mnema_s3_operations_total{op,result}` | counter | Object storage calls (`upload`, `get`, `delete`, `list`) by `ok`/`error`. |
| `mnema_webhook_events_dropped_total{reason}` | counter | Outgoing webhook events dropped or expired unsent: `webhook_cap` and `global_cap` (queue limits), `buffer_full`, `channel_share`, `store_error` and `expired` (older than the retry window). A rising count means a receiver is down or too slow. |
| `mnema_push_deliveries_total{outcome}` | counter | Web Push deliveries: `ok`, `gone` (the browser ended the subscription; it is removed), `retry` (temporary refusal, retried once), `rejected`, `error` (no answer: blocked address, DNS, connection, TLS, timeout) and `dropped` (a full queue). A rising `rejected` share right after a restore means the VAPID key changed. |

## Orphaned media

Objects in storage that no media row refers to (a failed delete, a crash
during an upload) are invisible to members and only take space. Once a day
the app counts those older than 24 hours under `uploads/`, `avatars/` and `banners/` and
logs a warning when it finds any; it never deletes them on its own.

```sh
# as admin (session cookie): count, then remove
GET  /api/admin/media/orphans           -> {"count": 2, "bytes": 150, "deleted": 0}
POST /api/admin/media/orphans/cleanup   -> {"count": 2, "bytes": 150, "deleted": 2}
```

Objects with a media row, objects younger than 24 hours and anything outside
those two prefixes are never touched.

## Logs

The app logs JSON lines to stdout:

```sh
docker compose logs -f app            # or: make logs
docker compose logs app | grep '"sfu'  # voice connection events
docker compose logs app | grep '"audit":"self_update"'
```

Compose rotates every container's log at 3 x 10 MB, so older lines drop off;
ship them elsewhere if you need a longer history.

Configuration problems the server notices (a broad `TRUSTED_PROXY_CIDRS` in
an environment that still starts, requests arriving through an untrusted
proxy, voice unreachable from outside
or without a shared UDP port, voice disabled) are logged and also shown at the
top of Admin > System, each with a short explanation and a link to this guide.
They are not part of the public `/api/health`.

At startup the app logs `webrtc announce` with the announced IPs and the
port range, or `webrtc sfu unavailable, voice disabled` when the SFU could
not start (e.g. the UDP range is in use).

## Troubleshooting

### Account deletion and data exports

Members delete their own account and download their data under Account &
security (the Owner account cannot be deleted). The export is a ZIP of JSON
files with the member's profile, settings, messages, earlier versions of
their edits, reactions, poll votes, uploads, the polls they started, the
messages they pinned, their push devices (label and dates, no keys), the
custom emoji they uploaded, the members they blocked, the reports they sent,
their saved messages (ids, notes and times, not the saved text), the threads
they follow, audit entries about their account and their sign-in setup;
nothing of other members. Deleting keeps an anonymised
row and the member's messages without text, removes everything else (see
[SECURITY.md](../SECURITY.md)) and frees the username. Their blocks (in both
directions), saved list and thread follows go; a direct message conversation
nobody else is in is deleted with its messages and files, and in the others
their messages stay without text; reports keep their copies for the
moderators with the member's text blanked; files are removed from
object storage in the same request, and anything that could not be removed is
found by Admin > Media > orphan cleanup. Exports are stored for at most 24 hours
under `exports/` in the bucket and need object storage; back-ups taken in that
window contain them, so a restore can bring back a deleted account's rows and
files: delete the account again after restoring an old back-up. Add this to your
back-up retention statement.

### The Owner lost the authenticator and the recovery codes

Whoever can run commands on the host can turn two-factor sign-in off for the
account (it asks for nothing more, because that person can already read the
database), end its sessions and leave an audit entry:

```
docker exec mnema-talk /usr/local/bin/mnema-talk reset-2fa OWNER_NAME
```

Other members' second factor is reset by
the Owner in Admin > Members. Rotating `JWT_SECRET` also makes every stored
authenticator secret unreadable: each affected account has to be reset and
enrol again, so rotate it only with that in mind.

### The Owner cannot sign in: "too many requests"

Repeated wrong passwords lock a client address (1, 5, then 30 minutes). Many
wrong guesses on one account from many addresses switch on an account-wide
cap: while it is on, browsers that signed in to the account before (they hold
a signed `mnema_dev_*` cookie, valid a year, independent of the IP) sign in
normally, and every other device together gets about one password check per
minute per client address, and about ten per minute for the whole account
(refused attempts are not charged to that shared ceiling, so one hostile
address cannot use it up alone). The cap lasts at most 15 minutes. All of this
state is held in memory; a restart is not needed to clear it:

- **From the host** (works even when nobody can sign in):

  ```sh
  docker exec mnema-talk /usr/local/bin/mnema-talk unlock USERNAME   # one account
  docker exec mnema-talk /usr/local/bin/mnema-talk unlock --all    # everybody
  ```

  The command runs in a second process, so it leaves a request in the
  database (table `login_unlock_requests`); the running server picks it up
  within a couple of seconds and forgets that account's lockouts (every client
  address, the account-wide cap, the new-device budgets and the
  password-change lockout). It prints `Login lockouts cleared ...` once the
  server applied it, or says the request stays queued when no server picked it
  up in 15 seconds. Only someone who can reach the database can do this; there
  is no network endpoint for it.
- **From the admin panel:** the Owner sees a "Sign-in locked" badge on a locked
  account in Admin > Members and can click "Clear sign-in lock".

Otherwise sign in from a browser you used before, or wait.

### Nobody can join voice, or it connects and drops after a few seconds

1. **UDP range forwarded?** `WEBRTC_UDP_PORT_MIN`–`MAX` (default
   `50000-50050/udp`) must be forwarded from the router to the host and
   published by Docker (compose does that). The reverse proxy does not carry
   voice.
2. **Announced address right?** Check the `webrtc announce` log line. Behind
   NAT set `WEBRTC_NAT_1TO1_IP` to the public IP or a dynamic-DNS name **and**
   the server's LAN IP, e.g. `myhome.example.org,192.168.0.10`. Members in the
   LAN need the LAN IP when the router has no hairpin NAT.
3. **Connection log.** `sfu ice connected` shows the chosen local and remote
   address and the remote candidate type; `sfu ice failed`/`disconnected` and
   `sfu peer state ... failed` mean packets don't get through (firewall,
   wrong announced address).
4. **More detail.** `LOG_LEVEL=debug` adds the candidates each browser offers
   and the browsers' own connection diagnostics (`webrtc client diag`).
   Revert to the default afterwards.
5. **UDP blocked on the member's side** (company network, VPN, Opera's VPN or
   WebRTC protection): only TURN over TCP helps, see [TURN](#turn-coturn).

### A screen share shows nothing

- Viewers receive a share only after clicking **Watch**.
- The sharer's browser decides the codec (H.264 preferred, hardware encoded).
  If a viewer's browser can't decode it, the stage stays black; try another
  browser on the viewer side.

### Login or every request fails with 403

`PUBLIC_URL` / `CORS_ALLOWED_ORIGINS` don't match the URL in the browser
(scheme, host and port must match; default ports are ignored).

### Everyone shares one rate limit or gets locked out together

The app sees the proxy's IP instead of the clients': add the proxy's address
to `TRUSTED_PROXY_CIDRS`.

### The app does not start

`docker compose logs app` shows the validation message, e.g. a placeholder
secret, `TRUSTED_PROXY_CIDRS contains an invalid entry`, or a migration
failure (see [upgrade.md](upgrade.md)).

## Downgrading

> **Do not roll back below this release once roles or private channels exist.**
> Migration 0017 adds the tables that restrict access. A binary that does not
> know them (0.7.x before roles, or older) would ignore them and show every
> private channel to everyone. This release and later ones refuse to start on a
> database migrated by a newer version (CI checks this against the 0.8.16 and
> 0.9.0 images), but binaries from before that guard
> existed cannot. To go back, restore a backup taken before the update.
