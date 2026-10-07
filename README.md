# Mnema Talk

A lightweight, private, single-server Discord alternative: text channels,
instant-join voice hangouts and high-quality screen sharing. It is a
single Go binary with the web app and a WebRTC SFU built in.

## Features

**Chat**

- **One community, no guilds**: categories with text channels and voice
  channels. Every voice channel has its own text chat as well.
- **Text chat** over WebSocket: Markdown, replies that quote and jump to the
  original (even thousands of messages back), threads, reactions, emoji,
  @mentions, file and image attachments, link cards, search, typing notices,
  unread and mention markers, per-channel notification settings and live
  presence with status text.

**Talk (voice channels)**

- **Hangout spots**: click a voice channel and you are in, no ringing. A page
  reload within 30 seconds puts you back into the call.
- **Voice**: voice activity or push-to-talk (while the tab has focus), noise
  suppression, per-person volume and mute, output device choice, mic test and
  sound effects.
- **Discord-like Talk view**: a participant grid that fits the free space, a
  **stage** for the screen share or camera you focus, cameras on the stage,
  full screen (double-click or `F`), **picture-in-picture** that keeps
  playing while you read a text channel, an option to hide participants
  without video, and the voice channel's chat as a resizable panel under the
  stage (deep link `/v/<id>/chat`).
- **Screen sharing** up to source quality (e.g. 1440p/4K at 60 FPS, hardware
  encoding in the browser). Watching is opt-in: a share is only sent to the
  people who click "Watch", and everyone sees who is watching. The sharer's
  **quality menu** sets mode (Gaming, Screen, Custom), resolution, frame rate
  and stream sound, with live codec and bitrate stats; every new share starts
  at 1080p / 30 FPS. Viewers set the **stream's volume** separately from the
  person's voice (0–100 %, default 50 %, remembered per sharer). One share per
  person; starting another one replaces it.
- The Pion-based SFU forwards RTP packets and never transcodes (see
  [doc/architecture.md](doc/architecture.md)).

**Administration**

- **Invite-only**: no public registration; the admin (default `Herzog`,
  configurable) creates invite codes.
- **Sidebar management** for admins: drag and drop channels and categories
  (mouse, pen, touch) or move them with `Alt+↑/↓`; every move is saved at
  once and can be undone from a toast. Context menus create, duplicate and
  edit channels and categories, collapse or expand them and mark them read.
- **Admin console**: users (disable, kick, reset password, revoke sessions),
  channel layout, invites, media (usage, pruning) and a **System** tab with
  version, health of database, storage and voice, the update check against
  GitHub releases and an optional "Update now" (see [Updates](#updates)).
- **Privacy-minded defaults**: no third-party STUN unless configured, no
  automatic data deletion unless enabled, generic legal/privacy page driven by
  env vars. The security model is summarised in [SECURITY.md](SECURITY.md).
- UI in German and English (chosen per account).

## Architecture

```
            Browser (Vue 3 SPA)
     HTTPS / WSS │        │ UDP (DTLS-SRTP)
                 ▼        ▼
   ┌───────────────────────────────────────┐
   │ mnema-talk  (single Go binary)        │
   │  - embedded web app (go:embed)        │
   │  - REST API + WebSocket hub           │
   │  - Pion WebRTC SFU                    │
   │  - embedded SQL migrations            │
   └──────────┬─────────────────┬──────────┘
              ▼                 ▼
      PostgreSQL 17       SeaweedFS (S3 API)
      (data)              (uploads, avatars)
```

`docker compose` runs three containers: `app`, `postgres` and `seaweedfs`
(plus `coturn` and the updater sidecar when you enable their profiles). Any
S3-compatible store can replace SeaweedFS.

| Part      | Tech |
|-----------|------|
| Backend   | Go, chi, pgx, gorilla/websocket, Pion WebRTC v4, AWS SDK v2 (S3) |
| Frontend  | Vue 3, Pinia, Vite, Tailwind CSS (`web/`) |
| Storage   | PostgreSQL 17, S3 (SeaweedFS by default) |

More documentation in [`doc/`](doc/):

- [doc/architecture.md](doc/architecture.md): components, data flow for chat
  and voice, frontend structure, design decisions
- [doc/operations.md](doc/operations.md): configuration reference, reverse
  proxy, TURN, backups, monitoring, troubleshooting
- [doc/upgrade.md](doc/upgrade.md): upgrade notes per version and the update
  procedure
- [doc/desktop-app.md](doc/desktop-app.md): plan for a desktop app with global
  push-to-talk

## Quickstart (Docker Compose)

```sh
cp .env.example .env
# edit .env: set PUBLIC_URL, JWT_SECRET (openssl rand -hex 32),
# POSTGRES_PASSWORD, S3_ACCESS_KEY / S3_SECRET_KEY, ADMIN_INITIAL_PASSWORD
docker compose up -d --build      # or: make up
```

Open `PUBLIC_URL`, log in as the admin, and create invite codes for your users.
`ADMIN_INITIAL_PASSWORD` is required for the first production start; it is only
used to create the admin. Change it after the first login (and then remove it
from `.env`). In development an empty value generates a password and logs it once.

## Development

Requirements: Go (see `go.mod`), Node 24 + npm, Docker, GNU make (on Windows use
Git Bash or WSL). Run `make help` for all targets.

```sh
make install-hooks   # pre-commit: gitleaks + gofmt
make dev             # postgres + seaweedfs in Docker (docker-compose.dev.yml publishes
                     # them on 127.0.0.1), server on :8080 via go run (reads .env)
cd web && npm run dev   # Vite dev server on http://localhost:3000, proxies /api to :8080
```

For the Vite dev server, allow its origin in `.env`:

```sh
CORS_ALLOWED_ORIGINS=http://localhost:8080,http://localhost:3000
DATABASE_URL=postgres://mnema:<password>@localhost:5432/mnema_talk?sslmode=disable
```

`make build` produces `bin/mnema-talk` with the web app embedded (`make web`
builds only `web/dist`).

## Testing

```sh
make test              # Go unit tests (-race)
make test-integration  # Go integration tests: starts postgres:17-alpine via Docker,
                       # or uses TEST_DATABASE_URL if set
make test-web          # vitest
make coverage          # Go (unit + integration) and web coverage: coverage.out,
                       # web/coverage/lcov.info; CI shows the totals in the job summary
make lint              # gofmt, go vet, eslint
make check             # everything CI runs: lint, tests, govulncheck, builds,
                       # npm audit, docker build
make e2e               # Playwright (Chromium) against the real binary, see below
```

### Browser tests (e2e)

`make e2e` (= `e2e/run.sh`) builds the web app and the binary, starts a
throwaway PostgreSQL and SeaweedFS with Docker on loopback ports, runs the
Playwright specs in `e2e/tests/` with fake camera/microphone devices and tears
everything down again. `SKIP_BUILD=1` reuses the last build,
`E2E_INSTALL_BROWSER=1` installs Chromium first and `E2E_ONLY=<spec>` runs one
file.

**Without Docker** (e.g. in a sandbox): start PostgreSQL 17 and a SeaweedFS
S3 gateway any other way, run the binary with the environment `e2e/run.sh`
uses (`APP_ENV=development`, a fresh `JWT_SECRET`, `ADMIN_INITIAL_PASSWORD`,
`DATABASE_URL`, `S3_*`, `UPDATE_CHECK_ENABLED=false`,
`WEBRTC_NAT_1TO1_IP=127.0.0.1`, ...), then point Playwright at it:

```sh
cd e2e && npm ci
E2E_BASE_URL=http://127.0.0.1:58080 E2E_ADMIN_USER=Herzog \
  E2E_ADMIN_PASSWORD=<the admin password you set> npx playwright test
```

The screen share specs need a Chromium with an H.264 encoder (CI's Chrome
has one; a locally installed open-source Chromium may not).

### CI pipeline

On every pull request and every push to `main`, `.github/workflows/ci.yml` runs
these jobs in parallel (rough wall times with warm caches):

| Job | What | Time |
|-----|------|------|
| `secret-scan` | gitleaks (pinned, checksum verified) over the tree and the history | < 1 min |
| `backend` | gofmt, go vet, OpenAPI check, unit tests, unit + integration tests with coverage (Postgres service), build | 4–6 min |
| `govulncheck` | Go vulnerability scan | 1–2 min |
| `frontend` | eslint, vitest with coverage, build, `npm audit` | 2–3 min |
| `docker` | image build for linux/amd64 + linux/arm64, not pushed | 2–4 min |
| `e2e` | Playwright smoke test against the real binary | 4–6 min |

`codeql.yml` (actions, Go incl. tests and the integration tag, JS) runs on the
same events plus weekly. Caches: Go modules and build cache per job (keyed on
`go.sum` and the Go version), npm (`setup-node`), Playwright browsers (keyed on
the Playwright version) and Docker layers (GitHub Actions cache, scope `image`,
shared with the release build). A new push to a pull request cancels its
running CI; runs for pushes to `main` always finish, because the release
workflow starts from their result. Every job has a `timeout-minutes`; tests are
never retried.

## Releases

Releases are automated with [release-please](https://github.com/googleapis/release-please)
(`.github/workflows/release.yml`) and driven by conventional commits
(`feat:` = minor, `fix:` = patch, `feat!:` / `BREAKING CHANGE:` = major). Until
1.0.0 the project stays on `0.x`: `feat:` and `fix:` bump the patch version and
breaking changes bump the minor version. 1.0.0 is released deliberately with a
`Release-As: 1.0.0` commit footer.

1. After CI passes on a push to `main`, release-please opens or updates a
   **release PR** that bumps `version.txt` and `CHANGELOG.md`. Only the CI run
   for the commit that is still `main`'s tip releases; if `main` moved on, the
   run for the newer commit decides.
2. The release workflow merges that PR right away and publishes the GitHub
   Release and tag `vX.Y.Z` (first release: `0.1.0`), so every green push to
   `main` that contains a `feat:` or `fix:` becomes a release.
3. The same workflow then builds the image and pushes it to GHCR:
   `ghcr.io/msch128/mnema-talk:X.Y.Z`, `:X.Y` and `:latest` (linux/amd64 and
   linux/arm64), with OCI labels,
   provenance and SBOM. The version and commit are baked into the binary
   (`internal/version`, via the `VERSION`/`REVISION` build args) and the same
   version into the web app, so the app can tell browsers when a newer
   version is running.

## Configuration

All settings are environment variables; `.env.example` documents every one of
them with safe placeholder values, and
[doc/operations.md](doc/operations.md#configuration-reference) lists each with
its default and validation. The server refuses to start on an invalid value
or, in production, on a placeholder secret. Notable ones:

- `PUBLIC_URL`: the URL browsers use; `https://` enables Secure/`__Host-` cookies.
- `CORS_ALLOWED_ORIGINS`: extra allowed origins (defaults to `PUBLIC_URL`).
- `TRUSTED_PROXY_CIDRS`: proxies whose `X-Forwarded-For` is trusted. The app
  defaults to loopback only; `docker-compose.yml` adds Docker's default bridge
  range `172.16.0.0/12`. If your reverse proxy runs on another host or in a
  Docker network with a custom subnet, set it to that address or network.
- `LOG_LEVEL`: `debug`, `info`, `warn` or `error` (empty = by `APP_ENV`).
- `MEDIA_RETENTION_DAYS`: `0` (default) = never delete media automatically.
- `API_DOCS_ENABLED`: `true` serves the API reference (Swagger UI) at `/api/docs` and the OpenAPI document at `/api/openapi.json` to signed-in members; off by default.
- `WEBRTC_UDP_PORT_MIN/MAX`, `WEBRTC_NAT_1TO1_IP`, `WEBRTC_STUN_URLS`: voice networking.
- `LEGAL_*`: operator details shown in the privacy policy.
- `UPDATE_CHECK_ENABLED`: `true` (default) lets the server ask GitHub every 30
  minutes for the latest release; admins see it in the System tab. `false`
  = no outbound request.

## Deployment behind a reverse proxy

Terminate TLS in a reverse proxy such as Caddy and forward HTTP and WebSocket
traffic to the app:

```caddy
chat.example.com {
    reverse_proxy app-host:8080
}
```

- The proxy must pass the client IP in `X-Forwarded-For` (Caddy does this by
  default), and its address must be within `TRUSTED_PROXY_CIDRS`. Otherwise
  rate limiting and login lockout see only the proxy's IP. The compose file
  trusts loopback and the Docker bridge range (`172.16.0.0/12`), which covers a
  proxy on the same host or in a container; a proxy on another machine needs
  its address in `TRUSTED_PROXY_CIDRS`. Don't list ranges that untrusted
  clients can connect from: they could fake their IP.
- Voice and video do **not** go through the proxy. Forward the UDP range
  `WEBRTC_UDP_PORT_MIN`–`WEBRTC_UDP_PORT_MAX` (default `50000-50050/udp`) from
  your router/firewall to the host, and set `WEBRTC_NAT_1TO1_IP` to the public
  IP when the server is behind NAT.
- Set `APP_ENV=production`, a strong `JWT_SECRET` and an `https://` `PUBLIC_URL`.
- Do not use `docker-compose.dev.yml` in production; the base compose file
  does not publish PostgreSQL or SeaweedFS.
- The SeaweedFS S3 gateway only accepts `S3_ACCESS_KEY` / `S3_SECRET_KEY`
  (letters, digits and `._~+/=-`; the secret at least 8 characters). The
  container writes its identity config from them at every start, so changing
  the keys in `.env` and running `docker compose up -d` updates both sides.

### Ports

What to open, assuming the default settings and a reverse proxy on the same
network:

| Port | Protocol | Service | Forward from the internet? |
|---|---|---|---|
| 443 (and 80 for certificates) | TCP | Reverse proxy (HTTPS, WebSocket) | Yes |
| 50000–50050 | UDP | App: voice, video and screen share (`WEBRTC_UDP_PORT_MIN/MAX`) | Yes |
| 3478 | UDP + TCP | coturn, only with the `turn` profile | Yes, if you run it |
| 49152–49200 | UDP | coturn relay ports, only with the `turn` profile | Yes, if you run it |
| 8080 | TCP | App HTTP (`PORT`), reached by the proxy | No, proxy only |
| 5432 | TCP | PostgreSQL | No, Docker network only |
| 8333 | TCP | SeaweedFS S3 API | No, Docker network only |

- The compose file publishes `8080/tcp` on all host interfaces. Don't forward
  it from the router; if the host is reachable from outside, restrict it with
  the host firewall or change the mapping to `127.0.0.1:8080:8080` when the
  proxy runs on the same host.
- If you change `WEBRTC_UDP_PORT_MIN/MAX`, forward the new range; Docker
  publishes the same range.
- Users behind strict firewalls that block UDP can only join voice through
  TURN (`WEBRTC_TURN_URLS`, see `.env.example`).

### Running a released image

Instead of building on the server, use a published image: set `MNEMA_IMAGE`
in `.env` (the compose file uses it for the `app` service) and start without
`--build`:

```sh
# .env
MNEMA_IMAGE=ghcr.io/msch128/mnema-talk:0.4   # or :0.4.1 (pinned) or :latest

docker compose pull app && docker compose up -d
```

Then update with `docker compose pull app && docker compose up -d app`, or
from the admin console (see [Updates](#updates)).

### Supported platforms

Images are published for **linux/amd64** and **linux/arm64** (e.g. Raspberry Pi
4/5 with a 64-bit OS); Docker picks the right one. The rest of the stack
(`postgres:17-alpine`, `chrislusf/seaweedfs`, `coturn/coturn`) is published for
both as well. **linux/arm/v7** (32-bit ARM) is not built: those images exist
for it too, but the server doesn't compile for 32-bit targets yet (a 64-bit
constant in `internal/db` overflows `int`). Builders cross-compile the Go
binary on the build machine, so a multi-arch build needs no QEMU:
`docker buildx build --platform linux/amd64,linux/arm64 .`

### Pull-based deployment

Deployment to a LAN/self-hosted server stays **manual and pull-based**: GitHub's
hosted runners cannot reach a server inside a private network, so nothing
pushes to it. Bump the tag (or pull `:X.Y` / `:latest`) when you want to update.
Database migrations run automatically at startup.

If the GHCR package is private (the default for a newly published package),
either make it public once under the package's settings on GitHub, or run
`docker login ghcr.io` on the server with a token that has `read:packages`.

## Updates

**Everyone: reload notice.** Every page knows the version it was built as and
the server tells each connection its version. After an update, open tabs show
"A new version is available – click here to reload". Nothing reloads by
itself; a call in progress reconnects automatically after the reload.

**Admins: update check.** The server asks GitHub every 30 minutes for the
latest release (`UPDATE_CHECK_ENABLED`, default on; conditional requests, so
it stays far below GitHub's rate limit). Admin → **System** shows the running
version and commit, the latest release with its notes, health of database,
storage and voice, and the update command. A dot on the ⋯ menu and a badge
on "Admin console" announce a new release. Nothing is ever updated
automatically.

**Optional: update from the admin console.** "Update now" asks a separate
updater sidecar to pull the app image again and restart the app (everyone is
disconnected for about 30–60 seconds). It is **off by default**; to enable it:

1. Run a released image: `MNEMA_IMAGE=ghcr.io/msch128/mnema-talk:0.4` (see
   below for the tag).
2. Set `UPDATER_TOKEN` in `.env` (`openssl rand -hex 32`, at least 32
   characters).
3. Start with the profile: `docker compose --profile autoupdate up -d`.

The confirm dialog asks for the admin's password again; the server allows one
update every 5 minutes and only while a newer release is known, and logs who
started which update (`"audit":"self_update"` in the app log). Read the
threat model in [SECURITY.md](SECURITY.md#self-update-sidecar) first: the
sidecar holds the Docker socket.

**Which tag?** The sidecar re-pulls the tag the app runs; it never switches
tags. That decides what "update now" can reach:

| `MNEMA_IMAGE` tag | Follows | Self-update |
|---|---|---|
| `:0.4` (major.minor) | all `0.4.x` releases | yes, until a breaking `0.5.0`; then change the tag by hand (recommended) |
| `:latest` | every release, breaking ones included | always |
| `:0.4.1` or `@sha256:…` | nothing (pinned) | no; the System tab shows the manual steps |
| `mnema-talk:local` (default) | built on the server | no; `git pull && docker compose up -d --build` |

Pinned tags are the most predictable (you choose every version and can roll
back by setting the old one); `:0.4` is the practical middle ground. Until
1.0.0, `feat:` and `fix:` releases bump the patch version, so a `:0.4` image
receives them, and breaking changes need a deliberate tag change. To roll
back, set the previous version (e.g. `:0.4.1`) and run `docker compose pull
app && docker compose up -d app`; database migrations are forward-only, so
check the changelog before going back across a release that changed the
schema.

**Manual update, step by step** (backup, compose file for the new tag, image,
pull, verify, rollback) and what changed between versions for operators:
[doc/upgrade.md](doc/upgrade.md).

## Backups

Everything stateful lives in two Docker volumes (PostgreSQL and SeaweedFS) plus
deployment configuration. `scripts/backup.sh` saves the data, `.env` and a
rendered uninterpolated `compose.yaml` into one timestamped folder
under `BACKUP_DIR` (default `$HOME/mnema-talk-backups`) and removes validated,
complete folders older than `KEEP_DAYS` (default 14). A backup contains your
secrets, so the script refuses a `BACKUP_DIR` inside a git checkout (`BACKUP_ALLOW_IN_REPO=1`
overrides that; `/backups/` is git-ignored). The app and SeaweedFS are stopped
before the database snapshot and media copy, so writes and retention pause
together; active calls disconnect. Previously running services resume afterward.
New backups include checksums and a completion marker. Retention runs after
successful backup and service recovery. Run from the deployment directory, for example
nightly via cron:

```sh
30 3 * * *  cd /path/to/mnema-talk && BACKUP_DIR=/srv/backups/mnema ./scripts/backup.sh >> /srv/backups/mnema/backup.log 2>&1
```

Copy the backup folder off the machine as well (another disk, NAS share or
cloud storage); a backup on the same disk does not survive a disk failure.

Check backup integrity and SQL import in an isolated disposable PostgreSQL
container, without touching live data (then test application functions on a
separately restored deployment):

```sh
./scripts/restore.sh /srv/backups/mnema/20261005-033000 --verify
```

Restore for real (stops the app, replaces database and media, starts again):

```sh
./scripts/restore.sh /srv/backups/mnema/20261005-033000
```

More on monitoring, health checks and troubleshooting voice connections:
[doc/operations.md](doc/operations.md).

## Security

See [SECURITY.md](SECURITY.md) for how to report vulnerabilities and a summary of
the security model. Contributors and AI agents: read [AGENTS.md](AGENTS.md)
first. This repository is public, so never commit secrets.

## License

Copyright (C) 2026 Marius Schröder ([msch128](https://github.com/msch128)).

Mnema Talk is licensed under the
[GNU Affero General Public License v3.0](LICENSE) (AGPL-3.0-only). You may
use, change and share it, also commercially, as long as you publish the
complete source code of your version under the same license. That includes
running a changed version as a network service: its users must be able to get
the source (the legal dialog in the app links to it; point `SOURCE_URL` in
`web/src/lib/thirdParty.js` at your own repository).

**Commercial license:** to use Mnema Talk, or parts of it, without the AGPL
obligations (for example in a closed-source product or service), get a
separate commercial license from the copyright holder. Contact
[msch128 on GitHub](https://github.com/msch128).

Contributions are accepted under the AGPL-3.0, and their authors agree that
Marius Schröder may also distribute them under a commercial license.
