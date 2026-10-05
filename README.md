# Mnema Talk

A lightweight, private, single-server Discord alternative: text channels,
instant-join voice hangouts and high-quality screen sharing. It is a
single Go binary with the web app and a WebRTC SFU built in.

## Features

- **One community, no guilds**: categories with text and voice channels.
- **Text chat**: real-time messages over WebSocket, replies/threads, reactions,
  Markdown, file and image attachments, and live presence.
- **Replies**: Discord-style replies quote the original message and jump to it, even thousands of messages back.
- **Voice hangouts**: click a voice channel and you are in, no ringing. A page
  reload within 30 seconds puts you back into the call.
- **Screen sharing** up to source quality (e.g. 1440p/4K at 60 FPS, hardware
  encoding in the browser). The Pion-based SFU forwards RTP packets and never transcodes.
- **Invite-only**: no public registration; the admin (default `Herzog`,
  configurable) creates invite codes.
- **Privacy-minded defaults**: no third-party STUN unless configured, no
  automatic data deletion unless enabled, generic legal/privacy page driven by env vars.

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

`docker compose` runs three containers: `app`, `postgres` and `seaweedfs`. Any
S3-compatible store can replace SeaweedFS.

| Part      | Tech |
|-----------|------|
| Backend   | Go, chi, pgx, gorilla/websocket, Pion WebRTC v4, AWS SDK v2 (S3) |
| Frontend  | Vue 3, Pinia, Vite, Tailwind CSS (`web/`) |
| Storage   | PostgreSQL 17, S3 (SeaweedFS by default) |

## Quickstart (Docker Compose)

```sh
cp .env.example .env
# edit .env: set PUBLIC_URL, JWT_SECRET (openssl rand -hex 32),
# POSTGRES_PASSWORD, S3_ACCESS_KEY / S3_SECRET_KEY
docker compose up -d --build      # or: make up
docker compose logs app           # shows the generated admin password on first start
```

Open `PUBLIC_URL`, log in as the admin, and create invite codes for your users.
If `ADMIN_INITIAL_PASSWORD` is empty, a random password is generated and logged
**once**. Change it after the first login.

## Development

Requirements: Go (see `go.mod`), Node 22 + npm, Docker, GNU make (on Windows use
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
make lint              # gofmt, go vet, eslint
make check             # everything CI runs: lint, tests, govulncheck, builds,
                       # npm audit, docker build
```

CI (`.github/workflows/ci.yml`) runs gitleaks, the backend suite against a
Postgres service container, govulncheck, the frontend lint/test/build/audit and
a Docker image build on every push to `main` and every pull request.

## Releases

Releases are automated with [release-please](https://github.com/googleapis/release-please)
(`.github/workflows/release.yml`) and driven by conventional commits
(`feat:` = minor, `fix:` = patch, `feat!:` / `BREAKING CHANGE:` = major). Until
1.0.0 the project stays on `0.x`: `feat:` and `fix:` bump the patch version and
breaking changes bump the minor version. 1.0.0 is released deliberately with a
`Release-As: 1.0.0` commit footer.

1. After CI passes on a push to `main`, release-please opens or updates a
   **release PR** that bumps `version.txt` and `CHANGELOG.md`.
2. Merging that PR creates the GitHub Release and tag `vX.Y.Z` (first release: `0.1.0`).
3. The same workflow then builds the image and pushes it to GHCR:
   `ghcr.io/msch128/mnema-talk:X.Y.Z`, `:X.Y` and `:latest`, with OCI labels,
   provenance and SBOM. The version is baked into the binary
   (`-X main.version=X.Y.Z`).

## Configuration

All settings are environment variables; `.env.example` documents every one of
them with safe placeholder values. Notable ones:

- `PUBLIC_URL`: the URL browsers use; `https://` enables Secure/`__Host-` cookies.
- `CORS_ALLOWED_ORIGINS`: extra allowed origins (defaults to `PUBLIC_URL`).
- `TRUSTED_PROXY_CIDRS`: proxies whose `X-Forwarded-For` is trusted.
- `MEDIA_RETENTION_DAYS`: `0` (default) = never delete media automatically.
- `WEBRTC_UDP_PORT_MIN/MAX`, `WEBRTC_NAT_1TO1_IP`, `WEBRTC_STUN_URLS`: voice networking.
- `LEGAL_*`: operator details shown in the privacy policy.

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
  rate limiting and login lockout see only the proxy's IP.
- Voice and video do **not** go through the proxy. Forward the UDP range
  `WEBRTC_UDP_PORT_MIN`–`WEBRTC_UDP_PORT_MAX` (default `50000-50050/udp`) from
  your router/firewall to the host, and set `WEBRTC_NAT_1TO1_IP` to the public
  IP when the server is behind NAT.
- Set `APP_ENV=production`, a strong `JWT_SECRET` and an `https://` `PUBLIC_URL`.
- Do not use `docker-compose.dev.yml` in production; the base compose file
  does not publish PostgreSQL or SeaweedFS.

### Running a released image

Instead of building on the server, use a published image. In
`docker-compose.yml` on the server, replace the `build:` block of the `app`
service with a pinned tag:

```yaml
  app:
    image: ghcr.io/msch128/mnema-talk:0.1.0   # or :0.1 to follow patch releases
```

Then update with:

```sh
docker compose pull && docker compose up -d
```

Deployment to a LAN/self-hosted server stays **manual and pull-based**: GitHub's
hosted runners cannot reach a server inside a private network, so nothing
pushes to it. Bump the tag (or pull `:X.Y` / `:latest`) when you want to update.
Database migrations run automatically at startup.

If the GHCR package is private (the default for a newly published package),
either make it public once under the package's settings on GitHub, or run
`docker login ghcr.io` on the server with a token that has `read:packages`.

## Backups

Everything stateful lives in two Docker volumes (PostgreSQL and SeaweedFS) plus
your `.env`. `scripts/backup.sh` saves all three into one timestamped folder
and removes folders older than `KEEP_DAYS` (default 14). Run it from the
deployment directory, for example nightly via cron:

```sh
30 3 * * *  cd /path/to/mnema-talk && BACKUP_DIR=./backups/nightly ./scripts/backup.sh >> ./backups/nightly/backup.log 2>&1
```

Copy the backup folder off the machine as well (another disk, NAS share or
cloud storage); a backup on the same disk does not survive a disk failure.

Prove that a backup restores, without touching the live data:

```sh
./scripts/restore.sh backups/nightly/20261005-033000 --verify
```

Restore for real (stops the app, replaces database and media, starts again):

```sh
./scripts/restore.sh backups/nightly/20261005-033000
```

## Security

See [SECURITY.md](SECURITY.md) for how to report vulnerabilities and a summary of
the security model. Contributors and AI agents: read [AGENTS.md](AGENTS.md)
first. This repository is public, so never commit secrets.

## License

See [LICENSE](LICENSE).
