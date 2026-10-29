# Deployment

Everything around running a released image: which ports to open, which image
tag to follow, supported platforms and the update paths. The configuration
reference, reverse proxy, TURN and monitoring are in
[operations.md](operations.md); step-by-step upgrades and rollback are in
[upgrade.md](upgrade.md); backups are in
[operations.md](operations.md#backups-and-restore).

New here? Start with the [quick start](quick-start.md); platform specifics
(Synology, Unraid, a plain VPS) are in [platform-notes.md](platform-notes.md) and
all updating is on one page, [updating.md](updating.md). The steps below are the
manual, platform-independent reference; the setup assistant
(`docker run --rm -it ghcr.io/msch128/mnema-talk setup`) does the "Fresh
install" part for you.

## Ports

The full reference, with every inbound port, every outbound connection and what
must stay closed, is [ports.md](ports.md). In short, with the default settings
forward TCP 443 to the reverse proxy, **UDP 50000-50050** for voice and video
and **UDP and TCP 3479** for the built-in TURN relay. Do not forward 8080
(published on `127.0.0.1` only), PostgreSQL or SeaweedFS.

## Fresh install

A new host needs Docker with Compose and nothing else: the compose file and
`.env.example` of the release are the whole deployment (no clone, no build).
Replace `X.Y.Z` with the version to install (the newest is on the
[releases page](https://github.com/msch128/mnema-talk/releases)).

```sh
mkdir mnema-talk && cd mnema-talk
VERSION=X.Y.Z
base=https://raw.githubusercontent.com/msch128/mnema-talk/v$VERSION
curl -fsSLO "$base/docker-compose.yml"
curl -fsSL "$base/.env.example" -o .env
chmod 600 .env
```

Edit `.env`: the values below are required in production, and the server
refuses the public placeholders from `.env.example`. (The setup assistant in
[quick-start.md](quick-start.md#step-1-make-the-files) generates this file and
the secrets for you.)

The same image is also published on Docker Hub as `docker.io/msch128/mnema-talk` (same digest and signature, see [Verifying a release image](operations.md#verifying-a-release-image)); GHCR stays the primary reference used in this guide.

| Variable | Set it to |
|---|---|
| `MNEMA_IMAGE` | `ghcr.io/msch128/mnema-talk:X.Y.Z` (or `:X.Y`, or `:latest`; see [Updates](#updates)) |
| `PUBLIC_URL` | The `https://` address members open. An `http://` address starts, with a warning (see [operations.md](operations.md#public_url-and-secure-cookies)). |
| `JWT_SECRET` | `openssl rand -hex 32` |
| `POSTGRES_PASSWORD` | `openssl rand -hex 16` (compose builds `DATABASE_URL` from it) |
| `S3_ACCESS_KEY`, `S3_SECRET_KEY` | Any letters and digits (secret: at least 8 characters) |
| `ADMIN_INITIAL_PASSWORD` | The Owner's password, at least 14 characters; remove it after the first start |
| `INITIAL_LANGUAGE` | Optional, `de` (default) or `en`: the language of the default category and channel names. Only the first start uses it. |

Then start and check it (the admin Status tab's
[setup checks](operations.md#setup-checks) list anything still missing):

```sh
docker compose config -q
docker compose up -d --wait        # pulls the images, returns once everything is healthy
curl -fsS http://127.0.0.1:8080/api/health
```

Sign in as the Owner (`ADMIN_USERNAME`, default `admin`) and create invite
codes. Put a reverse proxy with TLS in front, forward the voice UDP range
(see [Network ports](ports.md)) and set up [backups](operations.md#backups-and-restore)
before inviting anyone. CI runs exactly these steps against the image it just
built (`scripts/fresh-install-check.sh`).

### After the first start

- Create the first invite: Admin console → Invites. Members register with that
  code; there is no public registration.
- Set the community name and the rules new members accept (Admin console →
  Welcome & rules). The same page has the Owner's switch for direct messages
  (on by default).
- Give moderators their rights through roles: *Manage reports* (the Reports
  tab) and *Kick members* are off for @everyone and for every role until you
  grant them ([roles](roles-and-permissions.md)).
- Optional features, each off until you configure it: e-mail and password reset
  (`SMTP_*`), Web Push (keys are generated on the first start;
  `PUSH_VAPID_SUBJECT` is optional), two-factor sign-in (members switch it on themselves),
  the API reference page (`API_DOCS_ENABLED`), a TURN relay (`turn` profile)
  and the update button (`autoupdate` profile). All variables:
  [operations.md](operations.md).
- Check what your host can carry: [capacity](architecture.md#capacity).
- Browsers that can join Talks: [README](../README.md#supported-browsers).

## Running a released image

Instead of building on the server, use a published image: set `MNEMA_IMAGE`
in `.env` (the compose file uses it for the `app` service) and start without
`--build`:

```sh
# .env
MNEMA_IMAGE=ghcr.io/msch128/mnema-talk:X.Y   # or :X.Y.Z (pinned) or :latest

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

Everything about updating is on one page: [updating.md](updating.md). It covers
the backup, the update check and the optional "Update now" button, which image
tag to follow (`:X.Y`, `:latest` or a pinned `:X.Y.Z`; there is no `:X` tag),
the command-line steps, verification and rollback. Per-version notes for
operators: [upgrade.md](upgrade.md).
