# Troubleshooting: the site, live updates and voice

Work from the bottom up. Voice depends on the site, the site depends on the
proxy, and each layer can look fine while the one above it is broken. Never
test only from inside your own network: nearly every voice problem is invisible
from there. Terms are in the [glossary](glossary.md#voice-and-network-terms).

## The ladder

Check each rung in order and stop at the first one that fails.

### Rung 1: the site

Open `https://chat.example.com` on a phone with Wi-Fi off. Expected: a padlock
and the sign-in page. If not: the DNS A record does not point to your public
IPv4 address, TCP 443 (and 80 for certificates) is not forwarded or not open in
the firewall, or the certificate is wrong. See [TLS certificates](reverse-proxy.md#tls-certificates).
From a computer: `curl -I https://chat.example.com`.

### Rung 2: live updates (WebSocket)

Sign in. The page must not show "Connection lost – reconnecting…" and messages
must arrive without a reload. If it does show it, the proxy does not pass the
WebSocket. In the browser's developer tools (F12 > Network, filter "WS") the
request `/api/ws` must be status **101**; 400, 426 or 502 mean missing `Upgrade`
and `Connection` headers. Fix: [WebSockets](reverse-proxy.md#websockets).
Admin > Status has a *WebSocket* item for this.

### Rung 3: Admin > Status

Open Admin console > Status as the Owner and wait about twenty seconds after a
start. Every amber or red item says what to change. What the items mean:

| Item | Plain meaning | Red or amber means |
|---|---|---|
| Public address | `PUBLIC_URL`: the address members type | It is `localhost`, plain `http://`, or not the address you opened (a proxy must pass `Host` and `X-Forwarded-Proto`) |
| Voice address | The public IP the server tells browsers to send media to, and where it got it | It found none: set `WEBRTC_NAT_1TO1_IP`. If the address it found is private or in `100.64.0.0/10`, a CGNAT warning appears: see [Can I host at home?](quick-start.md#can-i-host-at-home) |
| Voice port | The server listens for media on UDP 50000 | Red: the voice engine did not start (ports busy). Amber: `WEBRTC_UDP_MUX_PORT=0` caps callers. Reads "Listening – not yet confirmed from outside" until a member from outside your network has connected, then "Confirmed: a member from outside connected" |
| TURN relay | The built-in relay for networks that block UDP | It could not bind port 3479, did not answer on it from the server (a router without NAT loopback also causes that although the forward works), or is off with no external TURN: members behind strict networks cannot join |
| WebSocket | Your session reached the server through the proxy as a WebSocket | The proxy does not pass `Upgrade` and `Connection` |
| Reverse proxy | Requests arrive from a trusted proxy | The proxy is not in `TRUSTED_PROXY_CIDRS`, or the setting is too broad |
| Outgoing mail | SMTP is set | Optional |
| Object storage, Database | The containers answer | A container is down or the secrets in `.env` do not match |
| Server clock | The host clock is right | Turn on NTP |

Green does not prove that your router forwards UDP: the server cannot test that
from inside. Use rungs 4 and 5.

### Rung 4: Test my connection

Admin > Status > **Test my connection** runs in your browser. Open it from a
network outside the server's (a phone hotspot, or the phone itself) and read the
verdict: direct UDP works, only the relay works, or nothing works. "Only the
relay works" means calls work but the UDP forward is missing or blocked: fix it
(rung 5) so the relay is not the only way in. "Nothing works" means neither
50000-50050/udp nor 3479 reached the server: check the router and firewall, and
CGNAT. In a terminal, `docker compose exec app mnema-talk doctor` prints the
Status checks with the same fixes.

### Rung 5: a real call between two networks

Join a Talk with a computer on Wi-Fi and a phone on mobile data (two members
share one invite each). Expected: audio both ways within about two seconds.
Two tabs on one computer test only the app, not your network; use them to rule
the network out when there is no sound.

To test the TURN fallback, use a network that blocks UDP (a phone hotspot with a
VPN on works) or read the selected pair in webrtc-internals below. If it works
only there, direct UDP is blocked and the relay carries the call.

## Reading chrome://webrtc-internals and about:webrtc

Browsers show their own view of a call. In Chrome, Edge and Opera open
`chrome://webrtc-internals` (`edge://webrtc-internals`) in a new tab **before**
you join the Talk; in Firefox open `about:webrtc`. Safari has no equivalent.

Each connection lists *candidates*, the addresses it can use:

| Type | Meaning |
|---|---|
| host | The device's own LAN address |
| srflx | The device's public address as seen through STUN |
| relay | An address on a TURN relay |

Under the connection, the **selected** (nominated, succeeded) candidate pair
shows what actually carries the media. How to read it:

- **Only host candidates**: the browser got no answer from any STUN or TURN
  server; nothing outside the LAN can work. The server's address was probably
  not offered: check the announced address.
- **srflx candidates, but the call fails**: the announced address
  (`WEBRTC_NAT_1TO1_IP`) is wrong, or UDP 50000-50050 is not forwarded.
- **Selected pair is relay**: direct UDP failed and the relay carries it. Fine
  for one or two members; fix the forward so everyone does not depend on the relay.
- **Selected pair is srflx or host**: direct UDP works.
- The connection state `failed` means no candidate pair worked.

## The logs

```sh
docker compose logs --since 10m app | grep -E 'webrtc announce|setup check|sfu ice|webrtc client diag'
```

On Synology without a shell: Container Manager > Container > `mnema-talk` > Log.

- `webrtc announce` at startup shows the public IP the server tells browsers
  (and your LAN IP if you listed it), not only `172.x` or `10.x` addresses. If it
  shows only a private address, see "Works on the LAN, not outside" below.
- `sfu ice connected` appears when a member's media path is up. Its
  `remote_type` is `host`, `srflx` or `relay`; a `relay` means that member
  came in through the TURN relay.
- `sfu ice failed` or `sfu ice disconnected`, and a `webrtc client diag` line
  with `"ice":"failed"`, mean packets from that member do not arrive. The
  diag line also lists the candidate types the browser gathered; no `relay`
  entry means no TURN server was on offer.
- `setup check:` lines are the Status items, logged about twenty seconds after
  the start.
- For more detail set `LOG_LEVEL=debug`, run `docker compose up -d`, reproduce
  the problem, then remove the line again.

## Symptoms, causes, fixes

| Symptom | Likely cause | Fix |
|---|---|---|
| The site does not load | DNS, 443 not forwarded, certificate | Fix the A record, forward 443 and 80, check the [certificate](reverse-proxy.md#tls-certificates) |
| Site loads, chat dead, "Connection lost – reconnecting…" | The proxy does not pass WebSockets | [Enable WebSockets](reverse-proxy.md#websockets) |
| 403 on sign-in or on every request | `PUBLIC_URL` does not match the address in the browser (scheme, host and port) | Set `PUBLIC_URL` to exactly that address, `docker compose up -d` |
| Everyone shares one rate limit, or gets locked out together | The app sees the proxy's address instead of the visitors' | Put the proxy address named in Status > Reverse proxy into `TRUSTED_PROXY_CIDRS` |
| No sound even on the same LAN | Microphone permission, wrong input device, a silent microphone | Browser site settings and the in-app audio settings; test two tabs on one computer to rule the network out |
| Works on the LAN, not from outside | UDP 50000-50050 not forwarded, host firewall, wrong announced address | Forward the whole range to the host's LAN address, open the host firewall, check `webrtc announce` |
| Works from outside, not on the LAN | The router has no NAT loopback | Add the server's LAN address after the public one: `WEBRTC_NAT_1TO1_IP=203.0.113.7,192.168.1.20`, or use split-horizon DNS |
| Nobody outside ever connects, though ports are forwarded | CGNAT or DS-Lite: your line has no public IPv4 | [Can I host at home?](quick-start.md#can-i-host-at-home); move to a VPS |
| Connects, then drops after seconds | Wrong announced address, or only part of the range forwarded | Check `webrtc announce`; forward the whole range |
| One person cannot join, the others can | Their network blocks UDP (company, VPN, Opera VPN or WebRTC protection) | Forward 3479 on UDP **and** TCP, run the [probe](#probe-the-turn-relay-from-outside), try another network |
| Wrong `WEBRTC_NAT_1TO1_IP` | A typo, a stale dynamic-DNS name, or only the LAN IP set | Use the public IP or a current dynamic-DNS name, plus the LAN IP; check with `curl -4 https://ifconfig.me` from the server's network |
| A port stays closed although forwarded | The host firewall (ufw, DSM) or a cloud security group blocks UDP 50000-50050 or 3479 | Open them in both places ([platform notes](platform-notes.md#plain-vps)) |
| The desktop client shows a connection error | See the table | [Desktop client errors](desktop-app.md#connecting-to-your-server-errors-and-fixes) |

More on announced addresses, IPv6 and coturn: [operations.md](operations.md#nobody-can-join-voice-or-it-connects-and-drops-after-a-few-seconds).

## Probe the TURN relay from outside

`scripts/turn-probe.py` (standard library only) checks from another network
that the relay answers on UDP and TCP. Copy the file from the repository, run it
**outside** your network (a VPS or a phone hotspot with a laptop), and give the
target in the environment:

```sh
TURN_PROBE_HOST=chat.example.com python3 turn-probe.py            # port 3479 by default
TURN_PROBE_HOST=chat.example.com TURN_PROBE_PORT=3479 python3 turn-probe.py --attempts 3
```

For each transport (UDP and TCP) it sends a STUN request and an unauthenticated
TURN allocation and ends with `RESULT udp: PASS` and `RESULT tcp: PASS`, or
`FAIL`. A pass means the relay answered on that protocol. A `FAIL` on one
protocol only means you forwarded just the other one; `FAIL` on both means the
forward, the firewall or the host name is wrong. The script prints neither your
host name nor any address it resolved.
