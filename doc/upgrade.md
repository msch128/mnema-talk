# Upgrade notes

What operators need to know when moving between versions, and the update
procedure for a self-hosted (NAS) compose deployment. The full list of
changes is in [CHANGELOG.md](../CHANGELOG.md); the configuration is
described in [operations.md](operations.md#configuration-reference).

Database migrations are embedded in the binary and run automatically at
startup, in order, and are **forward-only**: an older version cannot use a
schema a newer one migrated. Take a backup before every update.

## Update procedure (compose, released image)

Run in the deployment directory (where `docker-compose.yml` and `.env` are).
Replace the version and paths with yours.

```sh
cd /path/to/mnema-talk
NEW=0.4.0                                  # version to install

# 1. Back up database, media and .env (see operations.md, "Backups")
BACKUP_DIR=/path/to/backups ./scripts/backup.sh

# 2. Note what runs now, for a rollback
grep '^MNEMA_IMAGE=' .env                  # e.g. ghcr.io/msch128/mnema-talk:0.3.3
curl -fsS http://127.0.0.1:8080/api/health # {"status":"ok","version":"..."}
cp docker-compose.yml docker-compose.yml.bak

# 3. Fetch the compose file (and scripts) of the new tag
base=https://raw.githubusercontent.com/msch128/mnema-talk/v$NEW
curl -fsSLo docker-compose.yml        "$base/docker-compose.yml"
curl -fsSLo .env.example.new          "$base/.env.example"
curl -fsSLo scripts/backup.sh         "$base/scripts/backup.sh"
curl -fsSLo scripts/restore.sh        "$base/scripts/restore.sh"
curl -fsSLo scripts/health-check.sh   "$base/scripts/health-check.sh"
chmod +x scripts/*.sh
# New or changed variables since your .env was written:
diff <(grep -o '^[A-Z_0-9]*=' .env | sort) <(grep -o '^[A-Z_0-9]*=' .env.example.new | sort)

# 4. Point the app at the new image
if grep -q '^MNEMA_IMAGE=' .env; then
  sed -i "s|^MNEMA_IMAGE=.*|MNEMA_IMAGE=ghcr.io/msch128/mnema-talk:$NEW|" .env
else
  echo "MNEMA_IMAGE=ghcr.io/msch128/mnema-talk:$NEW" >> .env
fi

# 5. Check the configuration, pull and start
docker compose config -q
docker compose pull app
docker compose up -d              # add --profile turn / --profile autoupdate if you use them

# 6. Verify
docker compose ps                 # app "healthy" after ~15-30 s
curl -fsS http://127.0.0.1:8080/api/health
docker compose logs --since 10m app | grep -E 'migration applied|"level":"(WARN|ERROR)"'
```

Then open the app: other open tabs show a reload notice; voice calls
reconnect after the reload. Admin → System shows the new version.

If you follow a minor line instead (`MNEMA_IMAGE=ghcr.io/msch128/mnema-talk:0.4`),
step 4 is unnecessary within that line: `docker compose pull app && docker
compose up -d app`, or "Update now" in the System tab when the updater
sidecar is enabled (see the [README](../README.md#updates)). A new minor
line (e.g. `0.4` → `0.5`) always needs this full procedure.

If you build on the server instead (`MNEMA_IMAGE=mnema-talk:local`):
`git fetch --tags && git checkout v$NEW && docker compose up -d --build`
replaces steps 3–5.

### Rollback

```sh
cp docker-compose.yml.bak docker-compose.yml
sed -i "s|^MNEMA_IMAGE=.*|MNEMA_IMAGE=ghcr.io/msch128/mnema-talk:0.3.3|" .env   # the old version
docker compose pull app && docker compose up -d
```

If the new version applied a migration (`migration applied` in the log), the
old version refuses to start on the newer schema ("the database was migrated
by a newer version"): restore the backup from step 1 as well (`./scripts/restore.sh /path/to/backups/<timestamp>`), which replaces
the database and media with the state before the update.

## Version notes

### 0.3.x → 0.4.0

Operator-relevant changes (0.3.1, 0.3.2, 0.3.3 and 0.4.0):

- **New compose file.** The app's image is now `${MNEMA_IMAGE:-mnema-talk:local}`
  instead of a fixed `mnema-talk:local`, the app carries the updater labels,
  and there is a new optional `updater` service (profile `autoupdate`) with
  an internal network `mnema-updater`. Fetch the compose file of the tag
  (step 3); an old file keeps working but cannot run released images.
- **Released images** on GHCR for `linux/amd64` and `linux/arm64`
  (`:X.Y.Z`, `:X.Y`, `:latest`). Set `MNEMA_IMAGE` to use one instead of
  building on the server. If the package is private, `docker login ghcr.io`
  with a `read:packages` token first.
- **New variables** (all optional, defaults are safe):
  - `UPDATE_CHECK_ENABLED` (default `true`): the server asks
    `api.github.com` every 30 minutes for the latest release; set `false` for
    no outbound request.
  - `MNEMA_IMAGE` (compose default `mnema-talk:local`): the image the app
    runs.
  - `UPDATER_TOKEN` (default empty = off) and `UPDATER_URL` (default
    `http://mnema-updater:8080`): "Update now" through the sidecar. The token
    needs at least 32 characters (`openssl rand -hex 32`); a short or
    placeholder token stops startup. Read
    [SECURITY.md](../SECURITY.md#self-update-sidecar) before enabling it.
- **Version reporting.** The binary and the web app carry their version;
  `/api/health` returns it, and browsers on an older build show a reload
  banner (never an automatic reload).
- **Voice channels have a text chat** (panel under the Talk stage, deep link
  `/v/<id>/chat`). Messages, uploads, threads and read state for voice
  channels use the existing tables; no migration.
- **Stream audio behaviour change.** A shared screen's sound now travels on
  its own line instead of being mixed into the sharer's microphone. Viewers
  control it with the stage's volume slider (0–100 %, default 50 %,
  remembered per sharer); the member menu's volume and mute affect only the
  voice. Every new share starts at 1080p / 30 FPS; the sharer changes quality
  in the stream's gear menu. Make sure everyone reloads after the update (the
  reload banner asks them to): a tab still running the old web app sends its
  screen sound mixed into the microphone, so viewers can't control it
  separately.
- **Migrations:** none since 0.1.16 (latest is `0012_message_count.sql`), so
  rolling back to 0.3.x needs no database restore unless the changelog of
  the release you install says otherwise.

### 0.1.16 → 0.3.0

No configuration, compose or schema changes. 0.3.0 brings the sidebar
management (drag and drop, `Alt+↑/↓`, context menus, duplicate channel) and
the Talk stage (cameras on the stage, viewers of a share, grid, full screen,
picture-in-picture). Update the image or rebuild; nothing else to do.

### 0.1.15 or older → 0.1.16

This release tightened configuration and storage security. Check these
before updating, or the app (or SeaweedFS) will refuse to start:

- **Stricter configuration validation.** In `production`/`staging` every
  public example value from `.env.example` is refused: `JWT_SECRET`,
  `S3_ACCESS_KEY`/`S3_SECRET_KEY`, the database password inside
  `DATABASE_URL` (i.e. `POSTGRES_PASSWORD`), `METRICS_TOKEN`,
  `WEBRTC_TURN_SECRET`, and `ADMIN_INITIAL_PASSWORD` in any environment. Also
  enforced: booleans must be `true`/`false`/`1`/`0`, `LOG_LEVEL` one of
  `debug|info|warn|error` (or empty), `SESSION_EXPIRY_HOURS` 1–8760, `PORT`
  1–65535, `APP_ENV` one of `production|staging|development|test`, and every
  `TRUSTED_PROXY_CIDRS` entry a valid CIDR or IP. `PUBLIC_URL` and CORS
  origins are normalised (no trailing slash or default port).
- **`TRUSTED_PROXY_CIDRS` default is loopback only** (before: loopback plus
  all private ranges, `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`,
  `fc00::/7`). The compose file adds
  `172.16.0.0/12` (Docker's default bridge range). A reverse proxy on another
  LAN host or in a Docker network with a custom subnet must be listed
  explicitly, otherwise all clients share the proxy's IP for rate limits and
  login lockout.
- **SeaweedFS requires S3 credentials.** The `seaweedfs` service now writes
  an S3 identity from `S3_ACCESS_KEY`/`S3_SECRET_KEY` and the gateway accepts
  only that pair (before, it allowed anonymous access on the Docker
  network). Both must be set, use only letters, digits and `._~+/=-`, and the
  secret needs at least 8 characters; otherwise the container exits. Use the
  compose file of the release.
- **Migrations** `0011_username_ci_unique.sql` (usernames unique regardless
  of case) and `0012_message_count.sql`. 0011 fails, and startup stops, if
  two accounts differ only in case: rename one of them in the database and
  start again. From this release on each applied migration's checksum is
  recorded, and startup stops if an applied migration file was changed.
- **Backups.** `scripts/backup.sh` refuses a `BACKUP_DIR` inside a git
  checkout (the backup holds `.env`) unless `BACKUP_ALLOW_IN_REPO=1`, and
  stops SeaweedFS briefly for a consistent media copy (`SEAWEED_LIVE=1` for
  the old behaviour).
