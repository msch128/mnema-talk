package setupcheck

import (
	"sort"
	"strings"
)

// Params are the values a check message names: ports, variable values,
// addresses. They travel next to the code so the web app can put them into
// its own translation.
type Params map[string]string

// message is the English text of one check outcome; {name} marks a parameter.
// The web app translates by Code and keeps this text as the fallback for
// codes it does not know.
type message struct{ detail, fix string }

// messages lists every outcome a check can have, keyed by its stable code
// ("<check id>.<outcome>"). Never reuse or rename a code; the web app has a
// translation for each (web/src/i18n/statusCodes.test.ts).
var messages = map[string]message{
	"public_url.local": {
		"PUBLIC_URL is {public_url}, which only this machine can open; invite links, e-mails and browser origin checks use it. That is fine for trying Mnema Talk on this computer, but nobody else can join.",
		"To invite others, set PUBLIC_URL=https://chat.example.com (the address members type into their browser) in .env and run docker compose up -d.",
	},
	"public_url.origin_mismatch": {
		"You opened this page at {origin} but PUBLIC_URL is {public_url}.",
		"If {origin} is the address members use, set PUBLIC_URL={origin}; if you came through a reverse proxy, let it pass the Host and X-Forwarded-Proto headers.",
	},
	"public_url.insecure": {
		"PUBLIC_URL is plain http://, so session cookies lose the Secure flag and the __Host- prefix.",
		"Put a reverse proxy with TLS in front and set PUBLIC_URL=https://chat.example.com (skip this on a trusted LAN).",
	},
	"public_url.ok": {"PUBLIC_URL is {public_url}.", ""},

	"voice_address.development":          {"Development mode: voice uses this machine's own addresses.", ""},
	"voice_address.announced_env":        {"Voice is announced on {ips} (from WEBRTC_NAT_1TO1_IP).", ""},
	"voice_address.announced_public_url": {"Voice is announced on {ips} (taken from PUBLIC_URL).", ""},
	"voice_address.announced_stun":       {"Voice is announced on {ips} (detected with a STUN request).", ""},
	"voice_address.missing": {
		"No public voice address is known, so browsers are told the container's own address.",
		voiceAddressFix,
	},
	"voice_address.missing_detect_failed": {
		"No public voice address is known, so browsers are told the container's own address. Automatic detection failed: {error}.",
		voiceAddressFix,
	},
	"voice_address.missing_turn_only": {
		"No public voice address is known, so browsers are told the container's own address. Only the TURN relay can carry voice.",
		voiceAddressFix,
	},
	"voice_address.missing_detect_failed_turn_only": {
		"No public voice address is known, so browsers are told the container's own address. Automatic detection failed: {error}. Only the TURN relay can carry voice.",
		voiceAddressFix,
	},

	"voice_udp.unavailable": {
		"The voice engine did not start, so Talks have no audio or video.",
		"Another program may be using UDP ports {port_min}-{port_max} on the host: stop it or change WEBRTC_UDP_PORT_MIN and WEBRTC_UDP_PORT_MAX, then restart; the log names the cause.",
	},
	"voice_udp.mux_disabled": {
		"WEBRTC_UDP_MUX_PORT=0 gives every media peer its own port, which caps how many people can be in calls.",
		"Remove WEBRTC_UDP_MUX_PORT (or set it to {port_min}) so all peers share one UDP port.",
	},
	"voice_udp.unconfirmed": {
		"Listening – not yet confirmed from outside. Voice listens on UDP {port}, but no member from outside this network has connected since the server started, so the router forward is unproven.",
		"Forward UDP {port_min}-{port_max} from the router to this host, then open Test my connection from a phone on mobile data (Wi-Fi off); this turns green once it works.",
	},
	"voice_udp.confirmed": {"Confirmed: a member from outside connected ({time}). Voice listens on UDP {port}.", ""},

	"turn.builtin_failed_external": {"A TURN relay is configured for members behind strict firewalls; the built-in relay could not start.", ""},
	"turn.builtin_failed": {
		"The built-in TURN relay could not listen on port {port}: another program on this host is probably using it, so members on networks that block UDP cannot join voice.",
		"Stop whatever uses port {port} (UDP and TCP) or pick another port with WEBRTC_BUILTIN_TURN_PORT (and forward that one), then run docker compose up -d.",
	},
	"turn.voice_unavailable": {
		"The built-in TURN relay is not running because voice is unavailable: members on networks that block UDP cannot join voice.",
		"Fix the voice engine first (see the Voice port check), then run docker compose up -d.",
	},
	"turn.builtin_off_external": {"A TURN relay is configured for members behind strict firewalls; the built-in relay is not running.", ""},
	"turn.unreachable": {
		"The built-in TURN relay runs, but this server could not verify port {port} from the inside; a router without NAT loopback causes this although the forward works.",
		"Open Test my connection from a phone on mobile data to check the relay from outside; if it fails, forward port {port} (UDP and TCP) to this server, or set WEBRTC_BUILTIN_TURN_HOST if members must reach it under another name.",
	},
	"turn.builtin_ok":  {"The built-in TURN relay serves members behind strict firewalls on port {port}; forward it (UDP and TCP) next to the voice ports.", ""},
	"turn.external_ok": {"A TURN relay is configured for members behind strict firewalls.", ""},
	"turn.none": {
		"No TURN relay: members on networks that block UDP cannot join voice.",
		"Optional: remove WEBRTC_BUILTIN_TURN=off to use the built-in relay (forward port {port}, UDP and TCP), or set WEBRTC_TURN_URLS and WEBRTC_TURN_SECRET for an external one.",
	},

	"mail.ok": {"Outgoing mail is configured ({host}).", ""},
	"mail.none": {
		"No outgoing mail: members get no reset link for a forgotten login and e-mail addresses cannot be confirmed.",
		"Optional: set SMTP_HOST, SMTP_PORT, SMTP_USERNAME, SMTP_PASSWORD and SMTP_FROM in .env.",
	},

	"storage.not_configured": {
		"Object storage is not configured, so uploads and avatars do not work.",
		"Set S3_ENDPOINT, S3_BUCKET, S3_ACCESS_KEY and S3_SECRET_KEY in .env.",
	},
	"storage.unreachable": {
		"Object storage does not answer yet; uploads fail until it does.",
		"Check that the seaweedfs container is running (docker compose ps) and that S3_ACCESS_KEY and S3_SECRET_KEY in .env match the ones it was first started with.",
	},
	"storage.ok": {"Object storage is ready.", ""},

	"database.ok": {"The database is reachable.", ""},
	"database.unreachable": {
		"The database does not answer.",
		"Check that the postgres container is healthy (docker compose ps) and that POSTGRES_PASSWORD in .env is the one it was created with.",
	},

	"clock.ok": {"The server clock is within {window} of real time.", ""},
	"clock.skewed": {
		"The server clock is off by about {skew}, which breaks two-factor codes and session expiry.",
		"Turn on time synchronization (NTP) on the host; on a Synology: Control Panel > Regional Options > Time > Synchronize with an NTP server.",
	},

	"network.cgnat": {
		"This home connection has no public IPv4 address of its own (the address {address} is in the carrier-grade NAT range 100.64.0.0/10), so port forwarding cannot work and members outside your network cannot reach voice.",
		noPublicIPv4Fix,
	},
	"network.no_ipv4": {
		"No public IPv4 address was found but IPv6 works: this looks like a connection without its own public IPv4 (DS-Lite), so port forwarding cannot work and members outside your network cannot reach voice over IPv4.",
		noPublicIPv4Fix,
	},
	"network.address_mismatch": {
		"This host's own address {local} differs from the address {public} the internet sees, so its traffic leaves through another gateway and announcing {public} may be wrong.",
		"If this host is reachable from outside on {local}, set WEBRTC_NAT_1TO1_IP={local}; if a router or gateway sits in front, forward the voice ports there.",
	},

	"reverse_proxy.not_trusted": {
		"Requests arrive with X-Forwarded-For from a proxy that is not trusted, so every member looks like one address to rate limits and the login lockout.",
		"Set TRUSTED_PROXY_CIDRS=<address of your reverse proxy> in .env (its container IP, the Docker bridge gateway or its LAN address; never a whole private range).",
	},
	"reverse_proxy.broad": {
		"TRUSTED_PROXY_CIDRS covers a wide range, so any host in it can forge a client address.",
		"List only the reverse proxy's own address in TRUSTED_PROXY_CIDRS.",
	},
	"reverse_proxy.ok": {"No proxy header problems seen.", ""},

	"websocket.ok": {"WebSocket via proxy OK: a browser opened /api/ws and it upgraded ({time}).", ""},
	"websocket.unchecked": {
		"Not checked yet: no browser has opened the live connection (/api/ws) since the server started.",
		"Reload this page; if it stays unchecked, enable WebSocket in your reverse proxy.",
	},
	"websocket.failing": {
		"Browsers tried to open /api/ws {count} times without a WebSocket upgrade (last {time}); the reverse proxy most likely does not pass WebSocket connections, so the app shows “Connection lost”.",
		websocketFix,
	},
}

const noPublicIPv4Fix = "Home connection without own public IPv4 - port forwarding cannot work. Options: ask the internet provider for a public IPv4 address, run Mnema Talk on a VPS, or use an external relay (WEBRTC_TURN_URLS and WEBRTC_TURN_SECRET). See \"Can I host at home?\" in doc/operations.md."

const websocketFix = "Enable WebSocket in your reverse proxy: it must pass the headers Upgrade: websocket and Connection: Upgrade. Nginx Proxy Manager: switch on Websockets Support. nginx: proxy_set_header Upgrade $http_upgrade; proxy_set_header Connection upgrade; proxy_http_version 1.1. Caddy and Traefik do this by default."

const voiceAddressFix = "Set WEBRTC_NAT_1TO1_IP=<your public IPv4 address or dynamic-DNS name> in .env (add the LAN address after a comma for members on the same network), then docker compose up -d."

// Codes returns every check code, sorted.
func Codes() []string {
	out := make([]string, 0, len(messages))
	for c := range messages {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// newCheck builds a check from its catalog entry: the English detail and fix
// are the message with the parameters filled in.
func newCheck(id, code string, level Level, p Params) Check {
	m := messages[code]
	for k, v := range p {
		if v == "" {
			delete(p, k)
		}
	}
	return Check{ID: id, Code: code, Level: level, Params: p, Detail: render(m.detail, p), Fix: render(m.fix, p)}
}

func render(s string, p Params) string {
	if len(p) == 0 {
		return s
	}
	pairs := make([]string, 0, 2*len(p))
	for k, v := range p {
		pairs = append(pairs, "{"+k+"}", v)
	}
	return strings.NewReplacer(pairs...).Replace(s)
}
