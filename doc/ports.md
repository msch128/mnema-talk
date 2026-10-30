# Network ports

The one reference for every port Mnema Talk listens on and every outbound
connection it makes. Use it to set up a router, a cloud firewall or a strict
egress filter. The settings behind the ports are in
[operations.md](operations.md); install steps per platform are in
[quick-start.md](quick-start.md).

## What to forward, in short

That is all: **three things** from the internet, and what breaks without each.

| Port | Protocol | If it is missing |
|---|---|---|
| 443 | TCP | The site does not load |
| 50000-50050 | UDP | Chat works, voice and video show "No media connection" |
| 3479 | UDP and TCP | Voice works for most members, but not for those behind strict firewalls or VPNs that block UDP |

In detail:

1. **TCP 443** (and TCP 80 if your proxy fetches certificates over HTTP) to the
   reverse proxy. The proxy forwards to the app on `127.0.0.1:8080`.
2. **UDP 50000-50050** to the host that runs Mnema Talk: voice, video and
   screen share. The proxy does not carry it (see
   [Why voice needs UDP](quick-start.md#how-it-fits-together)). Only one port of
   the range carries media in the default mode (the lowest, `WEBRTC_UDP_MUX_PORT`),
   so forwarding just that port would work today; the whole range is forwarded
   so the legacy per-peer mode (`WEBRTC_UDP_MUX_PORT=0`, one port per peer, about
   25 people at once across all rooms) keeps working too.
3. **UDP and TCP 3479** to the same host: the built-in TURN relay for members
   whose network blocks UDP. Needed only while the relay runs (the default
   while `WEBRTC_TURN_URLS` is empty).

Nothing else must be reachable from the internet. PostgreSQL and SeaweedFS are
not published at all, and **8080 stays closed**.

### Router and firewall copy-paste table

Create these entries in your router's port-forwarding page (it may be called
port sharing, virtual servers or NAT). "Internal IP" is the LAN address of the
machine that runs Mnema Talk (or of your reverse proxy for the first line when
it is a different machine); give that machine a fixed address first.

| Name | Protocol | External port | Internal IP | Internal port |
|---|---|---|---|---|
| mnema-web | TCP | 443 | the proxy's LAN IP, e.g. 192.168.1.20 | 443 |
| mnema-web-http (only for certificates) | TCP | 80 | the proxy's LAN IP | 80 |
| mnema-voice | UDP | 50000-50050 | the server's LAN IP | 50000-50050 |
| mnema-relay-udp | UDP | 3479 | the server's LAN IP | 3479 |
| mnema-relay-tcp | TCP | 3479 | the server's LAN IP | 3479 |

Never forward 8080, 5432 or 8333. A router that asks for a single "protocol:
TCP/UDP" entry can use "both" for 3479. For example, a Fritz!Box calls the page Internet >
Permit Access > Port Sharing; menu names vary by model and firmware. On a VPS create the
same ports in the host firewall and in the provider's security group
([platform notes](platform-notes.md#plain-vps)). Check the result with
[Test my connection](troubleshooting-voice.md#rung-4-test-my-connection).

## Inbound ports

The addresses below are the defaults of `docker-compose.yml` and `.env.example`.
"Internet" means members outside your network.

| Port | Protocol | Reached from | Default and setting | Purpose | Needed? | Forward / open? |
|---|---|---|---|---|---|---|
| 443 | TCP | Internet | Your reverse proxy | Web app, API and WebSocket (`/api/ws`) over HTTPS | Required | Yes, to the proxy |
| 80 | TCP | Internet | Your reverse proxy | Certificate issuance and the redirect to HTTPS | Only if the proxy needs it | Yes, to the proxy |
| 8080 | TCP | Reverse proxy only | `PORT` (host side of the mapping; the container always listens on 8080), published on `PUBLISH_ADDR` (default `127.0.0.1`) | The app's HTTP server behind the proxy | Required | **No.** Loopback by default. Set `PUBLISH_ADDR=0.0.0.0` or a LAN address only when the proxy runs on another host, and then keep it off the router |
| 50000-50050 | UDP | Internet | `WEBRTC_UDP_PORT_MIN` / `WEBRTC_UDP_PORT_MAX`, published on `WEBRTC_PUBLISH_ADDR` (default `0.0.0.0`; `127.0.0.1` keeps voice on this computer, as `setup --local` sets) | Voice, video and screen share from browsers (ICE and SRTP) | Required for calls | Yes, the whole range |
| 3479 | UDP and TCP | Internet | `WEBRTC_BUILTIN_TURN_PORT`, narrowed with `TURN_PUBLISH_ADDR` | Built-in TURN relay for members behind firewalls that block UDP | Optional: only while the relay runs (`WEBRTC_BUILTIN_TURN=auto` with `WEBRTC_TURN_URLS` empty, or `on`) | Yes, both protocols, while the relay runs |
| 3478 | UDP and TCP | Internet | Fixed in the `coturn` service | External coturn, only with `docker compose --profile turn` | Optional: only if you use coturn | Yes, only with coturn |
| 49152-49200 | UDP | Internet | Fixed in the `coturn` service | coturn relay ports | Optional: only if you use coturn | Yes, only with coturn |

Notes on the table:

- **Media range.** All peers share one UDP port, by default the lowest of the
  range (`WEBRTC_UDP_MUX_PORT`, which must lie inside the range). Compose still
  publishes the whole range, and `WEBRTC_UDP_MUX_PORT=0` (legacy mode) uses one
  port per peer from it. Forward the whole range so either mode works. If you
  change `WEBRTC_UDP_PORT_MIN` or `WEBRTC_UDP_PORT_MAX`, forward the new range;
  Compose publishes the same one.
- **Built-in relay.** One port serves both UDP and TCP and needs no relay
  range of its own. It relays only to the server's own media port. With
  `WEBRTC_BUILTIN_TURN=on` the port must lie outside the media range. Compose
  publishes it even when the relay is off; with the relay off, nothing listens
  and the forward is unnecessary. `TURN_PUBLISH_ADDR=127.0.0.1` stops Docker
  from publishing it. Details: [Built-in TURN relay](operations.md#built-in-turn-relay).
- **coturn.** coturn uses host networking on 3478 and the range 49152-49200 and
  can run beside the built-in relay, which uses 3479. Details:
  [coturn](operations.md#turn-coturn).
- **IPv6.** Forwarding an IPv6 port is not needed; the server announces IPv4
  addresses.
- **On a VPS** the public address is on the machine, so open the same ports in
  the host firewall (for example `ufw`) and in the provider's security group.
- **Behind NAT** (home router, NAS), set `WEBRTC_NAT_1TO1_IP` when the automatic
  detection does not find the public address, and forward to the host's LAN
  address. A server behind DS-Lite or CGNAT cannot receive forwarded ports; see
  [operations.md](operations.md#ipv6-ds-lite-and-cgnat).

## Ports that stay closed

These exist only inside the Docker network or on loopback. Do not forward or
publish them.

| Port | Protocol | Service | Exposure |
|---|---|---|---|
| 5432 | TCP | PostgreSQL | Docker network only, never published. The development compose file publishes it on loopback (`POSTGRES_HOST_PORT`) |
| 8333 | TCP | SeaweedFS S3 API | Docker network only, never published. The development compose file publishes it on loopback (`S3_HOST_PORT`) |
| 8080 | TCP | App HTTP | Loopback by default (see above). `/api/metrics` is served here too, only with `METRICS_TOKEN` set; scrape it from inside your network ([operations.md](operations.md#health-and-metrics)) |

## Outbound connections

The server and its sidecars start these connections themselves. Allow them in a
strict egress filter, or turn the feature off where noted. Browsers make their
own connections (to your server, and to the STUN or TURN servers you list in
`WEBRTC_STUN_URLS` and `WEBRTC_TURN_URLS`); those are not the server's.

| Destination | Port and protocol | When | Purpose | Switch off / change |
|---|---|---|---|---|
| `stun.l.google.com`, `stun.cloudflare.com` | 19302 and 3478, UDP | At startup and every 10 minutes, production only, while `WEBRTC_NAT_1TO1_IP` is empty | One STUN request to find the server's public IPv4 address. The STUN server sees the server's IP address, nothing else | `WEBRTC_AUTO_ANNOUNCE=false` (then set `WEBRTC_NAT_1TO1_IP`); servers: `WEBRTC_DETECT_STUN_URLS` |
| `api.github.com` | 443, TCP | Every 30 minutes | Check for a newer release (admin System tab) | `UPDATE_CHECK_ENABLED=false` |
| `time.cloudflare.com`, `pool.ntp.org` | 123, UDP | Every 6 hours | Compare the server clock for the admin setup checks | `CLOCK_CHECK_ENABLED=false` |
| Your mail server | `SMTP_PORT`: 587 (STARTTLS) by default, 465 with `SMTP_TLS=tls`, TCP | Only when mail is sent | Address confirmation and password reset | Leave `SMTP_HOST` empty for no mail |
| Browser push services (the address each browser registered, for example the browser vendor's) | 443, TCP | When a member has Web Push on and a notification is due | Encrypted Web Push notifications. Only public `https://` addresses on port 443 are allowed | `PUSH_ENABLED=false` |
| Websites members link to; `youtube.com`, `vimeo.com`, `store.steampowered.com` for rich previews | 80 and 443, TCP, public addresses only | When a message with a link is posted | Link previews and embeds | `LINK_PREVIEWS_ENABLED=false` |
| Webhook targets an admin configured | Public addresses; private ones only if listed in `WEBHOOK_ALLOW_PRIVATE_TARGETS` | When a webhook fires | Outgoing webhooks | Do not create webhooks |
| `discord.com` | 443, TCP | Only when an admin starts a Discord structure import from a template link | Fetch the Discord template ([Import](import.md)) | Do not use the import |
| `ghcr.io` | 443, TCP | When you pull or update the image | `docker compose pull` and the self-update sidecar pull `ghcr.io/msch128/mnema-talk` | Pin a tag and update by hand |
| `registry-1.docker.io`, `auth.docker.io` | 443, TCP | Only if `MNEMA_IMAGE` is the Docker Hub mirror (`msch128/mnema-talk`) instead of GHCR | `docker compose pull` and the self-update sidecar pull `docker.io/msch128/mnema-talk`; the sidecar also reads the signature from there | Use `ghcr.io` |
| `tuf-repo-cdn.sigstore.dev` | 443, TCP | Only with the `autoupdate` profile | The updater verifies the image signature through Sigstore before it pulls | `UPDATER_TRUSTED_ROOT` with a local `trusted_root.json` |
| `raw.githubusercontent.com` | 443, TCP | Fresh install and manual upgrades | Download `docker-compose.yml` and `.env.example` of a release | Copy the files yourself |

The server never needs inbound access from any of these. DNS (port 53) is
needed for every name above, as for any host.

## Related

- [Deployment](deployment.md): image tags, platforms and update paths.
- [Operations](operations.md): every setting, reverse proxy, TURN and monitoring.
- [Quick start](quick-start.md) and [platform notes](platform-notes.md): router
  and firewall steps for Synology, Unraid and a plain VPS.
- [Voice troubleshooting](troubleshooting-voice.md): when a port seems open but
  calls fail.
