# FAQ

Short answers for people setting up Mnema Talk for the first time. The step-by-step
path is the [quick start](quick-start.md); terms are in the
[glossary](glossary.md#voice-and-network-terms).

## Do I need a domain name?

For a community other people reach over the internet, yes. A domain (about ten
euros a year, or a free sub-domain from a dynamic-DNS service) gives you a name
for the HTTPS certificate, and browsers allow microphone and camera only on HTTPS
pages. Without any domain you can still try Mnema Talk on one computer at
`http://localhost:8080` ([quick start](quick-start.md#try-it-on-one-computer-first)).

## My home IP address changes. What do I do?

Use *dynamic DNS*: a name that always points to your current address, such as the
one your router vendor offers (MyFRITZ! on a Fritz!Box) or a free service like
DuckDNS. Point `chat.example.com` at it with a CNAME record, or use the dynamic-DNS
name itself as your domain. For voice you can also give the name to the server:
`WEBRTC_NAT_1TO1_IP=myhome.example.org` is looked up again every five minutes.

## Can I use Cloudflare?

For the website, yes, with a catch. Cloudflare's orange-cloud proxy carries only
HTTP, so it cannot carry voice, and the built-in TURN relay is announced under
the host name of `PUBLIC_URL`: that name must lead to your server itself. Either
leave the record "DNS only" (grey cloud), or keep the orange cloud for the site
and set `WEBRTC_BUILTIN_TURN_HOST` to a second, grey-cloud name that points to
the server ([built-in TURN relay](operations.md#built-in-turn-relay)). The voice
ports must reach the server directly in both cases.

## Can I use Cloudflare Tunnel or Tailscale instead of opening ports?

A tunnel that carries HTTP (Cloudflare Tunnel, ngrok) can serve the website, but
voice is UDP and does not travel through it. Chat would work and calls would not.
Tailscale and similar VPNs carry UDP too, but only between devices that are on
that network, so every member would have to install it; set
`WEBRTC_NAT_1TO1_IP` to the server's address on that network. The project does not
test these setups. For a public community, a VPS with the three open ports is
simpler.

## Does it run on a Raspberry Pi?

Yes, on a Pi 4 or 5 with a 64-bit OS ([platform notes](platform-notes.md#raspberry-pi-and-other-arm-boards)).

## How many people can it carry?

It depends on your upload speed and CPU, because the server forwards every
video and audio stream to every receiver. [Capacity](architecture.md#capacity)
has the measurements, from a 4-core home NAS.

## Does it run on Windows or macOS with Docker Desktop?

It is not a tested hosting target. Docker Desktop is fine for
[trying it on one computer](quick-start.md#try-it-on-one-computer-first). For a
community use Linux, a NAS or a VPS.

## Why can't I reach port 8080 from another computer?

By design. The app port listens on `127.0.0.1` only (`PUBLISH_ADDR`), so that the
only way in is through your reverse proxy, which provides HTTPS. Never
forward 8080 on your router. If your proxy runs on another machine or in a
container, set `PUBLISH_ADDR` to the address the proxy can reach (`0.0.0.0` only
behind a firewall) and list the proxy in `TRUSTED_PROXY_CIDRS`
([reverse proxy](operations.md#reverse-proxy-and-trusted_proxy_cidrs)).

## It works from mobile data but not from my home Wi-Fi

Your router probably cannot send traffic from inside your network to your own
public address and back (*NAT loopback* or *hairpin*). Tell the server its LAN
address too, so devices at home connect to it directly: set
`WEBRTC_LAN_IP=192.168.1.20` (the server's address in your network; the setup
assistant asks for it), or, with a fixed public address,
`WEBRTC_NAT_1TO1_IP=203.0.113.7,192.168.1.20` (public address first). Then run
`docker compose up -d`. The name of the site can also
be answered with the LAN address inside your network (split-horizon DNS, often a
"local DNS record" in the router).

## Do I have to forward 50 UDP ports?

You forward a range, but media uses one of it: all connections share
`WEBRTC_UDP_MUX_PORT`, by default 50000. The rest of the range exists for the
legacy per-peer mode (`WEBRTC_UDP_MUX_PORT=0`) and costs nothing. Forwarding only
the one port works in the default mode, but the documented range keeps both
modes working, so forward all of it ([Network ports](ports.md#inbound-ports)).

## What does the server send to the internet?

A STUN request at startup to find its public address, a release check, a clock
check, and the optional services you turn on (mail, Web Push, link previews).
Everything is listed, with the switch to turn each off, in [Network ports](ports.md#outbound-connections).

## Do I have to edit `.env` by hand?

No. The setup assistant writes a minimal `.env`; everything else has a
default ([.env.example](../.env.example)). The settings you will touch first are
`PUBLIC_URL`, and, on a home network, perhaps `WEBRTC_NAT_1TO1_IP`.

## Where do I ask for help?

[SUPPORT.md](../SUPPORT.md) lists the order: documentation, Discussions, issues.
Include the version (Admin console > System), how you deploy, and what
`docker compose logs --since 10m app | grep -E 'webrtc announce|setup check|sfu ice'`
prints, without secrets ([voice troubleshooting](troubleshooting-voice.md#the-logs)).
