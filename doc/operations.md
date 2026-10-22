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
| `TRUSTED_PROXY_CIDRS` | `127.0.0.0/8,::1/128` (compose: the same) | CIDRs or IPs whose `X-Forwarded-For` is trusted. Each entry must parse. See [Reverse proxy](#reverse-proxy-and-trusted_proxy_cidrs). |
| `PORT` | `8080` | HTTP port, 1–65535. In compose the container always listens on 8080; `PORT` there is the **host** port of the mapping. |
| `BIND_ADDR` | `127.0.0.1` in development, else `0.0.0.0` | Listen address (outside compose). `APP_ENV=development` refuses any non-loopback address. |
| `PUBLISH_ADDR` | `127.0.0.1` | Compose only: host address the app port is published on. |
| `LOG_LEVEL` | empty | `debug`, `info`, `warn` or `error`. Empty = `info` in production/staging, `debug` otherwise. `debug` adds voice connection details (offered addresses, browser diagnostics). |

### Sessions and admin

| Variable | Default | Meaning and validation |
|---|---|---|
| `JWT_SECRET` | none | Signs session cookies. In production/staging at least 32 characters and not a placeholder (`openssl rand -hex 32`). In development an empty value means a random per-process secret (sessions reset on restart). Changing it logs everyone out. |
| `SESSION_EXPIRY_HOURS` | `720` | Session lifetime, 1–8760. |
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
| `MEDIA_RETENTION_DAYS` | `0` | `0` = never delete media automatically. A positive number deletes media older than that many days. Negative values are rejected. |
| `LINK_PREVIEWS_ENABLED` | `true` | The server fetches public pages for link cards (never private addresses); linked sites see the server's IP. |
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
cosign verify ghcr.io/msch128/mnema-talk:0.7 \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity-regexp '^https://github\.com/msch128/mnema-talk/\.github/workflows/release\.yml@'
```

A passing check shows that this repository's release workflow produced the
digest; it says nothing about the code in it beyond that. Pinning the digest in
`MNEMA_IMAGE` (`ghcr.io/msch128/mnema-talk@sha256:...`) keeps a verified image
from changing under a tag. Images released before signing was added carry no
signature.

## Health and metrics

- **`GET /api/health`** (public): `200 {"status":"ok","version":"X.Y.Z"}`
  when the database answers and object storage is reachable, otherwise `503`
  (`database unavailable` / `file storage unavailable`). The result is cached
  for 2 seconds, so it can't be used to flood the database. The compose
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

```yaml
# mnema-alerts.yml
groups:
  - name: mnema-talk
    rules:
      - alert: MnemaDown
        expr: up{job="mnema-talk"} == 0
        for: 2m
        labels: {severity: page}
        annotations: {summary: "Mnema Talk does not answer the scrape (or the token is wrong)"}

      # /api/health is public and the metrics endpoint is up while the
      # database or storage is not, so check those through their metrics too.
      - alert: MnemaStorageDown
        expr: mnema_storage_up == 0
        for: 5m
        labels: {severity: page}
        annotations: {summary: "Object storage is unreachable: uploads fail and /api/health answers 503"}

      - alert: MnemaHealthFailing
        # needs blackbox_exporter probing https://chat.example.com/api/health
        expr: probe_success{instance="https://chat.example.com/api/health"} == 0
        for: 3m
        labels: {severity: page}
        annotations: {summary: "/api/health fails (database or storage)"}

      - alert: MnemaDatabasePoolExhausted
        expr: mnema_db_connections_acquired >= mnema_db_connections_max
        for: 5m
        labels: {severity: warn}
        annotations: {summary: "All database connections are busy: requests queue"}

      - alert: MnemaMemoryHigh
        # process_resident_memory_bytes is reported on Linux; the compose
        # limit is APP_MEM_LIMIT (see Container resources).
        expr: process_resident_memory_bytes > 800e6
        for: 15m
        labels: {severity: warn}
        annotations: {summary: "Resident memory above 800 MB (80% of the default 1g container limit) for 15 minutes"}

      - alert: MnemaGoroutineLeak
        expr: go_goroutines > 5000
        for: 15m
        labels: {severity: warn}
        annotations: {summary: "Unusually many goroutines: possible leak"}

      - alert: MnemaServerErrors
        expr: sum(rate(mnema_http_requests_total{class="5xx"}[10m])) > 0.1
        for: 10m
        labels: {severity: warn}
        annotations: {summary: "More than one 5xx every 10 seconds"}

      - alert: MnemaSlowRequests
        expr: histogram_quantile(0.95, sum by (le) (rate(mnema_http_request_duration_seconds_bucket{route!~"/api/channels/.*/upload|/api/media/.*"}[10m]))) > 1
        for: 15m
        labels: {severity: warn}
        annotations: {summary: "95th percentile API latency above one second"}

      - alert: MnemaIceFailures
        # failed ICE transitions relative to successful ones
        expr: |
          sum(increase(mnema_sfu_ice_transitions_total{state="failed"}[30m])) > 3
          and
          sum(increase(mnema_sfu_ice_transitions_total{state="failed"}[30m]))
            / clamp_min(sum(increase(mnema_sfu_ice_transitions_total{state="connected"}[30m])), 1) > 0.2
        for: 10m
        labels: {severity: warn}
        annotations: {summary: "Many voice connections fail: check UDP port range, WEBRTC_ANNOUNCE_IP and TURN"}

      - alert: MnemaStorageErrors
        expr: sum(increase(mnema_s3_operations_total{result="error"}[15m])) > 5
        labels: {severity: warn}
        annotations: {summary: "Object storage calls fail"}

      - alert: MnemaBackupTooOld
        # see "Backup age" below: the app cannot know when you last backed up
        expr: time() - mnema_backup_last_success_timestamp_seconds > 36 * 3600
        labels: {severity: warn}
        annotations: {summary: "No successful backup for 36 hours"}
```

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
| `mnema_ratelimit_hits_total{limiter}` | counter | Requests refused with 429: `api_user`, `upload`, `link_preview`, `other` (login, native clients and the rest). |
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
| `mnema_sfu_peer_joins_total` | counter | Media connections created. |
| `mnema_db_connections_total\|acquired\|idle\|max` | gauge | Database pool. `mnema_db_acquires_total`, `..._waited_total`, `..._canceled_total`, `mnema_db_acquire_seconds_total` count acquisitions, those that had to wait, those abandoned and the time spent. |
| `mnema_storage_up` | gauge | 1 when object storage answers (the same check as `/api/health`), else 0. |
| `mnema_s3_operations_total{op,result}` | counter | Object storage calls (`upload`, `get`, `delete`, `list`) by `ok`/`error`. |

## Orphaned media

Objects in storage that no media row refers to (a failed delete, a crash
during an upload) are invisible to members and only take space. Once a day
the app counts those older than 24 hours under `uploads/` and `avatars/` and
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
  docker exec mnema-talk /usr/local/bin/mnema-talk unlock Herzog   # one account
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
- **From the admin panel:** the Owner sees a "Login locked" badge on a locked
  account in Admin > Users and can click "Clear login lockout".

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
