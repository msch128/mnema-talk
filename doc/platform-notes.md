# Platform notes

The [quick start](quick-start.md) is the same on every machine. This page holds
what differs on Synology, Unraid, a plain VPS and small ARM boards.

## Synology

Container Manager on DSM 7.2 or later.

1. **Files.** In File Station create `docker/mnema-talk` (path
   `/volume1/docker/mnema-talk`). Run the setup assistant over SSH (Control
   Panel > Terminal & SNMP > enable SSH; log in as an administrator):

   <!-- test:setup-synology -->
   ```sh
   cd /volume1/docker/mnema-talk
   sudo docker run --rm -it --user "$(id -u):$(id -g)" -v "$PWD:/out" ghcr.io/msch128/mnema-talk setup
   ```

   `$(id -u)` is evaluated before `sudo`, so the files belong to you, not to
   root. No SSH? Run the assistant on any computer with Docker and copy the two
   files into that folder.
2. **Project instead of `docker compose up`.** Container Manager > Project >
   Create: name `mnema-talk`, path `/volume1/docker/mnema-talk`, "Use existing
   docker-compose.yml", next until done. It starts the stack and pulls the
   images. A project pulls by the full name, so `ghcr.io` needs no registry
   entry. The Registry tab only searches the registries listed under Registry >
   Settings; to browse `ghcr.io` there, add it as a custom registry
   (`https://ghcr.io`). If the package is private, run
   `sudo docker login ghcr.io` once with a token that has `read:packages`.
3. **Reverse proxy and certificate:** [reverse-proxy.md](reverse-proxy.md#synology-dsm-reverse-proxy).
   The compose file needs no edit for it. To use another app port, set `PORT=`
   in the `.env` in the project folder (Container Manager reads it from there)
   and rebuild the project. If the proxy runs on another machine, or in a
   container that cannot reach the NAS's loopback address, also set
   `PUBLISH_ADDR=` to the NAS's LAN address (not `0.0.0.0` on a NAS that faces
   the internet). Keep that port closed on the router.
4. **Router and firewall.** Forward the [three ports](quick-start.md#step-4-open-the-three-ports)
   to the NAS. If the DSM firewall is on (Control Panel > Security > Firewall),
   allow the same ports there.
5. **Logs without a shell:** Container Manager > Container > `mnema-talk` > Log.

**Web Station** (Web service portal) lists only literal ports: it does not
resolve `${PUBLISH_ADDR:-127.0.0.1}:${PORT:-8080}:8080`. If you prefer it to the
reverse proxy, edit that line in `docker-compose.yml` to a literal such as
`"127.0.0.1:8080:8080"`, and re-apply the edit after each compose update from a
new release.

**Docker Hub instead of GHCR.** If your NAS reaches Docker Hub more easily
than `ghcr.io`, set `MNEMA_IMAGE=msch128/mnema-talk:latest` (or a tag such as
`msch128/mnema-talk:2.1`) in the `.env` and rebuild the project. "Update now"
works with it: the updater verifies the same signature and provenance and pulls
from Docker Hub, not from GHCR (outbound `registry-1.docker.io` and
`auth.docker.io`, see [ports](ports.md#outbound-connections)).

**One-click updates.** Answer yes to the updater question in the assistant, or
add two lines to the `.env` in the project folder:

```sh
COMPOSE_PROFILES=autoupdate
UPDATER_TOKEN=<output of: openssl rand -hex 32>
```

Rebuild the project once (Container Manager > Project > `mnema-talk` > Action >
Stop, then Build). Container Manager reads the profile from that `.env` and
starts `mnema-updater` and `mnema-recreator` next to the app (confirmed on DSM
7.2). From then on use Admin > System > **Update now**; details and what to do
before it: [Updating](updating.md).

In Container Manager's image list, `<none>` as the tag of the postgres,
seaweedfs and watchtower images is expected: they are pinned by digest.
Container Manager's own image update view only tracks registries in its
registry list, so it will not flag a new Mnema Talk image; the "Update now"
button and the admin update check do.

## Unraid

1. **Files.** Open the Unraid terminal and run the assistant in an appdata
   folder:

   <!-- test:setup-unraid -->
   ```sh
   mkdir -p /mnt/user/appdata/mnema-talk && cd /mnt/user/appdata/mnema-talk
   docker run --rm -it --user "$(id -u):$(id -g)" -v "$PWD:/out" ghcr.io/msch128/mnema-talk setup
   ```
2. **Start.** In that folder: `docker compose up -d --wait`. Recent Unraid
   releases include `docker compose`; otherwise install the *Docker Compose
   Manager* plugin and add a stack from this folder. The stack keeps its data in
   named Docker volumes; back them up as described in
   [operations.md](operations.md#backups-and-restore).
3. **Reverse proxy:** [Nginx Proxy Manager](reverse-proxy.md#nginx-proxy-manager-unraid)
   (SWAG and Caddy work as well). The proxy is a container, so set
   `PUBLISH_ADDR=0.0.0.0` in `.env` and list its address in `TRUSTED_PROXY_CIDRS`.
4. **Router:** forward the [three ports](quick-start.md#step-4-open-the-three-ports)
   to the Unraid server.
5. **Updates.** [Updating](updating.md). The optional updater sidecar needs
   `/var/run/docker.sock`, which Unraid provides; read its
   [threat model](../SECURITY.md#self-update-sidecar) before you enable it.

## Plain VPS

Debian or Ubuntu with Docker.

1. Run the assistant and start the stack as in the quick start, then set up
   [Caddy](reverse-proxy.md#caddy-vps-or-linux-box).
2. **Firewall on the machine:**
   `sudo ufw allow 80,443/tcp && sudo ufw allow 50000:50050/udp && sudo ufw allow 3479`
   (`3479` without a protocol opens UDP and TCP). Docker publishes ports around
   `ufw`'s rules on some setups; if a port stays closed anyway, check
   `sudo iptables -L DOCKER-USER`.
3. **The provider's firewall** (security group) is a second, separate firewall
   with the same ports.
4. On a VPS the public IP is on the machine itself, so voice needs no
   forwarding and no `WEBRTC_NAT_1TO1_IP`.

## Raspberry Pi and other ARM boards

Images exist for linux/amd64 and linux/arm64, so a Raspberry Pi 4 or 5 with a
64-bit operating system works; 32-bit ARM does not
([supported platforms](deployment.md#supported-platforms)). Use an SSD or a
good SD card for the database volume.

## Docker Desktop (Windows, macOS)

Not a tested hosting target. It is fine for [trying Mnema Talk on one
computer](quick-start.md#try-it-on-one-computer-first); for a community use a
Linux machine, a NAS or a VPS.
