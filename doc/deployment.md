# Deployment

Everything around running a released image: which ports to open, which image
tag to follow, supported platforms and the update paths. The configuration
reference, reverse proxy, TURN and monitoring are in
[operations.md](operations.md); step-by-step upgrades and rollback are in
[upgrade.md](upgrade.md); backups are in
[operations.md](operations.md#backups-and-restore).

## Ports

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

- The compose file publishes `8080/tcp` on `127.0.0.1` only (set
  `PUBLISH_ADDR=0.0.0.0` in `.env` to reach it from the network). Don't forward
  it from the router.
- If you change `WEBRTC_UDP_PORT_MIN/MAX`, forward the new range; Docker
  publishes the same range.
- Users behind strict firewalls that block UDP can only join voice through
  TURN (`WEBRTC_TURN_URLS`, see `.env.example`).

## Running a released image

Instead of building on the server, use a published image: set `MNEMA_IMAGE`
in `.env` (the compose file uses it for the `app` service) and start without
`--build`:

```sh
# .env
MNEMA_IMAGE=ghcr.io/msch128/mnema-talk:0.8   # or :0.8.1 (pinned) or :latest

docker compose pull app && docker compose up -d
```

Then update with `docker compose pull app && docker compose up -d app`, or
from the admin console (see [Updates](#updates)).

## Supported platforms

Images are published for **linux/amd64** and **linux/arm64** (e.g. Raspberry Pi
4/5 with a 64-bit OS); Docker picks the right one. The rest of the stack
(`postgres:18-alpine`, `chrislusf/seaweedfs`, `coturn/coturn`) is published for
both as well. **linux/arm/v7** (32-bit ARM) is not built: those images exist
for it too, but the server doesn't compile for 32-bit targets yet (a 64-bit
constant in `internal/db` overflows `int`). Builders cross-compile the Go
binary on the build machine, so a multi-arch build needs no QEMU:
`docker buildx build --platform linux/amd64,linux/arm64 .`

## Pull-based deployment

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

1. Run a released image: `MNEMA_IMAGE=ghcr.io/msch128/mnema-talk:0.8` (see
   below for the tag).
2. Set `UPDATER_TOKEN` in `.env` (`openssl rand -hex 32`, at least 32
   characters).
3. Start with the profile: `docker compose --profile autoupdate up -d`.

The confirm dialog asks for the admin's password again; the server allows one
update every 5 minutes and only while a newer release is known, and logs who
started which update (`"audit":"self_update"` in the app log). Read the
threat model in [SECURITY.md](../SECURITY.md#self-update-sidecar) first: the
sidecar holds the Docker socket.

**Which tag?** The sidecar re-pulls the tag the app runs; it never switches
tags. That decides what "update now" can reach:

| `MNEMA_IMAGE` tag | Follows | Self-update |
|---|---|---|
| `:0.8` (major.minor) | all `0.8.x` releases | yes, until a breaking `0.9.0`; then change the tag by hand (recommended) |
| `:latest` | every release, breaking ones included | always |
| `:0.8.1` or `@sha256:…` | nothing (pinned) | no; the System tab shows the manual steps |
| `mnema-talk:local` (default) | built on the server | no; `git pull && docker compose up -d --build` |

Pinned tags are the most predictable (you choose every version and can roll
back by setting the old one); `:0.8` is the practical middle ground. Until
1.0.0, `feat:` and `fix:` releases bump the patch version, so a `:0.8` image
receives them, and breaking changes need a deliberate tag change. To roll
back, set the previous version (e.g. `:0.8.1`) and run `docker compose pull
app && docker compose up -d app`; database migrations are forward-only, so
check the changelog before going back across a release that changed the
schema.

**Manual update, step by step** (backup, compose file for the new tag, image,
pull, verify, rollback) and what changed between versions for operators:
[doc/upgrade.md](upgrade.md).
