# Deployment

Everything around running a released image: which ports to open, which image
tag to follow, supported platforms and the update paths. The configuration
reference, reverse proxy, TURN and monitoring are in
[operations.md](operations.md); step-by-step upgrades and rollback are in
[upgrade.md](upgrade.md); backups are in
[operations.md](operations.md#backups-and-restore).

Short, per-platform guides (Synology, Unraid, a plain VPS), including how to
update each: [quick-start.md](quick-start.md). The steps below are the
platform-independent reference; the setup assistant
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
Replace `1.1.0` with the version to install.

```sh
mkdir mnema-talk && cd mnema-talk
VERSION=1.1.0
base=https://raw.githubusercontent.com/msch128/mnema-talk/v$VERSION
curl -fsSLO "$base/docker-compose.yml"
curl -fsSL "$base/.env.example" -o .env
chmod 600 .env
```

Edit `.env`: the values below are required in production, and the server
refuses the public placeholders from `.env.example`. (The setup assistant in
[quick-start.md](quick-start.md#the-setup-assistant) generates this file and
the secrets for you.)

The same image is also published on Docker Hub as `docker.io/msch128/mnema-talk` (same digest and signature, see [Verifying a release image](operations.md#verifying-a-release-image)); GHCR stays the primary reference used in this guide.

| Variable | Set it to |
|---|---|
| `MNEMA_IMAGE` | `ghcr.io/msch128/mnema-talk:1.1.0` (or `:1.1`, see [Updates](#updates)) |
| `PUBLIC_URL` | The `https://` address members open. An `http://` address starts, with a warning (see [operations.md](operations.md#public_url-and-secure-cookies)). |
| `JWT_SECRET` | `openssl rand -hex 32` |
| `POSTGRES_PASSWORD` | `openssl rand -hex 16` (compose builds `DATABASE_URL` from it) |
| `S3_ACCESS_KEY`, `S3_SECRET_KEY` | Any letters and digits (secret: at least 8 characters) |
| `ADMIN_INITIAL_PASSWORD` | The Owner's password, at least 14 characters; remove it after the first start |
| `INITIAL_LANGUAGE` | Optional, `en` (default) or `de`: the language of the default category and channel names. Only the first start uses it. |

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
  Welcome & rules). *Admin* > *Sign-in & messages* has the switch for direct
  messages (on by default).
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
MNEMA_IMAGE=ghcr.io/msch128/mnema-talk:1.1   # or :1.1.0 (pinned) or :latest

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
storage and voice, and the update command. A dot on the server menu (the name at the top of the sidebar) and a badge
on "Admin console" announce a new release. Nothing is ever updated
automatically.

**Optional: update from the admin console.** "Update now" asks a separate
updater sidecar to check the new image's signature and build provenance, pull
that exact digest and restart the app (everyone is disconnected for about
30–60 seconds). The profile runs two small services: the `updater`, which
verifies and pulls, and the `recreator`, which only restarts the app from the
local image. An image that does not verify is refused and nothing changes
([how the check works](operations.md#verifying-a-release-image)). It is **off
by default**; to enable it:

1. Run a released image: `MNEMA_IMAGE=ghcr.io/msch128/mnema-talk:1.1` (see
   below for the tag).
2. Set `UPDATER_TOKEN` in `.env` (`openssl rand -hex 32`, at least 32
   characters).
3. Start with the profile: `docker compose --profile autoupdate up -d`.

The confirm dialog asks for the admin's password again; the server allows one
update every 5 minutes and only while a newer release is known, and logs who
started which update (`"audit":"self_update"` in the app log). Read the
threat model in [SECURITY.md](../SECURITY.md#self-update-sidecar) first: the
sidecar holds the Docker socket.

**Which tag?** The sidecar updates the tag the app runs; it never switches
tags. That decides what "update now" can reach:

| `MNEMA_IMAGE` tag | Follows | Self-update |
|---|---|---|
| `:1.1` (major.minor) | all `1.1.x` patch releases | yes, until `1.2.0`; a new minor is a deliberate tag change (recommended) |
| `:latest` | every release, breaking ones included | always |
| `:1.1.0` or `@sha256:…` | nothing (pinned) | no; the System tab shows the manual steps |
| `mnema-talk:local` (default) | built on the server | no; `git pull && docker compose up -d --build` |

Pinned tags are the most predictable (you choose every version and can roll
back by setting the old one); `:1.1` is the practical middle ground. Since
1.0.0, `fix:` releases bump the patch version and `feat:` releases the minor,
so a `:1.1` image receives only fixes, and a new minor needs a deliberate tag
change. There is no `:1` tag; the release pipeline publishes `X.Y.Z`, `X.Y`
and `latest`. To roll back, set the previous version (e.g. `:1.0.1`) and run `docker compose pull
app && docker compose up -d app`; database migrations are forward-only, so
going back from 1.1 to 1.0.x means restoring a backup taken before the
update (see [Rollback](upgrade.md#rollback)); check the changelog before
going back across any release that changed the schema.

**Manual update, step by step** (backup, compose file for the new tag, image,
pull, verify, rollback) and what changed between versions for operators:
[doc/upgrade.md](upgrade.md).
