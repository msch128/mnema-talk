# Mnema Talk

**A small, self-hosted home for one community.** Text channels, drop-in Talks
and screen sharing up to 4K at 60 FPS, in one Go binary that you run on your
own server or NAS. Invite-only, no accounts on someone else's platform, no
transcoding, and microphone, camera and screen are end-to-end encrypted
against the server that relays them.

Mnema Talk is not a Discord clone. It has one community instead of many
servers, no direct messages, no ringing, and its own quiet, dark design. It
keeps the parts that make a community server useful: roles, private channels,
webhooks and a call you can join with one click.

![Mnema Talk: a text channel with replies, reactions and a code block](doc/images/chat.png)

[Features](#features) · [Quickstart](#quickstart) · [Security model](#security-and-privacy) ·
[Desktop client](#desktop-client) · [Documentation](#documentation) ·
[Development](#development)

## Features

### Talk (voice, camera, screen)

- **Hangout spots.** Click a Talk and you are in. A reload within 30 seconds
  puts you back into the call.
- **Screen sharing up to source quality**, up to 4K at 60 FPS with hardware
  encoding in the browser. A share is sent as the source plus smaller **layers**;
  every viewer picks a layer or leaves it on **Auto**, which follows the
  receiver reports, so each viewer is served separately. Watching is opt-in
  and everyone sees who watches.
- **A Talk view built for long sessions.** Participant grid, a stage for the
  share or camera you focus, full screen, picture-in-picture while you read
  another channel, per-person and per-stream volume, push-to-talk, noise
  suppression and the Talk's own chat as a resizable panel.
- **No transcoding.** A Pion-based forwarding unit (SFU) copies RTP packets;
  see [doc/architecture.md](doc/architecture.md).

| Talk with a screen share and the quality picker | Safety code for the call |
|---|---|
| ![A Talk with a shared screen and the viewing quality menu](doc/images/talk-screenshare.png) | ![Connection details with the safety code and the members holding the keys](doc/images/safety-code.png) |

### Chat

- Markdown, replies that jump to the original (also thousands of messages
  back), threads, reactions, emoji, @mentions, file and image attachments,
  link cards, search, typing notices, unread and mention markers,
  per-channel notification settings and presence with a status line.
- One community with categories, text channels and Talks. Every Talk has its
  own text chat.
- German and English, chosen per account.

### Security and privacy

- **Media is end-to-end encrypted** in the browser with keys from an MLS
  group: microphone, camera, screen video and screen audio. Keys rotate every
  30 minutes, every sealed frame is signed by its sender, and a **safety code**
  in the connection details lets members compare keys out of band.
- **Invite-only.** There is no public registration.
- **Hardened by default**: `HttpOnly` session cookies with revocation, CSRF
  origin check, strict CSP, upload content sniffing, rate limits and login
  lockout, an SSRF guard for outgoing webhooks, and an app port that only
  listens on loopback until you say otherwise.
- **Privacy-minded**: no third-party STUN unless configured, no automatic data
  deletion unless enabled (`MEDIA_RETENTION_DAYS=0`), no telemetry (the optional release check is the only call home, and it can be
  turned off).
- Release images are signed with cosign and ship provenance and an SBOM.

### Administration

- **An Owner and roles.** The Owner can do everything and cannot be restricted
  by anyone. Below them, custom roles hold Discord-style permissions, and
  categories and channels can allow or deny them per role or member, which
  makes **private channels and Talks**. Changes apply live, also in a call.
- **Webhooks.** Incoming ones in Discord's execute format (so most existing
  integrations work) and outgoing ones that POST signed events to your service
  with retries and a delivery log.
- **Admin console** for members (disable, kick, reset password, revoke
  sessions), roles, channel layout, invites, media and storage, and a System
  tab with health and a live operator view. The sidebar supports drag and drop
  with undo.

| Roles and permissions | Webhook settings |
|---|---|
| ![The roles editor with the Moderator role selected](doc/images/roles.png) | ![The webhooks dialog of a channel with an incoming webhook's settings](doc/images/webhooks.png) |

### Operations

- **One binary, three containers**: `app`, PostgreSQL 18 and SeaweedFS (any S3
  store works). Low idle memory; images for linux/amd64 and linux/arm64, so a
  NAS or a Raspberry Pi works.
- **Observable**: Prometheus metrics at `/api/metrics`, a live operator view in
  the System tab, and alert rules you can copy
  ([doc/operations.md](doc/operations.md#monitoring-and-alerts)).
- **Recoverable**: backup and restore scripts, plus drills in CI that prove
  them: backup to restore, release-to-release upgrade and the PostgreSQL move.
- **Measured**: `make load-test` reproduces a 100-member call with real
  decrypting browsers and lightweight viewers
  ([loadtest/README.md](loadtest/README.md)).
- Optional TURN relay (`coturn` profile) and a self-update sidecar
  (`autoupdate` profile, off by default).

![The admin System tab with live operations charts](doc/images/operations.png)

## Quickstart

You need a Linux host with Docker and Docker Compose, a domain with HTTPS
(through a reverse proxy) and a UDP port range open for voice.

```sh
git clone https://github.com/msch128/mnema-talk.git && cd mnema-talk
cp .env.example .env
# edit .env: PUBLIC_URL, JWT_SECRET (openssl rand -hex 32), POSTGRES_PASSWORD,
# S3_ACCESS_KEY / S3_SECRET_KEY and ADMIN_INITIAL_PASSWORD (14+ characters)
docker compose up -d --build      # or: make up
```

To run a published image instead of building, set
`MNEMA_IMAGE=ghcr.io/msch128/mnema-talk:0.8` in `.env` and use
`docker compose pull app && docker compose up -d`
([doc/deployment.md](doc/deployment.md)).

Open `PUBLIC_URL`, sign in as the Owner (default username `Herzog`, set with
`ADMIN_USERNAME`) and create invite codes for your members.
`ADMIN_INITIAL_PASSWORD` only creates the account on the first start; remove
it from `.env` afterwards.

Two defaults to know about:

- **`PUBLISH_ADDR`** is `127.0.0.1`: the app port is only reachable from the
  host itself, which is what a reverse proxy on the same host needs. Set it to
  a LAN address (or `0.0.0.0` behind a firewall) when the proxy runs elsewhere.
- **`TRUSTED_PROXY_CIDRS`** is empty, which trusts loopback only. A reverse
  proxy on another host or in a container must be listed here by address,
  otherwise all members share the proxy's IP for rate limits and login
  lockout. See [the upgrade note](doc/upgrade.md#07x--080) for the exact setups.

### Behind a reverse proxy

Terminate TLS in a proxy such as Caddy and forward HTTP and WebSockets:

```caddy
chat.example.com {
    reverse_proxy app-host:8080
}
```

Voice and video bypass the proxy: forward the UDP range
`WEBRTC_UDP_PORT_MIN`-`WEBRTC_UDP_PORT_MAX` (default `50000-50050/udp`) to the
host and set `WEBRTC_NAT_1TO1_IP` to your public IP when the server is behind
NAT. Users on networks that block UDP need the optional TURN relay. Ports,
proxy settings, TURN and troubleshooting:
[doc/operations.md](doc/operations.md) and [doc/deployment.md](doc/deployment.md).

### Upgrades and backups

Take a backup before every update (`scripts/backup.sh`; restore with
`scripts/restore.sh`, which can verify a backup in a disposable container
first). Migrations run at startup and are forward-only. Per-version notes,
including the move from 0.7 to 0.8, and the rollback procedure are in
[doc/upgrade.md](doc/upgrade.md). Admins see new releases in the System tab; an
optional update button needs the `autoupdate` profile.

## Security and privacy

In a few honest sentences:

- **Media** (microphone, camera, screen video and screen audio) is encrypted in
  the browser with MLS-derived keys, so an honest server, and anyone who only
  sees the forwarded packets, cannot read it. The server still serves the web
  app's code and relays the key exchange, so a malicious or compromised server
  could subvert it; members who compare the safety code notice most such
  attacks. Who speaks when, and who is in which Talk, stays visible to the
  server.
- **Text messages and attachments** are readable by the server, as on
  Discord. Nothing is recorded and nothing is deleted automatically unless you
  turn retention on.
- **The desktop client removes server-served JavaScript**: it runs a signed web
  bundle that it verifies itself, so the server supplies data, never code.
- Transport is HTTPS and DTLS-SRTP. The project is pre-1.0 and has had an
  internal security review, not an external audit.

The full model, its limits and how to report a vulnerability privately are in
[SECURITY.md](SECURITY.md); the product's test scope is in
[doc/product-status.md](doc/product-status.md).

## Desktop client

The [Windows desktop client](https://github.com/msch128/mnema-talk-desktop)
(experimental, unsigned) wraps the same web app. Instead of loading code from
your server it runs a **signed bundle** that it verifies on every start, with
rollback protection and a security floor, and keeps the session in the OS
keychain. It adds a gaming sidepeek and an `Alt+M` overlay for your Talk. The
browser app remains the primary, fully supported client. Details and the
threat model: [doc/desktop-trusted-client.md](doc/desktop-trusted-client.md)
and [doc/desktop-app.md](doc/desktop-app.md).

## Documentation

| Topic | Where |
|---|---|
| Components, data flow, capacity | [doc/architecture.md](doc/architecture.md) |
| Configuration, reverse proxy, TURN, backups, metrics and alerts, troubleshooting | [doc/operations.md](doc/operations.md) |
| Ports, image tags, platforms, updating | [doc/deployment.md](doc/deployment.md) |
| Upgrade notes per version, rollback | [doc/upgrade.md](doc/upgrade.md) |
| Roles, permissions and private channels | [doc/roles-and-permissions.md](doc/roles-and-permissions.md) |
| Webhooks (incoming and outgoing) | [doc/webhooks.md](doc/webhooks.md) |
| Where media keys live in the browser | [doc/crypto-state.md](doc/crypto-state.md) |
| Trusted desktop client, gaming overlay | [doc/desktop-trusted-client.md](doc/desktop-trusted-client.md), [doc/desktop-app.md](doc/desktop-app.md) |
| Load test | [loadtest/README.md](loadtest/README.md) |
| Product status and acceptance | [doc/product-status.md](doc/product-status.md) |
| Building, testing, CI, releases | [doc/development.md](doc/development.md) |
| API reference | `API_DOCS_ENABLED=true` serves it at `/api/docs` to signed-in members |

## Development

Requirements: Go (see `go.mod`), Node and npm, Docker, GNU make. `make help`
lists every target.

```sh
make install-hooks      # pre-commit: gitleaks + gofmt
make dev                # postgres + seaweedfs in Docker, server on :8080 (reads .env)
cd web && npm run dev   # Vite on :3000, proxies /api to :8080
make check              # everything CI runs
make e2e                # Playwright against the real binary
```

Stack: Go, chi, pgx, Pion WebRTC; Vue 3, Pinia, Vite, Tailwind in strict
TypeScript; PostgreSQL 18; S3-compatible storage. More in
[doc/development.md](doc/development.md).

## Contributing

Pull requests are welcome. The repository is public: never commit secrets,
databases or uploads. Use conventional commits (`feat:`, `fix:`, `docs:`);
release-please derives versions and the changelog from them. `main` is
protected, so changes land through a pull request with green checks and a
squash merge. Read [AGENTS.md](AGENTS.md) first, it applies to people and AI
agents alike. Report vulnerabilities privately as described in
[SECURITY.md](SECURITY.md).

## License

Copyright (C) 2026 Marius Schröder ([msch128](https://github.com/msch128)).

Mnema Talk is licensed under the
[GNU Affero General Public License v3.0](LICENSE) (AGPL-3.0-only). You may
use, change and share it, also commercially, as long as you publish the
complete source code of your version under the same license. That includes
running a changed version as a network service: its users must be able to get
the source (the legal dialog in the app links to it; point `SOURCE_URL` in
`web/src/lib/thirdParty.ts` at your own repository).

**Commercial license:** to use Mnema Talk, or parts of it, without the AGPL
obligations (for example in a closed-source product or service), get a
separate commercial license from the copyright holder. Contact
[msch128 on GitHub](https://github.com/msch128).

Contributions are accepted under the AGPL-3.0, and their authors agree that
Marius Schröder may also distribute them under a commercial license.
