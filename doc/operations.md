# Operations

Running Mnema Talk with `docker compose`: configuration, reverse proxy, TURN,
storage, backups, monitoring and troubleshooting. For updating
see [updating.md](updating.md) and the per-version [upgrade.md](upgrade.md); new
to all this? Start with the [quick start](quick-start.md); for the security model see
[SECURITY.md](../SECURITY.md). Which ports to forward and which outbound
connections the server makes: [ports.md](ports.md).

## Settings at a glance

Everything is configured in `.env` next to `docker-compose.yml`; the
[setup assistant](quick-start.md#step-1-make-the-files) writes it. The server
checks the required settings at startup and, if any is missing or still a
placeholder, logs "Mnema Talk cannot start - fix these settings in .env:" with
the complete list and exits with status 78 (a refused configuration, not a
crash). `mnema-talk setup --check` prints the same list for the `.env` in a
folder before you start (`docker run --rm -v "$PWD:/out" ghcr.io/msch128/mnema-talk setup --check`).
A missing `ADMIN_INITIAL_PASSWORD` is reported separately, when the first start
tries to create the Owner.

**Required**

| Setting | What it is | Example | How to get it |
|---|---|---|---|
| `PUBLIC_URL` | The address members type into their browser | `https://chat.example.com` | You choose it (your domain) |
| `JWT_SECRET` | Secret that signs sessions, 32+ characters | (random) | `openssl rand -hex 32` |
| `POSTGRES_PASSWORD` | Database password | (random) | `openssl rand -hex 16` |
| `S3_ACCESS_KEY` | Media storage user name (compose falls back to `mnema_admin`) | `mnema` | Letters, digits and `. _ ~ + / = -` |
| `S3_SECRET_KEY` | Media storage password, 8+ characters, same allowed characters | (random) | `openssl rand -hex 16` |
| `ADMIN_INITIAL_PASSWORD` | The Owner's first password, 14+ characters; first start only | (random) | `openssl rand -base64 18`; delete the line after signing in |

**Usually worth setting**

| Setting | Why |
|---|---|
| `MNEMA_IMAGE` | Which released image to run (`:X.Y` or `:latest`; [updating](updating.md)) |
| `TRUSTED_PROXY_CIDRS` | Address of a reverse proxy in Docker or on another machine (the Status tab names it) |
| `WEBRTC_NAT_1TO1_IP` | Only if the server finds no public address itself, or LAN members need the LAN address too |
| `SMTP_*` | E-mail for password resets (optional) |
| `COMPOSE_PROFILES` | Optional parts: `autoupdate`, `turn` ([profiles](deployment.md#optional-parts-compose-profiles)) |

Everything else has a default; the next section lists every variable.

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
| `PUBLIC_URL` | `http://localhost:8080` | **The one address you must set**: the absolute `http(s)` URL browsers use. `https://` turns on `Secure` and `__Host-` cookies. Trailing slash and default ports are dropped so it matches the browser's `Origin`. Several defaults follow from it, see [what `PUBLIC_URL` sets](#public_url-and-secure-cookies). |
| `CORS_ALLOWED_ORIGINS` | `PUBLIC_URL` | Comma-separated origins allowed for API and WebSocket requests (CSRF origin check). Add `http://localhost:3000` for the Vite dev server. |
| `TRUSTED_PROXY_CIDRS` | `127.0.0.0/8,::1/128` (compose: the same) | CIDRs or IPs whose `X-Forwarded-For` is trusted. Each entry must parse. See [Reverse proxy](#reverse-proxy-and-trusted_proxy_cidrs). |
| `PORT` | `8080` | HTTP port, 1–65535. In compose the container always listens on 8080; `PORT` there is the **host** port of the mapping. |
| `BIND_ADDR` | `127.0.0.1` in development and test, else `0.0.0.0` | Listen address (outside compose). `APP_ENV=development` and `test` refuse any non-loopback address. |
| `PUBLISH_ADDR` | `127.0.0.1` | Compose only: host address the app port is published on. |
| `LOG_LEVEL` | empty | `debug`, `info`, `warn` or `error`. Empty = `info` in production/staging, `debug` otherwise. `debug` adds voice connection details (offered addresses, browser diagnostics) and turns on Pion's ICE log unless `PION_LOG_DEBUG` is set. |

### Sessions and admin

| Variable | Default | Meaning and validation |
|---|---|---|
| `JWT_SECRET` | none | Signs session cookies. In production/staging at least 32 characters and not a placeholder (`openssl rand -hex 32`). In development an empty value means a random per-process secret (sessions reset on restart). Changing it logs everyone out unless the old value goes into `JWT_SECRET_PREVIOUS`: see [Rotating keys](#rotating-keys). |
| `JWT_SECRET_PREVIOUS` | empty | The session secret you are replacing. Sessions and known-device cookies signed with it stay valid; nothing new is signed with it. Must differ from `JWT_SECRET`; in production/staging the same length and placeholder rule. |
| `DATA_KEY` | empty | Seals the authenticator secrets of two-factor sign-in and keys the recovery codes. Empty = they stay under keys derived from `JWT_SECRET`, as in every earlier release. When set: never a session secret (current or previous), and in production/staging at least 32 characters and not a placeholder (`openssl rand -hex 32`). **Once set it stays**: emptying it makes what it sealed unreadable. Keep it with your backups; the only way back is [Stop using a data key](#stop-using-a-data-key). |
| `DATA_KEY_PREVIOUS` | empty | The `DATA_KEY` you are replacing: what it sealed stays readable until `mnema-talk rotate-keys` has moved it. Must differ from `DATA_KEY` and follows the same rules. Without a `DATA_KEY` it is the way back to the keys derived from `JWT_SECRET`. |
| `SESSION_EXPIRY_HOURS` | `720` | Session lifetime, 1–8760. An upper bound: a session that is not used for the inactivity limit (see "Sessions and inactivity sign-out") ends earlier. |
| `INITIAL_LANGUAGE` | `en` | `de` or `en`: the language of the default categories and channels ("Text channels", "Voice hangouts", #general, #media, Lounge, Gaming 4K) on a brand-new installation. Used only when the database is still untouched (one account, no messages, the seeded names); an existing installation is never renamed. Any other value is rejected at startup. |
| `ADMIN_USERNAME` | `admin` | The Owner account (all-powerful, cannot be restricted by anyone; see [roles-and-permissions.md](roles-and-permissions.md)), created on first start when none exists. |
| `ADMIN_INITIAL_PASSWORD` | empty | Only used for that first start, and required then in production. In development, empty = a random password is generated and logged **once**. A placeholder value is rejected. |

### Database

| Variable | Default | Meaning and validation |
|---|---|---|
| `DATABASE_URL` | none (required) | PostgreSQL connection URL. Compose builds it from `POSTGRES_*` and the `postgres` service. In production a placeholder password in it is rejected. |
| `POSTGRES_USER` / `POSTGRES_PASSWORD` / `POSTGRES_DB` | `mnema` / none / `mnema_talk` | Compose only: initialise the `postgres` container and build `DATABASE_URL`. Changing them later does not change an existing database. |
| `DB_MAX_CONNS` | `25` | Connections the server keeps to PostgreSQL at most, 1-1000. Keep it below the database's `max_connections` (100 in the bundled `postgres`), with room for `pg_dump` and `psql`. Raise it only when `mnema_db_acquires_waited_total` keeps growing under normal load. |
| `DB_STATEMENT_TIMEOUT` | `15s` | Longest one database statement of a request may run, as a duration (`15s`, `2m`), 1s-1h; `0` = no limit. The database cancels a statement that runs longer and the request fails with an internal error, logged with `SQLSTATE 57014`; a message or forum search answers 429 instead (searches are limited to 5 s). See [Database limits](#database-limits). |
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
| `UPLOAD_QUOTA_MB_PER_DAY` | `2048` | What one account (member, bot or webhook) may upload in any rolling 24 hours; further uploads answer 429. Deleted files and messages still count: usage is booked per upload, apart from the files. `0` turns the budget off. |
| `MEDIA_MAX_TOTAL_MB` | `0` | Pauses all uploads (503) once the stored media reaches this size. `0` = no cap. |
| `MEDIA_MIN_FREE_MB` / `MEDIA_DISK_PATH` | `1024` / empty | Pauses uploads (503) while the filesystem at `MEDIA_DISK_PATH` has less than this much free. The app can only see disks it has mounted: with the bundled SeaweedFS container, mount its data volume read-only into the app and point `MEDIA_DISK_PATH` at it, or watch the volume from outside. Empty path = off. |
| `BACKUP_STATUS_FILE` | empty (compose: `/backup-status/status.json`) | The status file `scripts/backup.sh` writes after each backup, as the app sees it: an absolute path, anything else is rejected at startup. The bundled compose file mounts `./backup-status` there read-only. Admin → Status shows the age of the last backup and its restore test ([Backups and restore](#backups-and-restore)); empty = *not reported*. |
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
| `WEBRTC_NAT_1TO1_IP` | empty | Comma-separated IPs or host names announced to browsers. Behind a home router list the public address (or a dynamic-DNS name, re-resolved every 5 minutes) **and** the server's LAN IP. Empty in production: the server announces an IPv4 address from `PUBLIC_URL` and finds its public address itself (`WEBRTC_AUTO_ANNOUNCE`); when that fails and no TURN is configured it logs a warning at startup. A value set here is used as it is and never replaced. |
| `WEBRTC_LAN_IP` | empty | Comma-separated IPv4 addresses of this host in your own network (for example `192.168.1.20`), announced to browsers after the public address the server found itself (`WEBRTC_NAT_1TO1_IP` empty). It solves "voice works from outside but not from inside the home network" on routers without NAT loopback. A process inside a Docker bridge network sees only the container's address, so the server cannot read the host's LAN address there: `mnema-talk setup` asks for it, or set it by hand. A server that runs outside a container announces its own private address when this is empty. Ignored when `WEBRTC_NAT_1TO1_IP` is set (list the LAN address there). |
| `WEBRTC_PUBLISH_ADDR` | `0.0.0.0` | Compose only: the address Docker publishes the voice UDP range on. `127.0.0.1` keeps voice reachable from this computer only (what `mnema-talk setup --local` writes, together with `TURN_PUBLISH_ADDR=127.0.0.1`). |
| `WEBRTC_AUTO_ANNOUNCE` | `true` | Production only, and only with `WEBRTC_NAT_1TO1_IP` empty: find the public IPv4 address with one STUN binding request at startup (and every 10 minutes, to follow a changed address) and announce it, logging what was found. The STUN server sees the server's IP address, nothing else. `false` = no request. |
| `WEBRTC_DETECT_STUN_URLS` | `stun.l.google.com:19302,stun.cloudflare.com:3478` | STUN servers that detection asks, in order (`host:port` or `stun:host:port`). Not the STUN servers browsers use (`WEBRTC_STUN_URLS`). |
| `WEBRTC_STUN_URLS` | empty | Optional STUN servers. Empty = no third-party STUN (named in the privacy policy when set). |
| `WEBRTC_TURN_URLS` | empty | Optional TURN URLs, e.g. `turn:turn.example.com:3478?transport=udp,turn:turn.example.com:3478?transport=tcp`. |
| `WEBRTC_TURN_SECRET` | empty | Shared secret with coturn (`use-auth-secret`). At least 16 characters when `WEBRTC_TURN_URLS` is set; no placeholder in production. |
| `WEBRTC_TURN_REALM` | compose: `turn.example.com` | Read by the coturn container only, not by the app. |
| `WEBRTC_BUILTIN_TURN` | `auto` | The TURN relay built into the server: `auto` runs it only while `WEBRTC_TURN_URLS` is empty, `on` always, `off` never. See [Built-in TURN relay](#built-in-turn-relay). |
| `WEBRTC_BUILTIN_TURN_HOST` | empty | Host name or IP members are told to reach the built-in relay at; empty = the host of `PUBLIC_URL`. |
| `WEBRTC_BUILTIN_TURN_PORT` | `3479` | UDP and TCP port of the built-in relay. With `on` it must lie outside the media range. Compose publishes it on `TURN_PUBLISH_ADDR` (default all addresses). |
| `VOICE_UPLOAD_LIMIT_MBIT` | `0` | The upload your connection can carry for calls, in Mbit/s; `0` = no limit. Above 0 the server measures the media it sends (all of it, audio included, with about 38 bytes of IP, UDP and SRTP overhead per packet; members who go through the built-in TURN relay are counted once, as the media the server sends them, plus the relay's 4 bytes of framing per packet, which also means a relayed member costs the same as a direct one and squeezing cannot reduce that per-packet overhead, only the layers) and, above about 85% of the limit, moves viewers on **Auto** quality one layer further down their screen-share and camera layers, biggest streams first, until the estimated saving covers the excess; below about 70% it lets them back, the cheapest first, one viewer at a time and not more often than every 20 seconds (and not within 20 seconds of a step down). A viewer on a layer that sends next to nothing at the moment (a static screen share) stays held until its picture moves, which costs nothing. Audio is never touched. A viewer who picked a quality, and a share or camera sent without layers, are not moved: when that leaves nothing to step down, the server logs it (once a minute) and counts `mnema_sfu_upload_unmet_total`. Must be 0 or between 1 and 100000. Metrics: `mnema_sfu_upload_limit_bits`, `mnema_sfu_upload_egress_bits`, `mnema_sfu_upload_capped_viewers`. |

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
| `PPROF_ADDR` | empty | Serves Go pprof profiles (`/debug/pprof/`) on this `host:port`; empty = off. Only a loopback address is accepted (`127.0.0.1:6060`, `[::1]:6060`, `localhost:6060`); anything else stops the start. There is no authentication, so inside the container read it with `docker compose exec app wget -qO- http://127.0.0.1:6060/debug/pprof/heap`. |

### Updates

| Variable | Default | Meaning and validation |
|---|---|---|
| `UPDATE_CHECK_ENABLED` | `true` | Ask GitHub every 30 minutes for the latest release (admin System tab). `false` = no outbound request. |
| `CLOCK_CHECK_ENABLED` | `true` | Compare the server clock with an NTP server (`time.cloudflare.com`, then `pool.ntp.org`) at startup and every 6 hours, for the *Server clock* item of the setup checks. The NTP server sees the server's IP address. `false` = no request; the item is then left out. |
| `MNEMA_IMAGE` | compose: `mnema-talk:local` | Image the `app` service runs; the app reads it to tell whether a re-pull can reach the latest release. See the tag table in the [deployment guide](deployment.md#updates). |
| `UPDATER_TOKEN` | empty | Turns on "Update now" through the updater sidecar (profile `autoupdate`). At least 32 characters of letters, digits and `._~+/=-`, no placeholder. Empty = feature off, `UPDATER_URL` ignored. |
| `UPDATER_URL` | `http://mnema-updater:8080` | Where the app reaches the sidecar: `http(s)` without credentials, query or fragment. |
| `UPDATER_IMAGE_REPOSITORY`, `UPDATER_CERT_IDENTITY_REGEXP`, `UPDATER_CERT_ISSUER`, `UPDATER_SOURCE_URL`, `UPDATER_TRUSTED_ROOT` | empty = this project's release images (GHCR and the Docker Hub mirror) and workflow | Read by the updater sidecar only, for a fork that publishes its own signed images: the one image repository it may pull from (setting it drops the built-in Docker Hub mirror), an anchored (`^...$`) pattern for the signing workflow's identity, its OIDC issuer, the repository URL recorded in the build provenance and (optional) a mounted `trusted_root.json`. See [verifying a release image](#verifying-a-release-image). |

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
| [`scripts/backup.sh`](../scripts/backup.sh) | `BACKUP_DIR` (default `$HOME/mnema-talk-backups`), `KEEP_DAYS` (14), `BACKUP_ALLOW_IN_REPO=1`, `RESTORE_TEST` (1; 0 skips the restore test), `BACKUP_STATUS_DIR` (default `./backup-status`), `BACKUP_MODE` (`cold`; `online` keeps the app running), `BACKUP_LOCK_WAIT` (600 s, online mode), `BACKUP_AGE_RECIPIENT` (empty = not encrypted) |
| [`scripts/restore.sh`](../scripts/restore.sh) | `BACKUP_AGE_IDENTITY` (identity file for an encrypted backup), `SEAWEEDFS_VERIFY_IMAGE` (image for the media check of `--verify`, default: the one the backup names) |
| [`scripts/health-check.sh`](../scripts/health-check.sh) | `MNEMA_URL` (default `http://127.0.0.1:8080`), `HEALTH_TIMEOUT` (5 s), `ALERT_WEBHOOK_URL` |

## Reverse proxy and `TRUSTED_PROXY_CIDRS`

Terminate TLS in a reverse proxy and forward HTTP and WebSocket traffic
(`/api/ws`) to the app's port. Voice and video do **not** go through the
proxy; they use the UDP range directly (and, for members behind a UDP block,
the built-in TURN port). All ports: [ports.md](ports.md).

Caddy (passes `X-Forwarded-For` and WebSocket upgrades by default):

<!-- test:caddy -->
```caddy
chat.example.com {
    reverse_proxy app-host:8080
}
```

nginx:

<!-- test:nginx -->
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

**What `PUBLIC_URL` sets.** Set it once; each of these follows from it unless its
own variable is set (explicit values always win):

| Derived setting | From `PUBLIC_URL` | Overridden by |
|---|---|---|
| Allowed origins (CSRF check, WebSocket, CORS) | its origin | `CORS_ALLOWED_ORIGINS` |
| `Secure` and `__Host-` cookies | on when it is `https://` | nothing (set the scheme) |
| Links in e-mails, invites and webhook URLs | its address | nothing |
| Web Push contact (VAPID subject) | itself when `https://` | `PUSH_VAPID_SUBJECT` |
| Voice address | an IPv4 address in it (a LAN install opened by IP) | `WEBRTC_NAT_1TO1_IP` |
| Public IP for voice (host-name installs) | found by a STUN request at startup | `WEBRTC_NAT_1TO1_IP`, `WEBRTC_AUTO_ANNOUNCE=false` |

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

## Vanity invite link

The Owner and Administrators can give the community one short address,
`https://chat.example.com/join/your-community` (Admin console > Invites >
*Vanity link*; `GET|PUT /api/admin/vanity-link`, audited as
`invite.vanity_link`). The name is 3 to 32 characters: lowercase letters,
digits and single hyphens between them (upper case is turned into lower case).
Words that name something of the app (`admin`, `api`, `join`, `login`,
`settings`, `wiki`, `events`, `assets`, `health`, `static`, ...) are reserved; a
test keeps the list in step with the routes of the web app and the files of the
web build. There is one link per community.

**Registration stays invite-only.** By default the link is only a friendly door:
it opens the sign-in page under "You were invited to <community>", and creating
an account still needs an invite code (individual, limited, revocable). The
link grants nothing.

Only the Owner can turn on the switch *Let this link create accounts*
(an Administrator gets 403, and renaming a link that takes sign-ups switches it
off for them); an Administrator may set or clear the name and switch sign-ups
off. The switch turns the link into a permanent public invite: unlimited uses,
no expiry. Anyone who knows the link can then register,
so enable it only when you want that (a poster, a profile bio). The sign-up goes
through the same registration as every invite (username rules, password rules,
rate limits, raid protection, the rules gate); its code is `~<name>`, which no
real invite code can look like. Accounts made this way have no invite behind them,
so banning one does not revoke an invite, and the audit log shows them as a
plain `auth.register` (no invite is recorded). Switching it off or removing the
link takes effect at once for every new sign-up; one that is already being
processed may still complete.

`GET /api/join/<name>` (public, rate limited like the invite preview) tells the
sign-in page whether the link exists and whether it takes sign-ups. Link
previews of `/join/<name>` carry the community name and nothing else; an
unknown name gets the generic page and is never written into it. The reverse
proxy needs nothing extra: `/join/...` is an ordinary page of the app, forwarded
like every other path ([reverse-proxy.md](reverse-proxy.md)).

## Sessions and inactivity sign-out

Each browser sign-in is a session. Members see their own sessions under
*Settings* > *Sessions* ("Where you are signed in"): a coarse device label
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
Owner and Administrators set the limit under *Admin* > *Sign-in & messages* (the community settings; `session_inactivity_days` of `PUT /api/admin/onboarding`), 1 to 365
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

## Rotating keys

Two secrets in `.env` protect accounts, and each can be replaced without
signing anyone out or breaking two-factor sign-in:

| Secret | Protects | While you rotate it |
|---|---|---|
| `JWT_SECRET` | Session cookies and the known-device cookie of the sign-in lockout | `JWT_SECRET_PREVIOUS` keeps verifying what the old secret signed |
| `DATA_KEY` | Authenticator secrets (encrypted) and recovery codes (keyed hashes) in the database | `DATA_KEY_PREVIOUS` keeps reading what the old key sealed |

An instance that sets neither `DATA_KEY` nor a previous value behaves as it
always did: the keys for the second factor are derived from `JWT_SECRET`.
Nothing has to be done after an update. Set `DATA_KEY` when you want to be
able to replace the session secret on its own.

Every stored authenticator secret names the data key it is under (the ones
under the session secret carry no name, which is the format of earlier
releases). A secret under a previous key is sealed again under the current
one the next time its owner signs in with a code; `mnema-talk rotate-keys`
does it for everyone at once. The command may be repeated and can run while
the server does:

```
docker exec mnema-talk /usr/local/bin/mnema-talk rotate-keys
```

```
This run seals under DATA_KEY, fingerprint 5f0c2a91. The server logs the fingerprint of its key at start ...
Previous keys configured for this run: DATA_KEY_PREVIOUS.
Authenticator secrets: 3 moved to DATA_KEY, 0 were under it already.
No authenticator secret is under a previous key any more.
2 account(s) hold unused recovery codes. Codes made before the rotation are checked with the previous key and stop working when you remove it; ...
```

**The command must run with the settings the server runs with.** The
`docker exec` line reads the environment of the running container, which is
the `.env` of the last `docker compose up -d`, not the file as you have just
edited it. So every runbook below restarts first and rotates second. Two
things in the output tell you that the order was wrong:

- The first line names the key new data is sealed under and a fingerprint of
  it (eight hex digits, not secret). The server logs the same fingerprint when
  it starts (`authenticator secrets are sealed under ...`), and
  `mnema-talk doctor` prints it. When the two differ, the command and the
  server use different settings: run `docker compose up -d` and then the
  command again.
- `Previous keys configured for this run` lists the previous values the run
  could read. When none is configured the command says
  `WARNING: no previous key is configured, so this run could not move anything from a replaced key`.
  In the middle of a rotation that means the restart is missing: do not remove
  or overwrite the old value.

At every start the server tries each stored authenticator secret with the
configured keys. When some cannot be opened it logs an error,
`AUTHENTICATOR SECRETS CANNOT BE READ`, with the number of accounts (no
names) and how many of them name a data key that is not configured;
`mnema-talk doctor` shows the same as a red line. Those members cannot pass
the second step of sign-in until the key is configured again, as the current
or a previous value, or their second factor is reset.

All secrets move in one transaction. When one of them cannot be read with any
configured key, or anything else fails, the command changes nothing, exits
with an error and names the accounts. Then a key is missing: do not remove
anything yet. A setup that was started and never confirmed is the exception:
when no configured key reads it and it was started more than an hour ago, the
command removes it and names the account. Two-factor sign-in was not on for
it, and the member starts again. A younger one is left alone: it may be a
setup a member is in the middle of.

**Recovery codes cannot be moved**, because only their hash is stored. A
recovery code works for as long as the key it was made under is still
configured, as the current or the previous value. When you retire a key,
recovery codes made under it stop working; the authenticator keeps working,
and the member makes new codes in the account settings. The number of
remaining codes in the account settings leaves out codes under a retired key;
codes made before the release that records their key are still counted,
because nothing tells which key they are under.

Copy `.env` before you start, and keep a retired key for as long as you keep
backups from its time: a restored database is sealed with the keys of the day
it was taken (`scripts/backup.sh` stores `.env` with it).

**`DATA_KEY` stays once it is set.** From the first sign-in with a code after
you set it, secrets are sealed under it, and only it opens them. A server
whose `DATA_KEY` was emptied starts, logs the error above and refuses the
second step for those members. Nothing is lost while you still have the
value: put it back, or follow [Stop using a data key](#stop-using-a-data-key).
Keep the value where you keep your backups.

### Start using a data key

1. Add `DATA_KEY=<openssl rand -hex 32>` to `.env`.
2. `docker compose up -d`, then run `mnema-talk rotate-keys`.

There is no previous key to name: the key the secrets were under is the one
derived from `JWT_SECRET`, which is still configured. Recovery codes made
before this step keep working until that session secret is retired.

### Replace the session secret

1. Move the current value to `JWT_SECRET_PREVIOUS` and put a new one
   (`openssl rand -hex 32`) into `JWT_SECRET`.
2. `docker compose up -d`, then run `mnema-talk rotate-keys`. Without a
   `DATA_KEY` this moves the authenticator secrets to the new session secret;
   with one there is nothing to move.
3. Remove `JWT_SECRET_PREVIOUS` once the old sessions have expired:
   `SESSION_EXPIRY_HOURS` after step 2 (30 days by default). Removing it
   earlier signs out everyone who has not signed in since step 2, and nothing
   else. Recovery codes made while the old secret keyed them stop working
   then (see above).

### Replace the data key

1. Move the current value to `DATA_KEY_PREVIOUS` and put a new one into
   `DATA_KEY`.
2. `docker compose up -d`, then run `mnema-talk rotate-keys`.
3. Tell members with two-factor sign-in to make new recovery codes, then
   remove `DATA_KEY_PREVIOUS` and run `docker compose up -d` again.

### Stop using a data key

The way back to the keys derived from `JWT_SECRET`. Emptying `DATA_KEY`
alone is not enough: the secrets sealed under it would be unreadable.

1. Move the current value of `DATA_KEY` to `DATA_KEY_PREVIOUS` and leave
   `DATA_KEY` empty.
2. `docker compose up -d`, then run `mnema-talk rotate-keys`. The output
   names `DATA_KEY_PREVIOUS` as a previous key and moves the secrets to the
   key derived from `JWT_SECRET`.
3. Tell members with two-factor sign-in to make new recovery codes, then
   remove `DATA_KEY_PREVIOUS` and run `docker compose up -d` again.

### A secret has leaked

The same steps as above, without the waiting:

1. Move the leaked value to its previous variable (`JWT_SECRET_PREVIOUS` or
   `DATA_KEY_PREVIOUS`) and put a new one (`openssl rand -hex 32`) into
   `JWT_SECRET` or `DATA_KEY`.
2. `docker compose up -d`. Without this restart the next step runs with the
   old settings, moves nothing and step 4 leaves every authenticator secret
   unreadable.
3. Run `mnema-talk rotate-keys`. Go on only when the output lists the previous
   variable under `Previous keys configured for this run` and ends without an
   error.
4. Remove the previous value from `.env` right away and run
   `docker compose up -d` again. Check the start log: it must not contain
   `AUTHENTICATOR SECRETS CANNOT BE READ`.

For the session secret step 4 signs everyone out, which is the point. While
`JWT_SECRET_PREVIOUS` is set, whoever holds the old secret can still mint
valid sessions for any account, which is why the previous value is removed
right away and not after the sessions have expired. Recovery codes made
under the leaked key are gone with it.

Sealing the secrets again does not undo what was already readable. Without a
`DATA_KEY`, a leaked `JWT_SECRET` together with a copy of the database from
before the rotation (a backup, a dump) decrypts the authenticator secrets in
that copy. If such a copy may be in the same hands, those members must set up
two-factor sign-in again: reset them in Admin > Members or with
`mnema-talk reset-2fa <username>`.

## Web Push and the installable app

Mnema Talk is an installable web app (manifest, icons and a service worker at
`/sw.js`). Members can install it from the browser (*Settings* > *Push & app*) and turn on push notifications per device, so they hear about
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
- **What a push contains.** Channel name (for a direct message only the sender), channel slug (for direct navigation) and sender are always in it; the start
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

## Built-in TURN relay

Needed only for members whose network blocks the UDP range: strict
firewalls, some VPNs, carrier networks, Opera with its VPN or WebRTC
protection on. The server has a TURN relay of its own for them, so a plain
install needs no second service. They connect to one port, over UDP or TCP,
and the relay hands their media to the voice server on the same machine.

**When it runs.** `WEBRTC_BUILTIN_TURN` switches it:

| Value | Meaning |
|---|---|
| `auto` (default) | Runs while `WEBRTC_TURN_URLS` is empty. As soon as you configure an external TURN server (coturn), the built-in relay stays off, so an installation that already has one behaves exactly as before. |
| `on` | Always runs. With `WEBRTC_TURN_URLS` set too, members are offered both servers and the browser takes whichever connects (the server logs this at startup). |
| `off` | Never runs. |

**What to forward.** The relay port (`WEBRTC_BUILTIN_TURN_PORT`, default
`3479`) on **both UDP and TCP** to the host, besides the media range
(`50000-50050/udp`). It needs no relay port range of its own. Compose
publishes the port whether the relay runs or not (with `off`, or an external
TURN under `auto`, nothing listens on it); the forward is only needed while the
relay runs. If something else on the host uses 3479, set
`WEBRTC_BUILTIN_TURN_PORT` to a free port. `TURN_PUBLISH_ADDR` narrows the
address it is published on (default all). Members are given `turn:<host of PUBLIC_URL>:<port>`, so that
name must resolve to the server itself from the outside, not to a CDN or tunnel
that only forwards HTTP: set `WEBRTC_BUILTIN_TURN_HOST` to a name that does
otherwise. Behind `localhost` or a loopback `PUBLIC_URL` the first
`WEBRTC_NAT_1TO1_IP` entry is used.

**Next to coturn.** The default 3479 is not coturn's 3478, so an external
coturn (profile `turn`, host networking) and the built-in relay can run on one
host. With `WEBRTC_TURN_URLS` set, `auto` keeps the built-in relay off; to try
it next to coturn set `WEBRTC_BUILTIN_TURN=on` and forward 3479 as well:
members are offered both servers and the browser takes whichever connects.
`TURN_PUBLISH_ADDR=127.0.0.1` stops Docker from publishing the port.

**If the port cannot be bound** (taken, or the container lacks the port), the
server logs a warning, keeps running without the relay (or with the one
transport that did bind) and shows *Built-in TURN relay is not fully running*
under Admin > System. Voice itself is unaffected. Admin > System also shows
whether the relay is listening, how many sessions it relays and a best-effort
test of the public name and port: the server sends one UDP request to
`<host>:<port>` from inside. *Not reachable from here* is a hint, not proof: a
router without NAT loopback fails it although the forward works, and a pass
says nothing about a firewall in front of your router. Test from outside
(a phone on mobile data) when in doubt.

**What it can and cannot do.** The relay is a bridge to the voice server and
nothing else:

- It forwards only to the voice server's own media sockets: its addresses
  (the announced `WEBRTC_NAT_1TO1_IP` and the machine's own) and its media
  ports. A request to relay to any other address or port is refused when the
  permission is asked for, and the relayed socket itself drops every datagram
  that does not go to, or come from, those media ports. It cannot reach your
  LAN, other containers, the host's other services or the internet.
- It only relays UDP. TCP allocations (RFC 6062) are refused.
- Credentials are minted by `GET /api/webrtc/config` for the signed-in member
  and only for members who may join a Talk. They are TURN REST style
  (`<expiry>:<member id>` plus an HMAC), signed with a random secret generated
  at every start, so they stop working at a restart and are not
  `WEBRTC_TURN_SECRET`. They are valid for 12 hours (a running call cannot
  renew them, so this is deliberately longer than a long call). Starting or
  refreshing a session re-checks that the account is neither disabled nor
  banned and that the member may still join voice (only for requests that
  passed the integrity check, never for forged ones; answers are reused for a
  few seconds), so a removed member loses the relay at the next refresh
  (within 10 minutes), not after 12 hours. If
  that check cannot be made (database down), new sessions are refused but
  running ones are kept.
- Limits: 4 sessions per member and 256 in total, 120 allocation attempts a
  minute per address (burst 60), a username that keeps failing is refused
  from that address for a while (counted per address and username, so forged
  requests cannot lock out the members behind one address), 4 MiB/s per
  session towards the voice server, sessions that carry nothing for 2 minutes
  or outlive their credential are closed, and TCP connections are capped per
  address, must authenticate within 15 seconds and are idle-timed.
- Refused attempts are counted in `mnema_turn_events_total{event}`
  (`auth_failed`, `forbidden`, `rate_limited`, `quota_user`, `quota_total`,
  `peer_denied`, `packet_blocked`, `throttled`) and logged at debug level
  (`LOG_LEVEL=debug`). `mnema_turn_allocations` is the live sessions,
  `mnema_turn_relayed_bytes_total{direction}` the traffic.

The built-in relay is part of your server, run by you and not a third party.
The privacy policy (version 1.7 and later) describes it as such while it is
offered: it sees the member's IP address and port, forwards encrypted media it
cannot read only to the server's own SFU, uses short-lived credentials, stores
nothing persistently and keeps only operational counters. IP addresses and
account IDs reach the log only at `LOG_LEVEL=debug`, so name that in your own
records if you run debug logging. An external TURN server from
`WEBRTC_TURN_URLS` is listed separately by its configured address.

## TURN (coturn)

The alternative to the [built-in relay](#built-in-turn-relay), for operators
who want a separate relay process or public relay ports. Needed only for
members whose network blocks the UDP range (strict firewalls, some VPNs, Opera
with its VPN or WebRTC protection on). Without any relay those members get no
voice: their browser tells them "No media connection" and the server log shows
`sfu ice failed` for them. At startup the server logs `webrtc relay: ...` when
no relay is on offer, or none that works over TCP or TLS. Setting
`WEBRTC_TURN_URLS` switches the built-in relay off under
`WEBRTC_BUILTIN_TURN=auto`. coturn runs with host networking on port 3478;
the built-in relay uses 3479, so the two do not collide.

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
Configured TURN servers are listed in the privacy policy by their address; the
policy describes the operator of such a server as a recipient of the member's
IP address and port, so list the provider in your own records if it is not you.

**Networks that block more than UDP.** `transport=tcp` on port 3478 passes
firewalls that drop UDP but allow other ports. Some company networks only allow
TCP 443 and inspect it; for them list a `turns:` URL
(`turns:turn.example.com:443?transport=tcp`). The bundled coturn has no
certificate, so `turns:` needs a coturn of your own with a TLS certificate (or a
TCP/TLS multiplexer in front of it) and the secret from `WEBRTC_TURN_SECRET`.
The app passes every URL in `WEBRTC_TURN_URLS` to the browser unchanged.

## Can I host at home?

Only if your internet connection has a public IPv4 address of its own. Voice
and video reach the server over UDP ports that your router forwards to it; a
provider that shares one public address between many customers (carrier-grade
NAT, CGNAT) or gives you IPv6 plus a shared IPv4 (DS-Lite) never delivers
those packets, however you configure the router. Chat over HTTPS may even work
through a tunnel while every voice call outside your home fails.

Mnema Talk looks for the signs and says so in the **Internet connection** item
of the Status tab, in the startup log (`setup check:` line) and in the setup
assistant's checks:

- the address STUN reports, or the host's own WAN address, is in
  `100.64.0.0/10` (the range providers reserve for CGNAT);
- no public IPv4 address can be found while a global IPv6 address works
  (typical for DS-Lite, for example some Fritz!Box lines);
- the host has a public address of its own that differs from the one the
  internet sees, so its traffic leaves through another gateway.

Check it yourself in two minutes: open your router's overview page (on a
Fritz!Box: *Internet > Online Monitor*), note the WAN or "Internet" IPv4
address and compare it with what `curl -4 ifconfig.me` prints on any computer
in the network. If they differ, or the router shows an address starting with
`10.`, `192.168.`, `172.16.`-`172.31.` or `100.64.`-`100.127.`, port forwarding
cannot work. Your options:

1. Ask the provider for a public IPv4 address (often a free option or a business
   tariff) or a dual-stack line instead of DS-Lite.
2. Run Mnema Talk on a small VPS, which has a public address.
3. Keep the instance at home and put an external relay in front of voice:
   coturn on a VPS ([TURN (coturn)](#turn-coturn)).

## IPv6, DS-Lite and CGNAT

- **The announced addresses are IPv4.** `WEBRTC_NAT_1TO1_IP` takes IPv4
  addresses and names that resolve to IPv4. With it set, the server offers
  browsers those addresses instead of its own and no IPv6 address that a
  browser cannot reach (Docker's unique-local `fd00::/8`, link-local or
  loopback). A global IPv6 address on a host-networked server is still offered.
  An instance with only an `A` record (no `AAAA`) is fine.
- **Members on IPv6-only or DS-Lite lines** reach the server's IPv4 address
  through their provider's translation (CLAT/NAT64 or the carrier-grade NAT).
  Outbound UDP works through it, so they need nothing from you; if their
  provider blocks it, only TURN over TCP helps.
- **The server behind DS-Lite or CGNAT** (no public IPv4 of its own: the
  router's WAN address is private, or `ipv4.icanhazip.com` and the router
  disagree) cannot receive the forwarded UDP range at all. Browsers outside
  the LAN then never connect directly. Rent a small VPS with a public address,
  run coturn there (`--profile turn`, `WEBRTC_NAT_1TO1_IP` set to the VPS) and
  list it in `WEBRTC_TURN_URLS`; or run the whole instance on the VPS.
- **Forward UDP for the SFU** (`WEBRTC_UDP_PORT_MIN`-`MAX`, default
  `50000-50050/udp`) to the host. Forwarding an IPv6 port is not needed.

## Database limits

The server sets three limits on its own database connections. They are
session settings of those connections, not of the database or the role, so
`pg_dump`, `psql` and the backup and restore scripts are not affected.

- **Pool size** (`DB_MAX_CONNS`, default 25). A request that finds no free
  connection waits for one; `mnema_db_acquires_waited_total` counts those.
- **Statement timeout** (`DB_STATEMENT_TIMEOUT`, default `15s`). One slow
  statement can no longer hold a connection, and the locks it took, for as
  long as it likes. A cancelled statement is logged with `SQLSTATE 57014`. If
  that shows up for ordinary requests on a slow disk, raise the value before
  switching it off with `0`.
- **Idle in transaction** (fixed, 1 minute). A session that opened a
  transaction and then runs nothing is ended by the database.

Work that rightly takes longer has its own limit instead of the request one:

| Work | Statement timeout |
|---|---|
| Schema migrations at startup (an index build on a large table) | none |
| Message search and forum post search | 5 s, answered with 429 |
| Data export, account deletion, deleting a channel, Discord import and its undo | 5 min |
| Sweeps: auto-delete, unsent attachments, orphaned media, edit history retention | 5 min |

The longer limits never undercut `DB_STATEMENT_TIMEOUT`: with a higher value,
or with `0`, that one applies to this work as well.

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
  This cold mode is the default; [`BACKUP_MODE=online`](#online-backup) keeps
  the app running.
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
  [`BACKUP_AGE_RECIPIENT`](#encrypted-backups) encrypts it.
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

**Restore test and status report.** After the services are back, `backup.sh`
runs `restore.sh <new backup> --verify`: it imports the dump into a throwaway,
network-isolated PostgreSQL container and removes it again, so the live
database is never touched (`RESTORE_TEST=0` skips this). It then writes
`backup-status/status.json` in the deployment directory (`BACKUP_STATUS_DIR`),
atomically and world-readable, without secrets:

```json
{"format":1,"last_backup_at":"2026-11-06T02:30:02Z","size_bytes":52428800,"restore_test":{"result":"passed","at":"2026-11-06T02:31:10Z"}}
```

`result` is `passed`, `failed` or `skipped`. A failed restore test keeps the
backup, prunes nothing and makes the script exit non-zero, so cron reports it.
A failed backup leaves the previous status file as it was. The bundled
`docker-compose.yml` mounts `./backup-status` read-only at `/backup-status` and
sets `BACKUP_STATUS_FILE`; with your own compose file add both:

```yaml
    environment:
      - BACKUP_STATUS_FILE=/backup-status/status.json
    volumes:
      - ./backup-status:/backup-status:ro
```

Admin → Status → *Backups & disaster recovery* then shows the age and size of
the last backup and the restore test. It warns when the backup is older than
36 hours (a nightly run was missed) or the restore test failed. Without
`BACKUP_STATUS_FILE` it says *not reported*; with it but before the first
backup, *no backup reported yet*. When Docker created `backup-status` (as root,
on the first `up` before any backup) and the script runs as another user, the
script writes the file through a short-lived container instead.

### Online backup

```sh
BACKUP_MODE=online BACKUP_DIR=/srv/backups/mnema ./scripts/backup.sh
```

The app is not stopped: messages, presence and calls continue. Only the file
storage is frozen (`docker compose pause seaweedfs`) while its volume is
copied, so uploads and downloads wait for that long (seconds on a small
instance, as long as the copy takes on a large one) and may time out; text
chat and calls are not affected. The order makes the copy consistent:

1. The script takes an advisory lock in PostgreSQL. The server's jobs that
   delete stored files (channel auto-delete, media pruning, the sweep of
   unsent attachments, the orphan report and the export sweep) wait while it
   is held, and the script waits up to `BACKUP_LOCK_WAIT` seconds for one that
   is running. The orphan cleanup (`POST /api/admin/media/orphans/cleanup`)
   answers `409 CONFLICT` during a backup. Servers up to 3.0.0 do not know
   the lock: their jobs keep running and can leave orphaned files behind.
2. It freezes the storage, fixes one database snapshot, copies the storage
   volume and lets the storage continue.
3. `pg_dump` writes exactly that snapshot while the app is in use again.

An upload stores its file before its database row is committed and a delete
removes the row first, so every file the snapshot refers to is in the copy.
The restore test proves it for each backup: `restore.sh --verify` starts a
throwaway, network-isolated SeaweedFS on a copy of the media archive and
reads every file the database refers to (`media-keys.txt`). That needs free
disk space for the extracted media; `RESTORE_TEST=0` skips it together with
the database import and is not recommended in online mode. Messages and
uploads from after the snapshot are not in the backup. A member who deletes a
message while the storage is frozen gets the row removed at once and the file
later counted as an orphan (the daily orphan report in the log).

An online backup directory is the same format (`format=mnema-backup-v1`) with
additions only: the file `media-keys.txt` (one `bucket/key` per line) and the
manifest lines `mode=online` and `media_keys_sha256=`. Restoring works the
same way as for a cold backup. A script that was killed hard may leave the
storage paused: `docker compose unpause seaweedfs`.

### Encrypted backups

Off by default. With one or more [age](https://age-encryption.org) public keys
in `BACKUP_AGE_RECIPIENT` (separated by spaces; `age` must be installed on the
host), the finished backup, after its restore test, is packed into one file
`BACKUP_DIR/<timestamp>.tar.age` and the plain directory is removed. Only the
matching identity (private key) can read it, so keep that key away from the
server:

```sh
age-keygen -o mnema-backup.key          # on another machine; prints the public key
BACKUP_AGE_RECIPIENT=age1... BACKUP_DIR=/srv/backups/mnema ./scripts/backup.sh

# Restore: the identity file comes back only for this
BACKUP_AGE_IDENTITY=/root/mnema-backup.key ./scripts/restore.sh /srv/backups/mnema/20261110-033000-AbCdEf.tar.age --verify
```

`restore.sh` unpacks the file into a temporary folder next to it (allow the
disk space), restores from it and removes the plain copy again. Retention
(`KEEP_DAYS`) removes old `*.tar.age` files after a successful backup without
opening them; each was validated before it was encrypted. Until the restore
test has finished, the new backup lies unencrypted in `BACKUP_DIR`.

## Verifying a release image

Release images on GHCR (`ghcr.io/msch128/mnema-talk`, every tag of a release)
and their mirror on Docker Hub (`docker.io/msch128/mnema-talk`, see below)
are signed keylessly with [cosign](https://github.com/sigstore/cosign) by the
release workflow, using its GitHub OIDC identity; no long-lived key exists.
The image also carries BuildKit provenance and an SBOM. To check a pulled
image before running it:

```sh
cosign verify ghcr.io/msch128/mnema-talk:latest \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity-regexp '^https://github\.com/msch128/mnema-talk/\.github/workflows/release\.yml@'
```

The Docker Hub copy carries the same digest, signature identity, provenance
and SBOM, so the same command works with the Docker Hub reference:

```sh
cosign verify docker.io/msch128/mnema-talk:latest \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity-regexp '^https://github\.com/msch128/mnema-talk/\.github/workflows/release\.yml@'
```

`docker buildx imagetools inspect` shows the same index digest on both
registries. GHCR stays the primary reference; the updater sidecar and the
examples use it.

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

- By default the updater trusts exactly two repositories:
  `ghcr.io/msch128/mnema-talk` and its Docker Hub mirror `docker.io/msch128/mnema-talk`
  (`msch128/mnema-talk` and `index.docker.io/...` name the same repository).
  It updates from the registry the running app image names, so an install with
  `MNEMA_IMAGE=msch128/mnema-talk:latest` stays on Docker Hub. Signature
  identity, issuer and provenance source are the same for both; any other
  namespace is refused. With `UPDATER_IMAGE_REPOSITORY` set, only that
  repository is trusted.
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
  `forward`, `search.filters.v2`, `threads.follow`, `slash.builtins`, `profile.banner` and `profile.avatar_remove`;
  1.2 adds `search.filters.v3`, `talks.lobby`, `status.expiry`, `reminders`, `messages.scheduled`, `polls.anonymous`, `channels.auto_delete`, `automod`, `moderation.history`, `ops.status`, `import.discord`, `talks.stage`, `talks.reactions`, `channels.forum`, `events`, `notify.levels.v2` and `dm.v2`; later releases add `events.roles`, `events.recurrence`, `wiki`, `messages.nonce` and `reports.open_count`;
  names never change meaning ([api-stability.md](api-stability.md)). A 1.0.x
  server sends no list. Storage is checked live, with one `HEAD` on the bucket
  at most every 10 s; it counts as down after two failed probes in a row, so a
  single blip does not turn the compose healthcheck `unhealthy`. The compose
  healthcheck and [`scripts/health-check.sh`](../scripts/health-check.sh)
  (cron, Unraid notification, optional webhook) use it.

  The `503` says what the server is as well: beside the usual `error` it
  carries `version`, `web_client_api`, `community_name`, `community_icon` and
  `features` like the `200`, `status` is `unavailable`, and `unavailable`
  lists what is down (`database`, `storage`, or both). With only `storage` in
  the list, uploads, attachments, avatars and other pictures fail while chat
  and Talks keep working: the web app starts as usual and shows a notice
  until storage is back (it asks again every 30 seconds). An older server
  answers a `503` with the `error` alone.
- **`GET /api/health/live`** (public): `200` with `{"status":"ok"}` whenever
  the process serves requests. It asks neither the database nor object
  storage, so it stays `200` while `/api/health` answers `503`. It is the
  liveness probe for an orchestrator that restarts a container on a failed
  probe (Kubernetes, Swarm, an autoheal container): restarting the app does
  not bring a dependency back, and it would end every running Talk. Use
  `/api/health` for readiness and for monitoring. The compose healthcheck
  stays on `/api/health`: plain Compose only labels the container, and
  `docker compose up --wait`, the setup assistant and the scripts rely on
  "healthy" meaning "ready". See
  [deployment.md](deployment.md#health-probes-for-other-orchestrators).
- **`GET /api/metrics`** (Prometheus text format), only when `METRICS_TOKEN`
  is set. Every metric is listed under [Monitoring and
  alerts](#monitoring-and-alerts) below, with scrape config and alert rules.
- **Admin → System** shows version and commit, database and migration state,
  storage, voice/SFU and server resources, the update check and a live
  operator view (uptime, CPU, memory, traffic, errors, ICE failures, charts
  of the last hour). It reads `GET /api/admin/system/ops` (admin session
  only), not the bearer endpoint.
- **Admin → Status** (feature key `ops.status`, `GET /api/admin/status`, Owner
  or Administrator only) shows an operator status overview: server version,
  update state, database size and migration version, storage reachability and
  disk free space, backup and restore status, live activity (sessions,
  WebSocket connections, Talks and participants), and active operator warnings.
  See [Operator status](#operator-status) below.

## Operator status

The **Admin → Status** tab provides an operator status overview in a single view:
- **Server version & updates**: Running binary version, git commit, Go runtime version, update availability, and auto-deploy container image status.
- **Database**: Reachability, database size (`pg_database_size`), applied migration count, and latest migration file.
- **Object storage & disk**: S3-compatible storage reachability and used media bytes, plus free disk space on the data/upload volume (`MEDIA_DISK_PATH`) when configured and statable.
- **Backups**: age and size of the last host-level backup and the result of its restore test, as `scripts/backup.sh` reports them; a warning when the backup is older than 36 hours or the test failed (see [Backups and restore](#backups-and-restore)).
- **Live activity**: Active sessions, connected WebSocket clients, active Talks, and current voice participants.
- **Operator warnings**: Active configuration warnings collected by the server (e.g. broad trusted proxies, WebRTC reachability).
- **Setup checks**: a green, amber, red and neutral list of what a new installation needs (below).
- **Capacity**: live CPU, memory and media rates, and per kind of Talk an estimate of how many people fit and what runs out first (below).
- **Test my connection**: a button that checks, from the browser you are using, whether voice reaches the server (below).

### Setup checks

The first block of the Status tab lists the checks below, red first, then amber,
then neutral (grey, *Not verified*), then green. The list needs about 20 seconds
after a start to be complete: an earlier look can show partial results. Every
amber or red item names the variable to set; the same list is logged (`setup check:`
lines, then one `setup checks` summary) about 20 seconds after the start, once
object storage and the clock measurement have settled. Fix the cause in `.env`,
run `docker compose up -d` and reload the tab.

| Check | Red | Amber |
|---|---|---|
| Public address | n/a | `PUBLIC_URL` is `localhost` (fine for a trial on one computer, nobody else can join), plain `http://`, or the page was opened at another address (behind a reverse proxy: it must pass `Host` and `X-Forwarded-Proto`) |
| Voice address | nothing announced and no TURN (`WEBRTC_NAT_1TO1_IP`) | nothing announced, TURN configured |
| Voice port | the voice engine did not start (UDP ports busy) | `WEBRTC_UDP_MUX_PORT=0` caps the number of callers |
| TURN relay | n/a | built-in relay could not bind its port, or `WEBRTC_BUILTIN_TURN=off` with no `WEBRTC_TURN_URLS` |
| Outgoing mail | n/a | `SMTP_HOST` empty (optional) |
| Object storage | not configured or not answering | n/a |
| Database | not answering | n/a |
| Server clock | 30 seconds or more off | 5 seconds or more off (left out when `CLOCK_CHECK_ENABLED=false` or no NTP answers) |
| Reverse proxy | n/a | proxy headers from an untrusted address, or `TRUSTED_PROXY_CIDRS` too broad |
| Internet connection | the WAN address is in `100.64.0.0/10` (CGNAT): port forwarding cannot work | no public IPv4 while IPv6 works (DS-Lite), or the host's own public address differs from the one the internet sees. Shown only when there is a finding; see [Can I host at home?](#can-i-host-at-home) |
| WebSocket | n/a | browsers opened `/api/ws` three or more times in 15 minutes without a WebSocket upgrade and none succeeded since |

Two items are neutral until the server has seen proof, because it cannot know
from inside the host whether the router forwards the voice UDP port:

- **Voice port** reads *Listening – not yet confirmed from outside* until a
  member whose address is not private (not on the LAN, loopback or
  carrier-grade NAT) has completed a media connection since the server started.
  Then it turns green and reads *Confirmed: a member from outside connected*
  with the time. The record is kept in memory only, so it starts empty after
  every restart. A connection made with *Test my connection* counts. To get it
  green yourself, run the test from a phone on mobile data (Wi-Fi off).
- **TURN relay** shows *Not verified* (neutral, no longer amber) when the server
  could not reach its own relay port from the inside; a router without NAT
  loopback causes that although the forward works. *Test my connection* from a
  phone on mobile data answers it.

**WebSocket.** When an administrator opens the tab, the page reports whether its
own `/api/ws` connection is open, and the server notes every signed-in request
for `/api/ws` that arrived with or without the WebSocket upgrade headers. The
item reads *WebSocket via proxy OK* with the time of the last upgrade. It turns
amber when requests keep arriving without the headers, which is what a reverse
proxy without WebSocket support produces (the app then shows *Connection lost*).
The fix is to enable WebSocket in the proxy: it must pass `Upgrade: websocket`
and `Connection: Upgrade`. Nginx Proxy Manager has a *Websockets Support*
switch; nginx needs `proxy_http_version 1.1;`,
`proxy_set_header Upgrade $http_upgrade;` and
`proxy_set_header Connection upgrade;`; Caddy and Traefik do it by default.
The item is neutral (*Not checked yet*) until a browser has connected.

### Capacity

The **Capacity** panel in Admin → Status (`GET /api/admin/status/capacity`,
Owner or Administrator only) shows, averaged over the last minute: CPU used of
the cores the container may use (the cgroup quota when there is one), memory
(resident size against the container limit, `APP_MEM_LIMIT`), the media the
server sends (Mbit/s and packets/s) and the members and screen shares in calls.
It works with no traffic at all; in the first seconds after a start, before
the first measurement, it says it is collecting.

Below that it prints an **estimate**: for each kind of Talk, about how many
people one Talk can hold on the CPU, memory and upload that are free now, and
which of them runs out first:

| Row | Shape | Basis |
|---|---|---|
| Text chat only | members online in a text channel, one in ten writing 0.2 messages/s | the chat runs (measured up to 1,000) |
| Listening | 3 speakers, everyone else listens | measured up to 100 |
| Conversation | everyone talks at once (continuous speech) | derived from the measured costs |
| Screen share | one 1.5 Mbit/s share and 3 speakers, everyone else watches | measured up to 100 |
| Video call | everyone talks with the camera on and sees up to 24 others | derived from the measured costs |

How each row is computed, from which measurement, is in [the
benchmark](benchmark.md#the-scenarios-in-admin--status). A row shows *measured
up to N*, *beyond the largest measured run* when the figure goes past it, or
*derived* when the shape was never run as a whole.

- **CPU** uses the cost model of [the benchmark](benchmark.md#the-model): 22 µs
  of one core per packet sent, 72 µs per packet received, 0.57 ms per second per
  member. It plans at half of the CPU, because bursts lose packets well before
  100 %, minus what the server uses now. With substantial traffic (5,000
  packets/s sent or more) the panel compares the CPU the media actually used
  (the process CPU minus about 0.08 core the server spends with no media at
  all) with what the model predicts and scales the media cost by that factor
  (between 0.5 and 4), so a slower or faster host gives its own numbers; with
  less traffic it uses the table values and says so. Text chat is not scaled.
- **Memory** counts 1.3 times the measured peak heap per member against what is
  left below 85 % of the container limit (`APP_MEM_LIMIT`).
- **Upload** is counted only when `VOICE_UPLOAD_LIMIT_MBIT` is set: the row then
  stops at that rate. Otherwise each row shows the upload it would need, and a
  line under the table says how to include it; on a home line upload is
  usually the first limit.

When the server is already past half of its CPU the panel says that instead of
numbers, and when resident memory is above 85 % of the limit it says memory
comes first. Relayed packets (+12 µs each through the built-in TURN) and
several Talks at once are not part of it. CPU quotas are read from the
container's cgroup (v2 `cpu.max` or v1 `cpu.cfs_quota_us`); without one the
host's cores count. The fields are in `GET /api/admin/status/capacity`
(`estimate.scenarios`).

### Test my connection

**Test my connection** (Admin → Status, and under Settings → Audio for every
member) runs in the browser and needs no microphone or camera: it does not join
a Talk and leaves no presence. It

1. gathers ICE candidates against the configured STUN and TURN servers and
   reports how many are local (host), public (srflx) and relay;
2. connects to the server's voice port directly over UDP, with one data channel
   to a throwaway connection on the server (`POST /api/webrtc/probe`, signed-in
   members only, twelve per ten minutes each, eight at a time);
3. connects to the voice port through the built-in TURN relay over UDP, and
   again over TCP, using relay candidates only;
4. reads a short stream from the server (`GET /api/webrtc/speedtest`: at most
   16 MiB or three seconds, signed-in members only, four per ten minutes per
   member and six per address, one test at a time) and divides what arrived by
   the time it took. It measures the speed **to where the server is reachable**:
   behind a reverse proxy, a CDN or a tunnel that is the speed to that proxy,
   not necessarily to the server (the stream carries `Content-Encoding:
   identity` and `X-Accel-Buffering: no` so proxies pass it through unchanged).
   From outside the server's network it is roughly the server's upload, and the
   result says what screen share that upload can serve (1.5 Mbit/s plus 30 %
   overhead per watcher, as in [the sizing guide](benchmark.md#the-model)).
   A page address on a private network (or a private direct path) is taken as
   the same network, where the test says it cannot measure the upload; run it
   from a phone on mobile data. A speed of 700 Mbit/s or more is shown as
   measured, with the note that it may be a local connection (symmetric
   gigabit fibre exists, but so does a LAN), so test from outside to be sure. A busy slot or an exhausted budget costs nothing, and
   the test is skipped while a Talk has participants, because for three seconds
   it can fill a home uplink and would be heard in the call. The setup assistant
   prints only this hint, never a number.

The verdict is one of *Direct connection works*, *Only the relay works (direct
UDP blocked or not forwarded)* or *Nothing works*, with the likely cause and
the fix, and a link to the voice troubleshooting guide. Run it from a device
outside your network (a phone on mobile data) to test the way in from outside;
on the same network it can only prove the inside. The test sends nothing to
third parties beyond what voice itself does (the STUN servers you configured).

**Check codes.** The API (`GET /api/admin/status`) and the web app identify each
outcome by a stable code, `<check>.<outcome>`; this table is the reference for
what each means. A CI check fails when a code the server can send is missing
here.

| Code | Meaning and what to do |
|---|---|
| `public_url.ok` | `PUBLIC_URL` is set to an address members can use. |
| `public_url.local` | `PUBLIC_URL` is `localhost`: set it to the address members type into their browser. |
| `public_url.insecure` | `PUBLIC_URL` is plain `http://`: cookies lose `Secure`. Put TLS in front and use `https://` (skip on a trusted LAN). |
| `public_url.origin_mismatch` | The page was opened at another address than `PUBLIC_URL`: fix `PUBLIC_URL`, or let the proxy pass `Host` and `X-Forwarded-Proto`. |
| `voice_address.development` | Development mode: voice uses the machine's own addresses. |
| `voice_address.announced_env` | Voice is announced on the address from `WEBRTC_NAT_1TO1_IP`. |
| `voice_address.announced_public_url` | Voice is announced on the address taken from `PUBLIC_URL`. |
| `voice_address.announced_stun` | Voice is announced on the address a STUN request detected. |
| `voice_address.missing` | No public voice address is known: set `WEBRTC_NAT_1TO1_IP`. |
| `voice_address.missing_detect_failed` | As above, and automatic detection failed (the check names the error): set `WEBRTC_NAT_1TO1_IP`. |
| `voice_address.missing_turn_only` | As `voice_address.missing`, and only the TURN relay can carry voice. |
| `voice_address.missing_detect_failed_turn_only` | Detection failed and only the TURN relay can carry voice: set `WEBRTC_NAT_1TO1_IP`. |
| `voice_udp.unconfirmed` | Neutral: *Listening – not yet confirmed from outside*. Voice listens on the named UDP port, but no member from outside the network has connected since the server started. Forward the port and run *Test my connection* from a phone on mobile data. |
| `voice_udp.confirmed` | A member from outside connected (the item shows when): the voice port is reachable from outside. |
| `voice_udp.unavailable` | The voice engine did not start: free the UDP range (or change `WEBRTC_UDP_PORT_MIN` / `WEBRTC_UDP_PORT_MAX`); the log names the cause. |
| `voice_udp.mux_disabled` | `WEBRTC_UDP_MUX_PORT=0` gives each peer its own port and caps the callers: remove it. |
| `turn.builtin_ok` | The built-in TURN relay serves members behind strict firewalls; forward its port (UDP and TCP). |
| `turn.builtin_failed` | The built-in relay could not listen on its port: free it or set `WEBRTC_BUILTIN_TURN_PORT`. |
| `turn.builtin_failed_external` | The built-in relay could not start, but an external TURN relay is configured. |
| `turn.builtin_off_external` | The built-in relay is off and an external TURN relay is configured. |
| `turn.voice_unavailable` | The relay is not running because the voice engine is down: fix `voice_udp.unavailable` first. |
| `turn.unreachable` | Neutral: the server could not verify its relay port from the inside (a router without NAT loopback also causes this). Run *Test my connection* from mobile data; if it fails, forward the port. |
| `turn.external_ok` | An external TURN relay is configured. |
| `turn.none` | No TURN relay: members on networks that block UDP cannot join (optional; see the *TURN relay* row above). |
| `mail.ok` | Outgoing mail is configured. |
| `mail.none` | No outgoing mail: no password reset e-mails (optional). |
| `storage.ok` | Object storage is ready. |
| `storage.not_configured` | Object storage is not configured: set the `S3_*` variables. |
| `storage.unreachable` | Object storage does not answer: check the `seaweedfs` container and the `S3_ACCESS_KEY` / `S3_SECRET_KEY` pair. |
| `database.ok` | The database is reachable. |
| `database.unreachable` | The database does not answer: check the `postgres` container and `POSTGRES_PASSWORD`. |
| `clock.ok` | The server clock is within the window of real time. |
| `clock.skewed` | The clock is off: turn on NTP on the host. |
| `reverse_proxy.ok` | No proxy header problems seen. |
| `reverse_proxy.not_trusted` | Requests carry `X-Forwarded-For` from a proxy that is not trusted: set `TRUSTED_PROXY_CIDRS` to the proxy's own address. |
| `reverse_proxy.broad` | `TRUSTED_PROXY_CIDRS` covers a wide range: list only the proxy's address. |
| `websocket.ok` | A browser opened `/api/ws` and it upgraded (the item shows when). |
| `websocket.unchecked` | Neutral: no browser has opened the live connection since the server started; reload the Status tab. |
| `websocket.failing` | Browsers keep opening `/api/ws` without the upgrade headers: enable WebSocket in the reverse proxy (`Upgrade: websocket`, `Connection: Upgrade`). |
| `network.cgnat` | The WAN address is in `100.64.0.0/10` (carrier-grade NAT): port forwarding cannot work. See [Can I host at home?](#can-i-host-at-home). |
| `network.no_ipv4` | No public IPv4 while IPv6 works (DS-Lite): port forwarding over IPv4 cannot work. See [Can I host at home?](#can-i-host-at-home). |
| `network.address_mismatch` | The host's own address differs from the one the internet sees: set `WEBRTC_NAT_1TO1_IP` to the host address if it is reachable from outside, or forward the voice ports on the gateway in front. |

### The doctor command

For installations managed over SSH there is no browser tab to read: run

```sh
docker compose exec app mnema-talk doctor
```

It prints the checks above, one line each with `[ ok ]`, `[warn]` or `[FAIL]`
and the fix underneath every amber or red one, and exits with status 1 when a
check is red (0 otherwise), so it also fits a script. It measures what a second
process can: the configuration, the voice address (a STUN request, as at
startup), the database, object storage, the clock, and whether the running
server holds the voice and relay ports. The *Reverse proxy* check needs live
requests and stays in the Status tab. Without a running server in the container
the voice and relay checks are skipped and say so. The setup assistant runs it
for you with `mnema-talk setup --start`.

The tab is gated by the `ops.status` feature key in `/api/health` and backed by `GET /api/admin/status` (restricted to the Owner and members with the *Administrator* permission; other users receive `403 Forbidden`).

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
| `MnemaMemoryHigh` | Resident memory stays above 800 MB (about 75% of the default limit) for 15 minutes. |
| `MnemaGoroutineLeak` | More than 5000 goroutines for 15 minutes. |
| `MnemaServerErrors` | More than one 5xx every 10 seconds for 10 minutes. |
| `MnemaSlowRequests` | The 95th percentile API latency (uploads and media excluded) is above one second for 15 minutes. |
| `MnemaIceFailures` | More than 20% of voice connections fail ICE over 30 minutes. |
| `MnemaStorageErrors` | More than 5 object storage errors in 15 minutes. |
| `MnemaGoroutinePanic` | The server recovered from a panic in the last 15 minutes (`mnema_goroutine_panics_total`). |
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
| `mnema_ratelimit_hits_total{limiter}` | counter | Requests refused with 429, by limiter: `api_user` (600 a minute per member for requests that change something), `api_user_read` (3000 a minute per member for GET, HEAD and OPTIONS; reads have their own budget so browsing never uses up what a member may post), `upload`, `link_preview`, `poll_create` (10 a minute), `poll_vote`, `pin` (60 pins or unpins a minute), `emoji_upload` (10 per 10 minutes), `edit_history`, `export_download`, `dm_start` (new conversations), `block`, `report`, `saved`, `forward`, `thread_follow`, `slash_nick`, `test_mail`, `wiki`, `talk`, `talk_create`, `event_comment`, `later_change`, `self_roles`, `discord_import`, `connection_probe`, `speed_test`, the bot, webhook, gateway, push, e-mail and password-reset limiters, `ws_user` and `ws_ip`, and `other` for the few without a name. |
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
| `mnema_sfu_retransmitted_packets_total` | counter | Packets actually resent after a NACK. The history they come from is sized by bytes and shrinks as the server fills up ([architecture](architecture.md#capacity)), so a request for a packet that is no longer remembered is counted in `nack_packets` but not here. |
| `mnema_sfu_retransmit_dropped_total` | counter | Packets a subscriber asked for that were not resent: refused by the per-subscriber retransmission budget (a share of 25% of the connection's video bitrate, at least 128 KB/s) or asked faster than the server answers (each connection answers its NACKs one at a time, 16 waiting at most). A steady rate means subscribers are losing more than the server is willing to repair, or a subscriber is flooding NACKs: look at the downlinks and at host overload. |
| `mnema_sfu_keyframe_requests_total` | counter | PLI and FIR feedback from viewers. |
| `mnema_sfu_upload_limit_bits`, `mnema_sfu_upload_egress_bits`, `mnema_sfu_upload_capped_viewers` | gauge | `VOICE_UPLOAD_LIMIT_MBIT` in bits per second (0 = off), the media the server sends now (smoothed; measured only while a limit is set) and the viewers held below their best layer by the limit. |
| `mnema_sfu_upload_unmet_total` | counter | Checks that found the upload above the limit with nothing left to step down (see `VOICE_UPLOAD_LIMIT_MBIT`). |
| `mnema_sfu_layer_switches_total` | counter | Simulcast layer changes of screen share viewers. |
| `mnema_sfu_camera_layer_switches_total` | counter | Simulcast layer changes of camera viewers (a small tile moving to the thumbnail, the stage to the best layer, or the server moving a viewer down under loss). The admin overview's quality switches add both. |
| `mnema_sfu_bandwidth_estimate_reports_total{range}` | counter | Bandwidth estimates taken for viewers whose layer of a screen share or camera the server chooses, by range (`lt0.5m` ... `ge16m`, bits per second); `rate()` per range shows how the downlinks are distributed. |
| `mnema_sfu_bandwidth_layer_moves_total` | counter | Moves to a lower layer caused by the bandwidth estimate (those caused by loss, round trip time or keyframe requests are only in the layer switch counters). |
| `mnema_sfu_ice_transitions_total{state}` | counter | `connected`, `failed`, `disconnected` ICE transitions. A rising `failed` share means clients cannot reach the UDP port or TURN. |
| `mnema_sfu_ingress_dropped_total{reason}` | counter | Publisher packets dropped over the ingress limit: `rate` (a sender above its budget) or `closed` (the peer was already closing); `rtcp_ssrcs` counts media connections closed because their RTCP named too many SSRCs nothing reads, or because the SFU made more than 32 768 senders on them (see SECURITY.md, media connection limits). |
| `mnema_sfu_peer_joins_total` | counter | Media connections created. |
| `mnema_turn_events_total{event}` | counter | The built-in TURN relay: `allocation`, `relay_opened` and the refused attempts `auth_failed`, `forbidden`, `rate_limited`, `quota_user`, `quota_total`, `peer_denied`, `packet_blocked`, `throttled`. A steady climb of `peer_denied` or `packet_blocked` means someone is probing the relay; it cannot get anywhere. |
| `mnema_turn_allocations` | gauge | Sessions relayed by the built-in TURN relay right now. |
| `mnema_turn_relayed_bytes_total{direction}`, `mnema_turn_relayed_packets_total{direction}` | counter | Bytes and packets the relay forwarded, `to_sfu` and `from_sfu`. |
| `mnema_db_connections_total\|acquired\|idle\|max` | gauge | Database pool. `mnema_db_acquires_total`, `..._waited_total`, `..._canceled_total`, `mnema_db_acquire_seconds_total` count acquisitions, those that had to wait, those abandoned and the time spent. |
| `mnema_storage_up` | gauge | 1 when object storage answers (the same check as `/api/health`), else 0. |
| `mnema_s3_operations_total{op,result}` | counter | Object storage calls (`upload`, `get`, `delete`, `list`) by `ok`/`error`. |
| `mnema_webhook_events_dropped_total{reason}` | counter | Outgoing webhook events dropped or expired unsent: `webhook_cap` and `global_cap` (queue limits), `buffer_full`, `channel_share` and `store_error` (only `VOICE_STATE_UPDATE` passes the in-memory buffer; message and reaction events are written with their change) and `expired` (older than the retry window). A rising count means a receiver is down or too slow. |
| `mnema_push_deliveries_total{outcome}` | counter | Web Push deliveries: `ok`, `gone` (the browser ended the subscription; it is removed), `retry` (temporary refusal, retried once), `rejected`, `error` (no answer: blocked address, DNS, connection, TLS, timeout) and `dropped` (a full queue). A rising `rejected` share right after a restore means the VAPID key changed. |
| `mnema_job_duration_seconds{job}` | histogram | How long one run of a periodic maintenance job took. `job` is `session_sweep`, `status_expiry`, `ban_expiry`, `unlock`, `export_sweep`, `slow_mode_prune`, `edit_history`, `auto_delete`, `poll_fold`, `later`, `audit_retention`, `community_events`, `push_sweep` or `avatar_reencode`. |
| `mnema_job_errors_total{job}` | counter | Runs of those jobs that returned an error. Stopping the process is not counted. |
| `mnema_job_panics_total{job}` | counter | Runs that panicked. The panic is logged and the job keeps its schedule; it is not also counted as an error. |
| `mnema_goroutine_panics_total{name}` | counter | Panics outside an HTTP request and outside those jobs: on a goroutine, timer or callback of the server. `name` is the fixed name of the place in the code (`ws_read`, `sfu_forward`, ...). It is logged as `goroutine panicked` with a stack. Work that belongs to one connection or one call participant closes only that connection or removes only that participant, and the server carries on; a panic in a process-wide loop ends the process after the log line, and the container restarts it. A place that panics over and over is counted every time but logged only five times a minute. Fatal runtime errors (concurrent map writes, a stack overflow, out of memory) cannot be recovered and still end the process without this count. Timers that belong to one member are recovered where that is safe: the end of a Talk seat after its grace time (`ws_voice_grace`; the seat is removed before any listener runs, and if the panic came earlier the removal is tried once more) and the delayed write of voice time (`ws_voice_time`; the seconds are put back for the next write). One timer still ends the process when it panics, because it fans out to several parties and nothing records who was already told: the announcement that a new member's wait has ended (`perm_wait_end`), like `perm_changed`, which enforces a permission change. The enforcement on live connections (`ws_perm_enforce`) fails closed: a panic in it, on a request or a timer, is counted and logged, and the members concerned (everybody when the set is unknown) are disconnected and removed from their Talks one by one, so they are checked again when they reconnect and a panic for one member does not spare the others. A panic inside that clean-up itself is not recovered by the hub: on a timer it ends the process, on a request the HTTP layer answers a 500 and the process lives, with the change already saved. The same holds for a panic in the clean-up that runs after a recovered panic. Goroutines that the WebRTC library (Pion) starts itself, such as its interceptors for sender reports, transport-wide feedback and NACK generation, run outside any recovery of ours and end the process if they panic; only the RTP writer of a call viewer is guarded (`sfu_viewer_write`), and that cannot be fixed in this code. Any value above 0 is a bug worth reporting. |

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

### Following one session through the log

A member can be connected more than once: two tabs, the desktop app next to
a browser, or a reconnect that overlaps the connection it replaces. Lines
that only name the member (`user`) cannot tell these apart, so every line
that belongs to one WebSocket connection also carries `conn_id`:

- the hub's lines: `ws connected`, `ws disconnected`, `voice join`,
  `voice leave` (also the leave that ends a grace period after the socket
  dropped), refused joins and `webrtc client diag`;
- the lines of the media connection that WebSocket made in a Talk
  (`sfu peer state`, `sfu ice connected`, `sfu track received`, unanswered
  offers, dropped candidates, ...). A rejoin makes a new media connection, so
  the old and the new one of the same member are told apart the same way;
- `goroutine panicked`, when the panic happened in work of one connection.

`conn_id` is the request id the server gave the upgrade request, so it
equals the `request_id` of that connection's `http` line for `/api/ws`
(which also has the address).

When the upgrade request came with an `X-Request-Id` of its own (your reverse
proxy sets one, or the client did), the server does not use it for
`conn_id`: whoever sends it could pick the id of another session. The
connection then gets a random `conn_id`, and its `ws connected` line carries
what was sent as `client_request_id` (at most 128 letters, digits, `.`, `_`
and `-`; anything else is replaced as for every request). That value is the
`request_id` of the `http` line, which links the two.

```sh
# 1. The member's connections and when each one came and went
docker compose logs app | grep -E '"msg":"ws (dis)?connected"' | grep '"user":"alice"'
# 2. Everything one of them did, in order
docker compose logs app | grep '"conn_id":"4f0c2a1e-..."'
# 3. Where it came from (with a proxy's X-Request-Id: the client_request_id of step 1)
docker compose logs app | grep '"request_id":"4f0c2a1e-..."'
```

The id is only in the server log. No client is told its own or anybody
else's, it is in no event and it is no metric label. Lines about a member as
a whole (a kick by a moderator, the write of voice time) and the panic of a
connection's retransmission worker (`sfu_nack`) carry none.

## Troubleshooting

### Account deletion and data exports

Members delete their own account and download their data under Account &
security (the Owner account cannot be deleted). The export is a ZIP of JSON
files with the member's profile, settings, messages, earlier versions of
their edits, reactions, poll votes, uploads, the polls they started, the
messages they pinned, their push devices (label and dates, no keys), the
custom emoji they uploaded, the members they blocked, the reports they sent,
their saved messages (ids, notes and times, not the saved text), the threads
they follow, their reminders and unsent scheduled messages, the devices
signed in, the direct message conversations they are in (their own messages
are in the message file), their RSVPs and event sign-ups, their event
comments, the wiki pages they saved (their text; the pages stay in the wiki),
audit entries about their account and their sign-in setup; nothing of other
members. The archive's own `README.txt` lists every file. Deleting keeps an anonymised
row and the member's messages without text, removes everything else (see
[SECURITY.md](../SECURITY.md)) and frees the username. Their blocks (in both
directions), saved list and thread follows go; a direct message conversation
nobody else is in is deleted with its messages and files, and in the others
their messages stay without text; reports keep their copies for the
moderators with the member's text blanked; files are removed from
object storage in the same request, and anything that could not be removed is
found by Admin > Media > orphan cleanup. The one exception is a picture a
wiki page still shows: it survives the erasure, unlinked from the person
(see [the wiki](wiki.md#privacy)). Exports are stored for at most 24 hours
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
the Owner in Admin > Members. Replacing `JWT_SECRET` (or `DATA_KEY`) without
naming the old value in `JWT_SECRET_PREVIOUS` (`DATA_KEY_PREVIOUS`) makes the
stored authenticator secrets unreadable, and so does emptying a `DATA_KEY`
that was in use. The server logs this at start and `mnema-talk doctor` shows
it: put the old value back as the previous one (or `DATA_KEY` back), or reset
each affected account so it can enrol again. See
[Rotating keys](#rotating-keys).

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

New to this? The [voice troubleshooting guide](troubleshooting-voice.md) walks
through the layers in order, explains `chrome://webrtc-internals` and has a
symptom table. The reference steps follow.

1. **UDP range forwarded?** `WEBRTC_UDP_PORT_MIN`–`MAX` (default
   `50000-50050/udp`) must be forwarded from the router to the host and
   published by Docker (compose does that). The reverse proxy does not carry
   voice.
2. **Announced address right?** The Status tab's *Voice address* item and the
   `webrtc announce` log line show it and where it came from (set by you, taken
   from `PUBLIC_URL`, or found with a STUN request). Behind
   NAT, when the automatic detection is wrong (carrier-grade NAT, a proxy in
   front of the server) or switched off, set `WEBRTC_NAT_1TO1_IP` to the public IP or a dynamic-DNS name **and**
   the server's LAN IP, e.g. `myhome.example.org,192.168.0.10`. Members in the
   LAN need the LAN IP when the router has no hairpin NAT: with the public
   address detected automatically, `WEBRTC_LAN_IP=192.168.0.10` adds just that
   (the setup assistant asks for it). No public IPv4 at all? See
   [Can I host at home?](#can-i-host-at-home).
   **A port in use** shows up in the log as `address already in use` with the
   port, the protocol and the variable that moves it (`PORT`,
   `WEBRTC_BUILTIN_TURN_PORT`, `WEBRTC_UDP_PORT_MIN`/`MAX`); stop the other
   program or pick a free port and forward that one.
3. **Connection log.** `sfu ice connected` shows the chosen local and remote
   address and the remote candidate type; `sfu ice failed`/`disconnected` and
   `sfu peer state ... failed` mean packets don't get through (firewall,
   wrong announced address).
4. **More detail.** `LOG_LEVEL=debug` adds the candidates each browser offers
   and the browsers' own connection diagnostics (`webrtc client diag`).
   Revert to the default afterwards.
5. **UDP blocked on the member's side** (company network, VPN, Opera's VPN or
   WebRTC protection): only TURN over TCP helps. The server's own relay does
   it (see [Built-in TURN relay](#built-in-turn-relay); forward its port on UDP
   and TCP), or use [coturn](#turn-coturn).
   The affected member sees "No media connection" in the app; the log has
   `webrtc client diag` at warn level with `"ice":"failed"` and the candidate
   types the browser gathered (no `relay` entry means no TURN was on offer).
6. **Works for you, not for someone else.** Compare your `sfu ice connected`
   line (`remote_type`) with theirs. If theirs is missing or `failed`, their
   network is the difference: ask for another network (a phone hotspot), the
   browser and whether a VPN is on. Make sure the instance runs a release that
   drops a failed peer and tells its owner (1.2.6 or newer); older ones leave
   the member in the call without media and without a message.
   See [IPv6, DS-Lite and CGNAT](#ipv6-ds-lite-and-cgnat).

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

`docker compose logs app` starts with one block that lists **every** required
setting that is missing or invalid, each with why it is needed and an example:

```
Mnema Talk cannot start - fix these settings in .env:
  - JWT_SECRET must be set to a random string of at least 32 characters in production: ...
  - S3_SECRET_KEY is required in production: ...
```

Fix them in `.env` and run `docker compose up -d`. The process exits with
status 78 (a refused configuration, not a crash) after a five second pause;
Docker restarts the container (`restart: unless-stopped`), so the block
repeats at that pace until the settings are right. The server deliberately
serves no "error page" meanwhile: a half-started process would be an
unauthenticated surface and every health check would have to special-case it.
Check an `.env` without starting anything with
`docker run --rm -v "$PWD:/out" ghcr.io/msch128/mnema-talk setup --check --dir /out`
(or `mnema-talk setup --check`). Other messages here: `TRUSTED_PROXY_CIDRS
contains an invalid entry`, or a migration failure (see [upgrade.md](upgrade.md)).
`mnema-talk doctor` prints the same block when the settings are wrong.

## Downgrading

> **Do not roll back below this release once roles or private channels exist.**
> Migration 0017 adds the tables that restrict access. A binary that does not
> know them (0.7.x before roles, or older) would ignore them and show every
> private channel to everyone. This release and later ones refuse to start on a
> database migrated by a newer version (CI checks this against the 0.8.16 and
> 0.9.0 images), but binaries from before that guard
> existed cannot. To go back, restore a backup taken before the update.
