# Quick start by platform

From an empty directory to the first voice call in about ten minutes. Pick your
platform; each section fits on one screen and ends with how to update. Ports,
image tags and the details behind these steps:
[deployment.md](deployment.md); every setting: [operations.md](operations.md).

**What you need on every platform**

1. A host name that points to your server, e.g. `chat.example.com`.
2. A reverse proxy with a TLS certificate in front of port 8080 (Synology and
   Unraid bring one; on a VPS Caddy is the shortest way).
3. The ports forwarded to the server: **TCP 443** for the proxy, **UDP
   50000-50050** for voice and video (the proxy does not carry them) and
   **UDP and TCP 3479** for the built-in TURN relay, which lets members behind
   firewalls that block UDP join calls. Every port, what it is for and the
   outbound connections: [Network ports](ports.md).

Nothing else is required. `PUBLIC_URL` is the one address you set; allowed
origins, `Secure` cookies and the links in e-mails follow from it, and the
server finds its own public IP for voice at startup.

## The setup assistant

It asks for the public address (and, if you want, mail), generates all secrets
and writes a complete `.env` and `docker-compose.yml`:

```sh
mkdir mnema-talk && cd mnema-talk
docker run --rm -it --user "$(id -u):$(id -g)" -v "$PWD:/out" ghcr.io/msch128/mnema-talk setup
```

It also checks what can be checked from inside: that the host name resolves to
this network's public address, whether the web port answers, and that the clock
is right. It cannot test your router's UDP forwarding; the admin console does
that job afterwards (see [Check the result](#check-the-result)). It never
overwrites an existing `.env`: a new one would hold new secrets that an
existing database refuses.

Without a terminal (scripts, a NAS web UI) pass everything as flags:

```sh
docker run --rm --user "$(id -u):$(id -g)" -v "$PWD:/out" ghcr.io/msch128/mnema-talk \
  setup --non-interactive --public-url https://chat.example.com \
  --admin-username admin --language en
# mail (optional): --smtp-host --smtp-port --smtp-tls --smtp-username --smtp-password --smtp-from
# update button:  --auto-update      other flags: setup --help
```

The minimal `.env` it writes is what you would write by hand:

```sh
PUBLIC_URL=https://chat.example.com
MNEMA_IMAGE=ghcr.io/msch128/mnema-talk:1.4
JWT_SECRET=...            # openssl rand -hex 32
POSTGRES_PASSWORD=...     # openssl rand -hex 16
S3_ACCESS_KEY=...         # any letters and digits
S3_SECRET_KEY=...         # at least 8 characters
ADMIN_INITIAL_PASSWORD=...  # the Owner's first password, 14+ characters
```

## Synology (Container Manager, DSM 7.2 or later)

1. **Files.** In File Station create `docker/mnema-talk` (path
   `/volume1/docker/mnema-talk`). Run the assistant over SSH (Control Panel >
   Terminal & SNMP > enable SSH; log in as an administrator):

   ```sh
   cd /volume1/docker/mnema-talk
   sudo docker run --rm -it -v "$PWD:/out" ghcr.io/msch128/mnema-talk setup
   ```

   No SSH? Run the assistant on any computer with Docker and copy the two files
   into that folder.
2. **Project.** Container Manager > Project > Create: name `mnema-talk`, path
   `/volume1/docker/mnema-talk`, "Use existing docker-compose.yml", next until
   done. It starts the stack and pulls the images. A project pulls by the full
   name, so `ghcr.io` needs no registry entry. The Registry tab only searches
   the registries listed under Registry > Settings; to browse `ghcr.io` there,
   add it as a custom registry (`https://ghcr.io`). If the package is private,
   run `sudo docker login ghcr.io` once with a token that has `read:packages`.
3. **Reverse proxy.** Control Panel > Login Portal > Advanced > Reverse Proxy >
   Create: source HTTPS, `chat.example.com`, port 443; destination HTTP,
   `localhost`, port 8080. In Custom Header press Create > WebSocket (it adds the
   `Upgrade` and `Connection` headers). Get the certificate under Control Panel >
   Security > Certificate (Let's Encrypt).
4. **Router and firewall.** Forward TCP 443, **UDP 50000-50050** and **UDP and TCP 3479** to the NAS.
   Allow both in Control Panel > Security > Firewall if it is on.
5. Open `https://chat.example.com`, sign in as the Owner, then
   [check the result](#check-the-result). The Status tab's *Reverse proxy* item
   names the exact `TRUSTED_PROXY_CIDRS=` value if the DSM proxy shows up as an
   untrusted address.

**Update.** Take a backup first ([upgrade.md](upgrade.md)). Over SSH:
`cd /volume1/docker/mnema-talk && sudo docker compose pull && sudo docker compose up -d`.
In the UI: Container Manager > Project > `mnema-talk` > Action > Stop, then
Clean (removes the project's containers and images, not your data) and Build,
which pulls the current image of the tag again. The tag in `MNEMA_IMAGE`
decides what is "current": `:1.4` follows patch releases of 1.4, `:latest`
everything ([tags](deployment.md#updates)). Menu wording differs a little
between DSM versions.

## Unraid

1. **Files.** Open the Unraid terminal and run the assistant in an appdata
   folder:

   ```sh
   mkdir -p /mnt/user/appdata/mnema-talk && cd /mnt/user/appdata/mnema-talk
   docker run --rm -it -v "$PWD:/out" ghcr.io/msch128/mnema-talk setup
   ```
2. **Start.** Still in that folder: `docker compose up -d --wait`. Recent
   Unraid releases include `docker compose`; otherwise install the *Docker
   Compose Manager* plugin and add a stack from this folder. The stack keeps its data in
   named Docker volumes; back them up as described in
   [operations.md](operations.md#backups-and-restore).
3. **Reverse proxy.** Nginx Proxy Manager, SWAG or Caddy from Community
   Applications all work: forward `chat.example.com` to `http://<unraid-ip>:8080`
   with WebSockets on. Set `PUBLISH_ADDR=0.0.0.0` in `.env` when the proxy
   runs in another container or on another machine, and list its address in
   `TRUSTED_PROXY_CIDRS` (the Status tab names it).
4. **Router.** Forward TCP 443, **UDP 50000-50050** and **UDP and TCP 3479** to the Unraid server.
5. [Check the result](#check-the-result).

**Update.** After a backup: `cd /mnt/user/appdata/mnema-talk && docker compose pull && docker compose up -d`.
Prefer a button? The optional updater sidecar (`--auto-update`, or
[Updates](deployment.md#updates)) adds "Update now" to the admin console; it
verifies the new image's signature first and needs `/var/run/docker.sock`,
which Unraid provides. Read its [threat model](../SECURITY.md#self-update-sidecar)
before you enable it.

## Plain VPS (Debian or Ubuntu with Docker)

1. **Files.**

   ```sh
   mkdir mnema-talk && cd mnema-talk
   docker run --rm -it --user "$(id -u):$(id -g)" -v "$PWD:/out" ghcr.io/msch128/mnema-talk setup
   ```
2. **Start.** `docker compose up -d --wait`
3. **TLS.** Caddy obtains the certificate by itself. In `/etc/caddy/Caddyfile`:

   ```caddy
   chat.example.com {
       reverse_proxy 127.0.0.1:8080
   }
   ```

   Then `sudo systemctl reload caddy`. Point the DNS A record of
   `chat.example.com` at the server before this step.
4. **Firewall.** `sudo ufw allow 80,443/tcp && sudo ufw allow 50000:50050/udp && sudo ufw allow 3479`
   (`3479` without a protocol opens UDP and TCP).
   Cloud providers have a second firewall (security group) with the same ports.
   On a VPS the public IP is on the machine itself, so voice needs nothing more.
5. [Check the result](#check-the-result).

**Update.** After a backup: `docker compose pull && docker compose up -d`.
Compose recreates only the services whose image changed; the migrations run at
startup and the downtime is a few seconds. The optional "Update now" button
works the same way ([Updates](deployment.md#updates)).

## Check the result

Sign in as the Owner and open **Admin console > Status**. The *Setup checks*
list shows green, amber and red items; every amber or red item says which
variable to set to what. The same list is in `docker compose logs app` about
twenty seconds after the start. Then create an invite (Admin console > Invites),
join with a second browser or phone and start a Talk.

| Check | Red or amber means |
|---|---|
| Public address | `PUBLIC_URL` is `localhost`, plain `http://`, or not the address you opened the page at |
| Voice address | The server found no public address: set `WEBRTC_NAT_1TO1_IP` |
| Voice port | The UDP port is taken or capped (`WEBRTC_UDP_MUX_PORT`) |
| TURN relay | The built-in relay could not bind port 3479, did not answer on it, or is switched off with no external TURN set: members behind networks that block UDP cannot join |
| Outgoing mail | None set: no password reset e-mails (optional) |
| Object storage, Database | A container is down or the secrets in `.env` do not match |
| Server clock | The host clock is off; turn on NTP |
| Reverse proxy | The proxy is not listed in `TRUSTED_PROXY_CIDRS` |

If the Talk connects for you but not for others, nearly always the router does
not forward UDP 50000-50050 to the server (members behind strict firewalls
also need 3479, UDP and TCP), or two members are on the same LAN
and the router lacks NAT loopback (list the server's LAN IP too:
`WEBRTC_NAT_1TO1_IP=<public IP>,<LAN IP>`). More:
[Nobody can join voice](operations.md#nobody-can-join-voice-or-it-connects-and-drops-after-a-few-seconds).
