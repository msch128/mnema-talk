# HTTPS and the reverse proxy

Step 3 of the [quick start](quick-start.md). A *reverse proxy* is a small web
server that sits in front of Mnema Talk. It owns the HTTPS certificate, accepts
connections from the internet on port 443 and passes them to the app on
`127.0.0.1:8080`. It carries the web page, the API and the *WebSocket*; it does
**not** carry voice (see [How it fits together](quick-start.md#how-it-fits-together)).

Pick the proxy that fits your machine, then read [WebSockets](#websockets) and
[TLS certificates](#tls-certificates). Reference material (`TRUSTED_PROXY_CIDRS`
rules, `PUBLIC_URL` and Secure cookies): [operations.md](operations.md#reverse-proxy-and-trusted_proxy_cidrs).

| Your machine | Use | Section |
|---|---|---|
| A VPS or any Linux box | Caddy (gets the certificate by itself) | [Caddy](#caddy-vps-or-linux-box) |
| Synology NAS | The DSM reverse proxy | [Synology](#synology-dsm-reverse-proxy) |
| Unraid | Nginx Proxy Manager from Community Applications | [Nginx Proxy Manager](#nginx-proxy-manager-unraid) |
| You already run nginx | A server block | [nginx](#nginx) |

Before any of them: the DNS A record of `chat.example.com` must point to your
public IPv4 address, and TCP 443 (and 80, see below) must reach the proxy.

## Caddy (VPS or Linux box)

Caddy is the shortest way: it requests and renews a Let's Encrypt certificate
without any further setup. Install it by following the official page for your
system, [caddyserver.com/docs/install](https://caddyserver.com/docs/install)
(on Debian and Ubuntu it is a few `apt` commands that add Caddy's package source).

Put this in `/etc/caddy/Caddyfile` (replace the example):

```caddy
chat.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

`8080` is the `PORT` value in `.env`; change both together. Then
`sudo systemctl reload caddy`. Caddy passes WebSocket upgrades and the
`X-Forwarded-For` header by default. Ports 80 and 443 must be open to the
internet (`sudo ufw allow 80,443/tcp`).

## Synology (DSM reverse proxy)

1. Control Panel > Login Portal > Advanced > Reverse Proxy > Create.
2. Source: protocol HTTPS, host name `chat.example.com`, port 443.
   Destination: protocol HTTP, host name `localhost`, port 8080 (the `PORT`
   value in `.env`).
3. Open the **Custom Header** tab of the same entry, press Create > WebSocket.
   This adds the `Upgrade` and `Connection` headers. Do not skip it.
4. Certificate: Control Panel > Security > Certificate > Add > Get a certificate
   from Let's Encrypt, for `chat.example.com`. DSM needs TCP port 80 reachable
   from the internet while it issues and renews. Then press Configure on that
   page and assign the certificate to the `chat.example.com` entry.

The Status tab's *Reverse proxy* item names the `TRUSTED_PROXY_CIDRS=` value
if DSM shows up as an untrusted address. If the proxy runs on another machine,
see `PUBLISH_ADDR` in the [platform notes](platform-notes.md#synology).

## Nginx Proxy Manager (Unraid)

Install *Nginx Proxy Manager* from Community Applications, open its web UI
(port 81) and:

1. Hosts > Proxy Hosts > Add Proxy Host.
2. Details tab: Domain Names `chat.example.com`; Scheme `http`; Forward
   Hostname / IP: the Unraid server's LAN address; Forward Port `8080`; switch
   on **Websockets Support**.
3. SSL tab: SSL Certificate "Request a new SSL Certificate", switch on Force SSL,
   enter your e-mail address, agree to the terms, Save. Port 80 must reach NPM.

NPM runs in its own container and cannot reach the host's loopback address, so
set `PUBLISH_ADDR=0.0.0.0` (or the Unraid LAN address) in the `.env` of the
Mnema Talk folder, list NPM's address in `TRUSTED_PROXY_CIDRS` (the Status tab
names it) and run `docker compose up -d`. Keep 8080 closed on the router.
SWAG and Caddy from Community Applications work the same way.

## nginx

```nginx
map $http_upgrade $connection_upgrade {
    default upgrade;
    ''      close;
}

server {
    listen 443 ssl;
    server_name chat.example.com;
    # ssl_certificate and ssl_certificate_key for this name

    client_max_body_size 60m;          # >= MAX_UPLOAD_SIZE_MB (the ceiling), plus framing

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $connection_upgrade;
        proxy_read_timeout 300s;        # the WebSocket stays open
    }
}
```

Get the certificate with `certbot` or your usual tool.

Every path of the web app, including invite links (`/invite/CODE`) and the
optional vanity link (`/join/NAME`), is served by the app itself; forward
everything to it and add no rewrite or redirect rules for these paths.

## WebSockets

After sign-in the browser opens a WebSocket to `/api/ws` and keeps it open. It
delivers new messages, typing, presence and the signalling for calls. A proxy
that does not forward the `Upgrade` and `Connection` headers breaks exactly this
connection while every ordinary page still loads. Admin > Status has a
*WebSocket* item that turns green when your own session reached the server this
way.

How to see it yourself: sign in, open the browser's developer tools (F12),
Network tab, filter "WS", reload. The request `/api/ws` must have status **101
Switching Protocols**. Status 400, 426 or 502 means the proxy dropped the
headers; turn on "Websockets Support" (NPM), the WebSocket custom header (DSM)
or the `Upgrade` and `Connection` lines (nginx).

## TLS certificates

**Why HTTPS is required.** Session cookies are sent only over HTTPS
(`PUBLIC_URL` starting with `https://` turns on `Secure` cookies), and browsers
allow microphone, camera and screen share only on HTTPS pages and on
`localhost`. A plain-`http://` install can read and write, but not talk.

**Where the certificate comes from.**

| Proxy | Certificate |
|---|---|
| Caddy | Automatic (Let's Encrypt or ZeroSSL), renewed by itself |
| DSM | Control Panel > Security > Certificate > Let's Encrypt |
| Nginx Proxy Manager | SSL tab > Request a new SSL Certificate |
| nginx | `certbot --nginx -d chat.example.com` |

**Port 80.** Let's Encrypt normally proves you own the name by fetching a file
over plain HTTP, so TCP 80 must reach the proxy while the certificate is issued
and renewed (about every 60 days). If your provider blocks port 80, ask the
proxy for a DNS challenge instead (Nginx Proxy Manager supports it for many DNS
providers; Caddy needs a DNS plugin). IPv6-only or DS-Lite lines have no
reachable IPv4 port 80 at all: see [Can I host at home?](quick-start.md#can-i-host-at-home).

**Test it.** From outside your network, run `curl -I https://chat.example.com`.
It should print `HTTP/2 200` (or 302) with no certificate error.

**Certificate errors, translated.**

| The browser or client says | Cause | Fix |
|---|---|---|
| Name mismatch (`ERR_CERT_COMMON_NAME_INVALID`) | The certificate is for another name | Issue it for exactly `chat.example.com`; check that the proxy entry uses it |
| Not trusted / self-signed (`ERR_CERT_AUTHORITY_INVALID`) | A self-made certificate, or a proxy default certificate | Use Let's Encrypt; the desktop client accepts only trusted certificates |
| Expired (`ERR_CERT_DATE_INVALID`) | Renewal failed (port 80 closed) or the computer's clock is wrong | Open port 80, renew, check the clock of the device and of the server |
| Works in a browser, fails in the desktop client | An antivirus or network filter intercepts HTTPS | See [desktop client errors](desktop-app.md#connecting-to-your-server-errors-and-fixes) |

After HTTPS works, check [the WebSocket](#websockets), then continue with
[Step 4](quick-start.md#step-4-open-the-three-ports).
