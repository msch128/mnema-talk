# Quick start

Mnema Talk is a self-hosted community app: text channels, direct messages and
voice rooms (called Talks) on a server you run yourself. This page is the one
path from an empty folder to your first voice call, on a NAS, a home server or a
VPS. The server itself takes about ten minutes; the domain name, the certificate
and the router settings take the rest, and they are the usual reason a first
install stalls. Words you do not know are in the
[glossary](glossary.md#voice-and-network-terms). Stuck? Jump to
[If something is red](#if-something-is-red).

## What you'll set up

About ten minutes for the server, plus DNS, certificate and router time.

- **Three containers that Compose runs for you**: the app (web, API and voice),
  PostgreSQL (database) and SeaweedFS (media storage); optionally an updater.
  In front of them you add a **reverse proxy** for HTTPS.
- **Three ports** open to the internet: TCP 443 (the site), UDP 50000-50050
  (voice) and port 3479 on UDP and TCP (relay for strict networks).
- **Six required settings** in `.env`. The setup assistant generates all of
  them except your address; by hand you must set each one.
- **Optional parts** (the "Update now" button, an external TURN server) are
  switched on with [Compose profiles](deployment.md#optional-parts-compose-profiles).

The required settings:

| Setting | What it is | Example | How to get it |
|---|---|---|---|
| `PUBLIC_URL` | The address members type into their browser | `https://chat.example.com` | You choose it (your domain) |
| `JWT_SECRET` | Secret that signs sessions, 32+ characters | (random) | `openssl rand -hex 32` |
| `POSTGRES_PASSWORD` | Database password | (random) | `openssl rand -hex 16` |
| `S3_ACCESS_KEY` | Media storage user name | `mnema` | Any letters and digits |
| `S3_SECRET_KEY` | Media storage password, 8+ characters | (random) | `openssl rand -hex 16` |
| `ADMIN_INITIAL_PASSWORD` | The Owner's first password, 14+ characters; first start only | (random) | `openssl rand -base64 18`; delete the line after signing in |

Anything missing or still a placeholder stops the server at startup. The log
(`docker compose logs app`) then says "Mnema Talk cannot start – fix these
settings in .env:" and lists them all. Every other setting has a default; the
full list is in [Settings at a glance](operations.md#settings-at-a-glance).

## What you need

- **A machine that runs Docker with Compose**: a Synology or Unraid NAS, a
  Raspberry Pi 4 or 5 with a 64-bit OS, any Linux box, or a small VPS. ARM and
  x86 both work; what a machine can carry is in [capacity](architecture.md#capacity). New to Docker? Install it from
  [docs.docker.com/engine/install](https://docs.docker.com/engine/install/).
- **A domain name** such as `chat.example.com` with a DNS *A record* (a DNS
  entry that points the name to an IPv4 address) for the address of your server
  or router. No domain yet? Read [the FAQ](faq.md#do-i-need-a-domain-name), or
  [try it on one computer first](#try-it-on-one-computer-first).
- **A way to open ports**: the settings of your home router, or the firewall of
  your VPS provider. [Step 4](#step-4-open-the-three-ports) lists exactly three.
- **A phone on mobile data** (Wi-Fi off) for the final test.

### Can I host at home?

Only if your internet line has its own public IPv4 address. Many providers
share one address between many customers (CGNAT, or DS-Lite on some cable and
fibre lines); ports you forward on such a line never receive anything from the
internet. Test it in a minute:

1. Open your router's status page and read its WAN or internet IP address. On a
   Fritz!Box, look under Internet > Online Monitor; menu names vary by firmware.
2. On any computer in your home network run `curl -4 https://ifconfig.me`.
3. If the two addresses differ, or the router's address starts with `10.`,
   `192.168.`, `172.16.` to `172.31.` or `100.64.` to `100.127.`, you are behind
   CGNAT. Run the server on a small VPS instead (any provider; one CPU core is
   enough for a small community), or follow the VPS-relay option in
   [IPv6, DS-Lite and CGNAT](operations.md#ipv6-ds-lite-and-cgnat). The rest of
   this page works the same on a VPS.

Mnema Talk also checks this itself: the setup assistant and Admin > Status warn
when the address it finds for your network looks like CGNAT.

## How it fits together

```
 Chat, pages, files          Browser ──HTTPS 443──▶ reverse proxy ──▶ app :8080
 Voice, video, screen share  Browser ──UDP 50000-50050────────────────▶ app (SFU)
 Fallback for strict networks Browser ──UDP/TCP 3479──▶ built-in TURN relay ──▶ app
```

- **Chat** is a normal website. A *reverse proxy* (a small web server in front
  of the app) holds the HTTPS certificate and passes requests on, including the
  *WebSocket* (the live connection that delivers new messages).
- **Voice and video** use *WebRTC*. Browsers send the media as UDP packets
  straight to the server, **not** through the reverse proxy. That is why a web
  app needs UDP ports at all. The server's *SFU* forwards each member's packets
  to the others without mixing them.
- Before a call the browser has to find a working path to the server (*ICE*).
  The server tells it its public address, which it learns at startup by asking
  a *STUN* server "what IP do you see?" (one request, nothing else is sent).
  Behind a home router, port forwarding (*NAT*) is what lets those packets in.
- Some networks block UDP entirely (company networks, some VPNs). For them the
  server has a built-in *TURN* relay on port 3479 that carries the media over
  one port, also over TCP. It is on by default.

## Step 1: make the files

The setup assistant asks a few questions, explains each one, generates every
secret and writes a complete `.env` and `docker-compose.yml`. Run it in an empty
folder (on a Synology or Unraid NAS: [platform notes](platform-notes.md)):

```sh
mkdir mnema-talk && cd mnema-talk
docker run --rm -it --user "$(id -u):$(id -g)" -v "$PWD:/out" ghcr.io/msch128/mnema-talk setup
```

`-it` lets it ask you questions; `--user` makes the files yours instead of
root's; `-v` is the folder it writes to. It asks for:

- the address members will open, e.g. `https://chat.example.com`,
- the Owner's user name (the all-powerful first account, default `admin`),
- the language of the default channel names, optionally mail settings, and
  whether to enable the "Update now" button.

It then checks what it can from inside (does the name resolve to this network's
public address, is the clock right) and prints a "what to do next" list. The
Owner's first password is printed once and also stored as
`ADMIN_INITIAL_PASSWORD` in `.env`; sign in as `admin` (or your user name), then
delete that line. The assistant never overwrites an existing `.env`: new secrets
would not fit an existing database.

No terminal (scripts, NAS web UI)? Pass everything as flags and see
`setup --help` for the rest:

```sh
docker run --rm --user "$(id -u):$(id -g)" -v "$PWD:/out" ghcr.io/msch128/mnema-talk \
  setup --non-interactive --public-url https://chat.example.com --admin-username admin --language en
```

## Step 2: start it

```sh
docker compose up -d --wait
curl -fsS http://127.0.0.1:8080/api/health     # {"status":"ok","version":"..."}
```

The first start pulls the images, which takes a few minutes. If the health call
works, the app is running; nobody can reach it from outside yet, because the
app port listens on the machine itself only ([why](faq.md#why-cant-i-reach-port-8080-from-another-computer)).

## Step 3: put HTTPS in front

Point the DNS A record of `chat.example.com` to your public IPv4 address, then
install a reverse proxy that forwards to `http://127.0.0.1:8080` and passes
**WebSockets**. Copy-paste steps for Caddy (VPS), the Synology reverse proxy,
Nginx Proxy Manager (Unraid) and nginx, plus certificates and the errors you can
meet: [reverse-proxy.md](reverse-proxy.md). A missing WebSocket setting is the
most common mistake: the page loads, then says "Connection lost – reconnecting…".

Once you can open `https://chat.example.com` and see the sign-in page, the web
part is done. If Admin > Status later shows *Reverse proxy* in amber, copy the
`TRUSTED_PROXY_CIDRS=` value it names into `.env` and run `docker compose up -d`.

## Step 4: open the three ports

The proxy handles chat. Voice needs two more things on the internet side. In
your router (port forwarding or "port sharing") or your VPS firewall create
these rules, all pointing to the machine that runs Mnema Talk:

| Name | Protocol | External port | Internal port | Why |
|---|---|---|---|---|
| web | TCP | 443 (and 80 for certificates) | the same | the site; without it nothing loads |
| voice | UDP | 50000-50050 | the same | voice and video; without it chat works and calls say "No media connection" |
| relay | UDP **and** TCP | 3479 | the same | built-in TURN relay; without it members behind strict firewalls cannot join |

**Never forward 8080** (the app port), 5432 or 8333. The long reference, every
setting and a version for several routers: [Network ports](ports.md). On a VPS
also allow the ports in the host firewall and in the provider's security group
([platform notes](platform-notes.md#plain-vps)).

## Step 5: check it layer by layer

Test in this order, and stop at the first thing that is not right. Each step
says what good looks like. A failure at one layer makes everything after it
look broken.

1. **The site.** On a phone with Wi-Fi off, open `https://chat.example.com`.
   Expected: a padlock and the sign-in page.
2. **Live updates.** Sign in as the Owner. Expected: the page stays usable and
   never shows "Connection lost – reconnecting…".
3. **Admin console > Status.** Wait about twenty seconds after a start, then
   read the list; every amber or red item names the setting to change. "Voice
   port" reads "Listening – not yet confirmed from outside" until a member from
   outside your network has connected, then "Confirmed: a member from outside
   connected". Green on the inside is not proof that your router forwards UDP.
4. **Test my connection.** In Admin > Status press the button. It runs in your
   browser and tells you whether direct UDP works, only the relay works, or
   nothing works. Run it from a network outside your home. Prefer a terminal?
   `docker compose exec app mnema-talk doctor` prints the same checks with fixes.
5. **A real call.** Create an invite (Admin console > Invites), open it on the
   phone on mobile data, join a Talk from your computer on Wi-Fi and speak.
   Expected: audio both ways within about two seconds.

Everything on the list worked? You are done. If one step failed, use the table
below or the [voice troubleshooting guide](troubleshooting-voice.md).

## If something is red

| What you see | Most likely cause | Where to look |
|---|---|---|
| The site does not load | DNS, port 443 not forwarded, certificate | [reverse-proxy.md](reverse-proxy.md#tls-certificates), [guide](troubleshooting-voice.md#rung-1-the-site) |
| Page loads, then "Connection lost – reconnecting…" | The proxy does not pass WebSockets | [reverse-proxy.md](reverse-proxy.md#websockets) |
| "No media connection" for people outside | UDP 50000-50050 not forwarded, or CGNAT | [guide](troubleshooting-voice.md#symptoms-causes-fixes) |
| Works from outside, not at home | Router without NAT loopback | [faq](faq.md#it-works-from-mobile-data-but-not-from-my-home-wi-fi) |
| 403 on sign-in | `PUBLIC_URL` differs from the address in the browser | [guide](troubleshooting-voice.md#symptoms-causes-fixes) |
| Everyone is locked out together | `TRUSTED_PROXY_CIDRS` missing | Status > Reverse proxy |

## Step 6: invite people and set the basics

Create invites, set the community name and rules, hand moderators their rights
and set up backups before more people arrive:
[After the first start](deployment.md#after-the-first-start) and
[Backups](operations.md#backups-and-restore).

## Try it on one computer first

Want to look around before buying a domain? Run step 1 with the address
`http://localhost:8080`, run step 2, open that address and sign in. Two browser
tabs can join a Talk together. Plain `http://` works only on `localhost`: browsers
block microphone and camera on any other `http://` address, so a LAN IP such as
`http://192.168.1.20:8080` lets you read and write but not talk. Anyone else
needs the HTTPS steps above.

## Later

- [Updating](updating.md), including the one-click "Update now" button.
- [Platform notes](platform-notes.md): Synology, Unraid, a VPS, Raspberry Pi.
- [FAQ](faq.md): domains, dynamic DNS, Cloudflare, tunnels, capacity.
- An external TURN server, the Docker Hub mirror, every setting, monitoring:
  [operations.md](operations.md). The manual route without the assistant:
  [deployment.md](deployment.md#fresh-install).
