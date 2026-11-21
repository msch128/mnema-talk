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
   sudo docker run --rm -it --user "$(id -u):$(id -g)" -v "$PWD:/out" ghcr.io/mnema-xyz/mnema-talk setup
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
than `ghcr.io`, set `MNEMA_IMAGE=mnemaxyz/mnema-talk:latest` (or a tag such as
`mnemaxyz/mnema-talk:2.1`) in the `.env` and rebuild the project. "Update now"
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

## Install on Unraid via Community Apps

Mnema Talk runs as three containers from the *Apps* tab: PostgreSQL 18 for
the data, SeaweedFS-Mnema for media (an S3 store), and Mnema Talk itself. Each
is a classic single-container template, so any Unraid release with Community
Apps works. Make every secret below on the Unraid terminal with
`openssl rand -hex 32`, and keep them.

1. **PostgreSQL.** Search for *postgresql18* and install the one from
   *jj9987's Repository* (the official `postgres:18` image). Set
   *POSTGRES_PASSWORD* to a new secret, *POSTGRES_USER* to `mnema` and
   *POSTGRES_DB* to `mnema_talk`. A cache-only data path such as
   `/mnt/cache/appdata/postgresql18` is faster. Talk needs PostgreSQL 18 with
   the `pgcrypto` and `pg_trgm` extensions, which the official image ships.
   If a container named `postgresql18` already exists for another app (the
   Docker tab lists it), do not reuse or overwrite it: change *Name* to, for
   example, `postgresql18-mnema` and give it its own data path.
2. **Media storage.** Search for *SeaweedFS-Mnema* and install it. Keep the
   access key `mnema` (or pick your own) and set the *Secret key* to a new
   secret. Only the S3 port 8333 is published, and it refuses every request
   without the key.
3. **Mnema Talk.** Search for *Mnema Talk* and install it. Fill in:
   - *Public URL*: the `https://` address people will open;
   - *Database URL*:
     `postgres://mnema:DB-PASSWORD@UNRAID-IP:5432/mnema_talk?sslmode=disable`
     with the password from step 1 and your Unraid server's address;
   - *Media storage address* `http://UNRAID-IP:8333`, and the access key and
     secret key from step 2;
   - *JWT secret* (a new secret) and the *Admin password* for the first login.

   Under *Show more settings*, also fill in the *Operator* fields (they appear
   on the legal notice) and, for e-mail, the *SMTP* fields. *SMTP from* takes
   a bare address such as `talk@example.com`: Unraid removes angle brackets, so
   `Name <talk@example.com>` arrives as an invalid address and Talk does not
   start. Mails carry your community name as the sender name anyway.

   The three containers stay on the default bridge network and reach each other
   through the Unraid server's address. Never forward ports 5432, 8333 or the
   web port 8080 on your router. To use another voice port range, edit the
   *Voice UDP ports* entry and set the same range as host and container port,
   then *Voice UDP port min* and *max* to match.
4. **Reverse proxy and router.** Same as steps 3 and 4 of the
   [Unraid section](#unraid) below: a proxy container with HTTPS in front of the
   web port, and the [three ports](quick-start.md#step-4-open-the-three-ports)
   forwarded (the voice ports keep the same number on host and container).
   *Reverse proxy address* is the address the proxy's requests arrive from. A
   proxy that forwards to `UNRAID-IP:8080` (the usual setup) arrives from the
   Docker bridge gateway, normally `172.17.0.1`; only a proxy on the same custom
   network that forwards to the Talk container directly arrives from its own
   container address. If Admin > Status warns about a proxy that is not
   trusted, the address is wrong.
5. **Autostart.** In the Docker tab, turn on autostart for all three in the
   order postgresql18, SeaweedFS-Mnema, Mnema Talk, with a wait of 10 seconds
   after the first two: Talk exits when the database is not up yet when it
   starts.

**Backups.** Copying the PostgreSQL appdata folder while the database runs is
no backup. Use
[`scripts/mnema-talk-backup.sh`](https://github.com/mnema-xyz/unraid-templates/blob/main/scripts/mnema-talk-backup.sh)
of the template repository as a daily user script (*User Scripts* plugin): it
stops Talk and SeaweedFS-Mnema for a few seconds, dumps the database, archives
the media folder and the three Docker templates, restore-tests the dump in a
throwaway container and reports the result in Admin > Status through the
*Backup status folder*. Setup and restore steps are in the
[template repository README](https://github.com/mnema-xyz/unraid-templates#backups).
`scripts/backup.sh` and `scripts/restore.sh` of this repository serve the
Compose stack only.

**Updates.** The templates install a released image and have no "Update now"
button. The tag `3.3` follows every 3.3.x release through the Docker tab's
update check; for a new minor release (3.4) edit the container once and change
the tag in *Repository*. The app's admin page tells you when a new release is
out. Keep PostgreSQL on major version 18 when you update it.

**Moving a Compose install to the templates.** Keep the Compose stack until
the new install works; it stays your way back.

1. Write down the settings the app runs with (`docker inspect mnema-talk`
   lists them as `Config.Env`) and back up: `./scripts/backup.sh` in the
   Compose folder, with the app stopped first (`docker compose stop app`) so
   nothing writes after the dump.
2. Install the three templates as above with the same values: the same
   database password, S3 keys, JWT secret (a new one signs every member out
   and makes the stored two-factor secrets unreadable) and bucket. The voice ports must be free, so stop the Compose stack
   (`docker compose stop`, never `down -v`) before you start Mnema Talk.
3. Import the database into the new, still empty PostgreSQL:
   `gunzip -c BACKUP/postgres.sql.gz | docker exec -i postgresql18 psql -U mnema -d mnema_talk -v ON_ERROR_STOP=1 -q`.
   Do not copy the old data folder instead: the Compose image is Alpine-based
   and the official one Debian-based, and their different text sorting breaks
   indexes on copied data. The Compose database runs in UTC, while the template
   container takes the Unraid time zone; keep UTC with
   `docker exec postgresql18 psql -U mnema -d mnema_talk -c "ALTER DATABASE mnema_talk SET timezone TO 'UTC'"`.
4. Copy the media with an S3 client while both stores run, for example
   `rclone copy` from the old SeaweedFS (on the Compose network, port 8333) to
   the new one (`UNRAID-IP:8333`), and compare both with `rclone check`.
5. Start Mnema Talk and keep the reverse proxy pointing at port 8080; adjust
   *Reverse proxy address* as in step 4 above.

**Alternative: Compose Manager.** To run the stack as one unit instead,
install the *Compose Manager Plus* plugin from Community Apps, add a stack, and
paste the
[`compose.yaml`](https://github.com/mnema-xyz/unraid-templates/blob/main/mnema-talk/compose.yaml)
and
[`.env.example`](https://github.com/mnema-xyz/unraid-templates/blob/main/mnema-talk/.env.example)
of the template repository (save the second as `.env` and fill in the five
values it lists). Updates: pull the new image in the stack, or follow
[updating.md](updating.md).

**Mnemo, the AI helper bot.** [Mnemo](https://github.com/mnema-xyz/talk-mnemo-bot)
(optional, bring your own AI provider) has a Community Apps template of its
own: install it from the *Apps* tab after you added the bot in Talk, steps in
the [Mnemo guide](https://github.com/mnema-xyz/talk-mnemo-bot/blob/main/README.md#install-on-unraid).

## Unraid

1. **Files.** Open the Unraid terminal and run the assistant in an appdata
   folder:

   <!-- test:setup-unraid -->
   ```sh
   mkdir -p /mnt/user/appdata/mnema-talk && cd /mnt/user/appdata/mnema-talk
   docker run --rm -it --user "$(id -u):$(id -g)" -v "$PWD:/out" ghcr.io/mnema-xyz/mnema-talk setup
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
2. **Firewall on the machine:** allow SSH first, then the Mnema Talk ports,
   and only then switch `ufw` on, so you cannot lock yourself out:
   `sudo ufw allow 22/tcp && sudo ufw allow 80,443/tcp && sudo ufw allow 50000:50050/udp && sudo ufw allow 3479 && sudo ufw default deny incoming && sudo ufw enable`
   (`3479` without a protocol opens UDP and TCP). Docker publishes its ports
   around `ufw`, so `ufw` does **not** restrict them: what keeps a service
   private is that Compose binds the app port (8080, or your `PORT`) to
   `127.0.0.1` by default (`PUBLISH_ADDR` in `.env`, which the assistant sets
   to `0.0.0.0` only when you chose a plain `http://` address) and does not
   publish PostgreSQL and SeaweedFS at all. Do not add `ports:` lines for
   them. To check from outside, run `nc -vz <your IP> 8080` from another
   machine: it must not connect; if it does, look at `PUBLISH_ADDR`. If a port that should be open stays closed,
   check `sudo iptables -L DOCKER-USER`.
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
