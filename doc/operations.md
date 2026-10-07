# Operations

Running Mnema Talk with `docker compose`: configuration, reverse proxy, TURN,
storage, backups, monitoring and troubleshooting. For the update procedure
see [upgrade.md](upgrade.md); for the security model see
[SECURITY.md](../SECURITY.md).

## Configuration reference

All settings are environment variables, read and validated in
[`internal/config/config.go`](../internal/config/config.go). In the compose
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
| `TRUSTED_PROXY_CIDRS` | `127.0.0.0/8,::1/128` (compose: plus `172.16.0.0/12`) | CIDRs or IPs whose `X-Forwarded-For` is trusted. Each entry must parse. See [Reverse proxy](#reverse-proxy-and-trusted_proxy_cidrs). |
| `PORT` | `8080` | HTTP port, 1–65535. In compose the container always listens on 8080; `PORT` there is the **host** port of the mapping. |
| `BIND_ADDR` | `0.0.0.0` | Listen address (outside compose). |
| `LOG_LEVEL` | empty | `debug`, `info`, `warn` or `error`. Empty = `info` in production/staging, `debug` otherwise. `debug` adds voice connection details (offered addresses, browser diagnostics). |

### Sessions and admin

| Variable | Default | Meaning and validation |
|---|---|---|
| `JWT_SECRET` | none | Signs session cookies. In production/staging at least 32 characters and not a placeholder (`openssl rand -hex 32`). In development an empty value means a random per-process secret (sessions reset on restart). Changing it logs everyone out. |
| `SESSION_EXPIRY_HOURS` | `720` | Session lifetime, 1–8760. |
| `ADMIN_USERNAME` | `Herzog` | The administrator account, created on first start when no admin exists. |
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
| `MAX_UPLOAD_SIZE_MB` | `50` | Largest single upload, 1–2048. Your reverse proxy must allow at least this body size. |
| `MEDIA_RETENTION_DAYS` | `0` | `0` = never delete media automatically. A positive number deletes media older than that many days. Negative values are rejected. |
| `LINK_PREVIEWS_ENABLED` | `true` | The server fetches public pages for link cards (never private addresses); linked sites see the server's IP. |
| `API_DOCS_ENABLED` | `false` | Swagger UI at `/api/docs` and `/api/openapi.json` for signed-in members. |

### Voice (WebRTC)

| Variable | Default | Meaning and validation |
|---|---|---|
| `WEBRTC_UDP_PORT_MIN` / `WEBRTC_UDP_PORT_MAX` | `50000` / `50050` | UDP range of the SFU; `1 <= MIN <= MAX`. Compose publishes the same range; forward it in your router. |
| `WEBRTC_UDP_MUX_PORT` | `0` | Optional shared UDP port across media peers, separately bound on each local IPv4/IPv6 interface. `0` keeps per-peer range allocation. Otherwise it must be inside `MIN`–`MAX`, already published by Compose. Requires restart. Reduces port use, not outgoing bandwidth; no participant-capacity guarantee. With `0` each peer takes its own port(s), so the default 51-port range fits only about 25 people in calls at once; the app logs a hint at startup. |
| `WEBRTC_MAX_ROOM_PEERS` | `0` | Most members one voice room admits; `0` = no limit, negative is rejected. A newcomer to a full room gets `voice_kicked` with `reason: "room_full"`; a member already present (second tab, reload within the grace period) always gets back in. |
| `WEBRTC_NAT_1TO1_IP` | empty | Comma-separated IPs or host names announced to browsers. Behind a home router list the public address (or a dynamic-DNS name, re-resolved every 5 minutes) **and** the server's LAN IP. In production, an empty value with no TURN configured logs a warning at startup. |
| `WEBRTC_STUN_URLS` | empty | Optional STUN servers. Empty = no third-party STUN (named in the privacy policy when set). |
| `WEBRTC_TURN_URLS` | empty | Optional TURN URLs, e.g. `turn:turn.example.com:3478?transport=udp,turn:turn.example.com:3478?transport=tcp`. |
| `WEBRTC_TURN_SECRET` | empty | Shared secret with coturn (`use-auth-secret`). At least 16 characters when `WEBRTC_TURN_URLS` is set; no placeholder in production. |
| `WEBRTC_TURN_REALM` | compose: `turn.example.com` | Read by the coturn container only, not by the app. |

### Monitoring

| Variable | Default | Meaning and validation |
|---|---|---|
| `METRICS_TOKEN` | empty | Enables `GET /api/metrics` with `Authorization: Bearer <token>`; empty = 404. At least 24 characters when set. |

### Updates

| Variable | Default | Meaning and validation |
|---|---|---|
| `UPDATE_CHECK_ENABLED` | `true` | Ask GitHub every 30 minutes for the latest release (admin System tab). `false` = no outbound request. |
| `MNEMA_IMAGE` | compose: `mnema-talk:local` | Image the `app` service runs; the app reads it to tell whether a re-pull can reach the latest release. See the tag table in the [README](../README.md#updates). |
| `UPDATER_TOKEN` | empty | Turns on "Update now" through the updater sidecar (profile `autoupdate`). At least 32 characters of letters, digits and `._~+/=-`, no placeholder. Empty = feature off, `UPDATER_URL` ignored. |
| `UPDATER_URL` | `http://mnema-updater:8080` | Where the app reaches the sidecar: `http(s)` without credentials, query or fragment. |

### Legal and privacy page

| Variable | Default |
|---|---|
| `LEGAL_OPERATOR_NAME` | `Community Operator` |
| `LEGAL_OPERATOR_EMAIL` | `admin@example.com` |
| `LEGAL_OPERATOR_COUNTRY` | `Deutschland` |
| `LEGAL_PROJECT_NOTICE` | `Privates, nicht-kommerzielles Projekt` |

Shown in the privacy policy (`GET /api/legal`). Set your real details in
`.env` only.

### Container resources (compose only)

Read by `docker-compose.yml`, not by the app. All services also rotate their
logs at 3 x 10 MB.

| Variable | Default | Meaning |
|---|---|---|
| `APP_MEM_LIMIT` | `2g` | Memory cap of the app container, including its RAM-backed `/tmp`. |
| `APP_TMP_SIZE` | `1g` | Size of the app's `/tmp` tmpfs. Uploads above 8 MB are buffered there until they reach S3, so keep it above `MAX_UPLOAD_SIZE_MB` times the uploads you expect at once; a full `/tmp` fails uploads instead of exhausting host RAM. |
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

    client_max_body_size 60m;          # >= MAX_UPLOAD_SIZE_MB

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
  Docker bridge; the compose default `127.0.0.0/8,::1/128,172.16.0.0/12`
  covers Docker's default bridge range.
- **Proxy on another LAN host or a custom Docker subnet**: set
  `TRUSTED_PROXY_CIDRS` to its address or network, e.g. `192.168.0.20`.
- Never list ranges untrusted clients can connect from, and don't expose port
  8080 directly while private ranges are trusted: anyone connecting from a
  trusted range could set their own `X-Forwarded-For`. Publish only the
  proxy (bind the app to `127.0.0.1:8080:8080` when the proxy is on the same
  host, or firewall it).

`PUBLIC_URL` must be the `https://` URL users open; otherwise the CSRF origin
check rejects their requests and cookies lose `Secure`.

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
- `--verify` imports into a network-isolated disposable PostgreSQL 17 container
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
- Nightly via cron, see the [README](../README.md#backups).

## Health and metrics

- **`GET /api/health`** (public): `200 {"status":"ok","version":"X.Y.Z"}`
  when the database answers and object storage is reachable, otherwise `503`
  (`database unavailable` / `file storage unavailable`). The result is cached
  for 2 seconds, so it can't be used to flood the database. The compose
  healthcheck and [`scripts/health-check.sh`](../scripts/health-check.sh)
  (cron, Unraid notification, optional webhook) use it.
- **`GET /api/metrics`** (Prometheus text format), only when `METRICS_TOKEN`
  is set:
  `mnema_up`, `mnema_db_connections_total|acquired|idle|max`,
  `mnema_ws_online_users`, `mnema_sfu_video_rooms`,
  `mnema_sfu_screen_shares`, `mnema_sfu_cameras`.

  ```yaml
  scrape_configs:
    - job_name: mnema-talk
      metrics_path: /api/metrics
      scheme: https
      authorization:
        credentials: <METRICS_TOKEN>
      static_configs:
        - targets: ["chat.example.com"]
  ```
- **Admin → System** shows version and commit, database and migration state,
  storage, voice/SFU and server resources, and the update check.

## Logs

The app logs JSON lines to stdout:

```sh
docker compose logs -f app            # or: make logs
docker compose logs app | grep '"sfu'  # voice connection events
docker compose logs app | grep '"audit":"self_update"'
```

Compose rotates every container's log at 3 x 10 MB, so older lines drop off;
ship them elsewhere if you need a longer history.

At startup the app logs `webrtc announce` with the announced IPs and the
port range, or `webrtc sfu unavailable, voice disabled` when the SFU could
not start (e.g. the UDP range is in use).

## Troubleshooting

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
