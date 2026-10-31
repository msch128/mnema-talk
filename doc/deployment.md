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

## UDP socket buffers and the memory limit

**Socket buffers.** The server asks the kernel for 4 MiB of receive and send
buffer on its media and relay sockets, because a screen frame arrives as a
burst of ten or more packets and a keyframe as 40 or more. The kernel grants
at most `net.core.rmem_max` (receive) and `net.core.wmem_max` (send) of the
**host**; the usual default of about 208 KB silently caps the request and
bursts are then dropped when the server is briefly short of CPU. On a host
with many viewers, raise both (the values are bytes):

```sh
sudo sysctl -w net.core.rmem_max=8388608 net.core.wmem_max=8388608
# keep them across reboots:
printf 'net.core.rmem_max=8388608\nnet.core.wmem_max=8388608\n' | sudo tee /etc/sysctl.d/90-mnema-udp.conf
```

Docker containers on the default bridge share the host's limits. To see
whether packets were dropped, compare `RcvbufErrors` and `SndbufErrors` before
and after a call: `nstat -az UdpRcvbufErrors UdpSndbufErrors`.

**Memory limit.** At start the server reads the container's memory limit
(cgroup v2 `memory.max`, else cgroup v1) and tells the Go runtime to keep its
heap within 85% of it, so the garbage collector works harder near the limit
instead of letting the kernel reclaim or kill the process. It logs what it
chose (`memory limit: ...`). An explicit `GOMEMLIMIT` in the environment is
left as it is; without a container limit nothing is set.

## Fresh install

A new host needs Docker with Compose and nothing else: the compose file and
`.env.example` of the release are the whole deployment (no clone, no build).
The commands install the latest release; to pin another, set `VERSION` yourself.

```sh
mkdir mnema-talk && cd mnema-talk
VERSION=$(curl -fsSL https://api.github.com/repos/msch128/mnema-talk/releases/latest | sed -n 's/.*"tag_name": *"v\([^"]*\)".*/\1/p')
[ -n "$VERSION" ] || { echo "could not read the latest version; set VERSION=x.y.z yourself" >&2; false; }
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

The Owner's first sign-in shows a **Get started** list under the channel header:
name the community, invite people (a button creates and copies a 7-day link),
review the channels, set up voice, check the status page and, optionally, write
rules. The steps tick themselves from what exists on the server, or once you
have opened the channels or status page. Close the list with the cross; the
server menu brings it back (**Get started checklist**). A community that
already had members when the Owner first looked starts with it closed. The
state is kept per Owner (`GET`/`PUT /api/admin/first-run`).

- Create the first invite: Admin console → Invites. Members register with that
  code; there is no public registration.
- Set the community name and the rules new members accept (Admin console →
  Welcome & rules). The switch for direct messages (on by default) and the
  sign-out after inactivity are on their own page, Admin console → Sign-in &
  messages.
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

## Optional parts (Compose profiles)

A *profile* is a group of optional services in `docker-compose.yml` that start
only when you enable them. Without profiles you get the app, PostgreSQL and
SeaweedFS. To enable one:

- put `COMPOSE_PROFILES=autoupdate` (several: `autoupdate,turn`) in the `.env`
  next to the compose file; this also works in Synology Container Manager, or
- start with `docker compose --profile autoupdate up -d`.

| Profile | Starts | You want it when | Ports it opens | Extra `.env` settings |
|---|---|---|---|---|
| `autoupdate` | `updater` and `recreator` (they hold the Docker socket) | You want an "Update now" button in Admin > System | None (internal network; outbound HTTPS to `ghcr.io` and Sigstore) | `UPDATER_TOKEN` (`openssl rand -hex 32`, 32+ characters), a released `MNEMA_IMAGE` |
| `turn` | `coturn`, an external TURN server on host networking | You prefer a separate relay process or public relay ports; the built-in relay on port 3479 needs no profile | 3478 UDP and TCP, 49152-49200 UDP | `WEBRTC_TURN_URLS`, `WEBRTC_TURN_SECRET` (16+ characters), optionally `WEBRTC_TURN_REALM` ([coturn](operations.md#turn-coturn)) |

The setup assistant can enable `autoupdate` for you (it asks, or pass
`--auto-update`). Read the sidecar's [threat model](../SECURITY.md#self-update-sidecar)
first. After changing profiles run `docker compose up -d`.

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
