# Trusted desktop client: a signed web app instead of server JavaScript

Status: implemented (roadmap item 0.9.1). The release tooling is in this
repository and needs no server change; the Tauri host that verifies and loads
the bundle is in
[mnema-talk-desktop](https://github.com/msch128/mnema-talk-desktop).

## Why

Microphone, camera and screen payloads are sealed in the browser with
MLS-derived keys, so an honest SFU cannot read them. [SECURITY.md](../SECURITY.md)
states the limit: an active or compromised server serves the web app, so it
can serve a modified one that hands out the keys. In a browser that cannot be
fixed, because the page is the server's page. A desktop client can fix it,
because the client is something the user installed. Until now the desktop
client did not use that freedom: it opened the instance's HTTPS origin in
WebView2 and ran whatever JavaScript the instance sent.

The trusted desktop client ships the web app itself and verifies it. The
instance supplies data, never code. Browser users are unchanged.

## Decisions at a glance

| Question | Decision |
| --- | --- |
| Where does the app come from? | A signed **bundle** (a zip of `web/dist` plus a manifest). One is **built into the installer**; newer ones are **downloaded from the GitHub release assets** of this repository, verified, and cached. |
| How is it signed? | Ed25519 over `manifest.json`, which lists the SHA-256 and size of every file plus `version`, `web_client_api`, `min_server_version` and `security_floor`. Signed in the release workflow with a key kept as an Actions secret. |
| What does the desktop trust? | Public keys **pinned in the desktop build** (a list, for rotation). TLS and GitHub are transport only: a bundle is accepted because of its signature, never because of where it came from. |
| Rollback? | Monotonic semver `version`, a persisted high-water mark, and a signed `security_floor`: bundles below the highest floor ever seen are refused, however the server answers. |
| Server API newer or older than the bundles? | Exact `web_client_api` match, `server.version >= min_server_version`, `bundle.version >= floor`. If none of the verified bundles qualifies the client checks for an update, and otherwise **refuses to connect with an explanation**. It never falls back to server JavaScript. |
| Can the instance's JavaScript still be loaded? | **No.** Not as an opt-in either: a malicious server could trigger the prompt on purpose and ask for consent in its own words. A developer-only build feature is not shipped in releases. |
| How does the local app reach the server? | A **native proxy** in the Tauri host behind a custom protocol, so the WebView still sees one origin and the web app's code (relative URLs, `fetch`, cookies in the browser model) works unchanged. The session cookie lives in the host, never in the page. WebSockets go through three IPC commands. |
| Server changes | **None to the request path or the browser security model.** The server already advertises `version` and `web_client_api` in `/api/health`. This repository adds the signing tooling, a contract file and tests that keep the two numbers in step. |

## How the desktop client loads the app today

`desktop_open_instance` in the desktop host validates the address (HTTPS
origin only), reads `GET /api/health` (`status: ok`, `web_client_api: 1`),
and opens a window on the instance origin. The page is then an ordinary
browser page: the `__Host-` session cookie, the Origin check
(`httpx.CSRF`, allow-list from `PUBLIC_URL` / `CORS_ALLOWED_ORIGINS`), the
SPA CSP (`script-src 'self'`) and the WebSocket at `/api/ws` all behave as in
a browser, which is why no server change was ever needed. The cost is the
trust model: `script-src 'self'` means *the server's* scripts.

## The bundle

### Contents and format

A bundle is one file, `mnema-web-<version>.zip`:

```
manifest.json     what is signed
manifest.sig      base64 Ed25519 signature over the exact bytes of manifest.json
app/<path>        every file of web/dist (index.html, assets/..., wasm, fonts)
```

`manifest.json`:

```json
{
  "schema": 1,
  "name": "mnema-web",
  "version": "0.8.0",
  "web_client_api": 1,
  "min_server_version": "0.7.16",
  "security_floor": "0.0.0",
  "commit": "<source revision>",
  "key_id": "1069c76ff2e11272",
  "files": [{ "path": "index.html", "sha256": "<hex>", "size": 1234 }]
}
```

* `version` is the release version, the same string baked into the web app
  (`__APP_VERSION__`) and the server binary.
* `web_client_api` is the contract number the app speaks. The server
  advertises its own in `/api/health`; `internal/server` and
  `web/desktop-compat.json` are checked against each other by a test.
* `min_server_version` is the oldest server release the app still works
  with. It lives in `web/desktop-compat.json` and is bumped by hand when the
  web app starts to require something new from the server.
* `security_floor` is the oldest bundle version that is still acceptable.
  Release engineering raises it when a bundle is withdrawn for a security
  reason. It also lives in `web/desktop-compat.json`.
* `key_id` is the first 8 bytes of SHA-256 over the raw public key. It only
  selects a pinned key; the signature check decides.
* No timestamp: bundles are reproducible from the tag.

The signature covers the manifest bytes as published. The client verifies
the signature before it parses the manifest, so a hostile manifest never
reaches the parser unauthenticated.

### Signing

`cmd/webbundle` (package `internal/webbundle`, plain `crypto/ed25519`) has
three commands: `build` (directory + key to zip), `verify` (zip + public key)
and `keyid`. The release workflow runs it in a job after the image job:
it builds the web app with `MNEMA_VERSION=<release>`, signs with the Actions
secret `WEB_BUNDLE_SIGNING_KEY`, verifies the result against the public key
committed in this repository (`web/desktop-signing.pub`) and attaches
`mnema-web-<version>.zip` (plus `manifest.json` and `manifest.sig` for
auditing) to the GitHub release. **Without the secret the job signs nothing
and succeeds** (forks). Pull requests never see the secret.

The release workflow builds and signs in separate jobs: `web-build` has no
secrets and installs with `--ignore-scripts`; `web-bundle` holds the key in the
GitHub environment `web-bundle-signing` (limited to `main`), never runs npm,
builds `cmd/webbundle` from the tag's own Go source and signs the downloaded
`web/dist`. Residual risk: the signed files are what the build job produced, so
a poisoned npm dependency that reaches the build output would be signed. The
lockfile pins every dependency and the manifest hash is printed in the job
summary for comparison with the image's build.

The private key is generated once with `openssl genpkey -algorithm ed25519`,
stored only as the environment secret and in the owner's backup directory
`~/.mnema-signing/` (mode 700/600). Only the public key is committed.

### Pinning and rotation

The desktop host embeds a list of `(key_id, public key)` pairs. A manifest is
accepted if one pinned key verifies it. To rotate: ship a desktop release
that pins both keys, sign new bundles with the new key, and retire the old
key from the next desktop release. A leaked key cannot be revoked remotely
for already-installed clients (no revocation channel exists, by design: it
would be a second thing a network attacker could forge); the answer is a
new desktop release with the compromised key removed, and `security_floor`
in bundles signed with the new key.

## How the desktop verifies, caches and selects

### Sources

1. **Built-in.** `mnema-web-<version>.zip` of a release is compiled into the
   desktop executable. It is verified with the same code path at every start,
   so a build with a broken or unsigned bundle fails visibly.
2. **Cache.** `<app data>/web-bundles/mnema-web-<version>.zip`, at most three
   versions. Every cached bundle is **fully re-verified on every load** (hash
   of every file against the signed manifest, no extra or missing entries,
   bounded sizes) and then served from memory. There is no state in which
   unverified bytes are served: modifying one byte of a cached file makes
   that bundle unusable and the client falls back to the next verified one.
3. **Update.** The host asks
   `api.github.com/repos/msch128/mnema-talk/releases/latest` (at most every
   six hours, and immediately when no bundle fits the server) and downloads
   `mnema-web-<version>.zip` when its version is newer than everything held.
   Redirects to GitHub's asset hosts are followed, HTTPS only, with a size
   cap. The result is verified like any other bundle before it is cached.
   `MNEMA_DESKTOP_NO_UPDATE_CHECK=1` turns the request off. The request
   reveals the user's IP address to GitHub and nothing else.

### Zip handling

Entries must be exactly `manifest.json`, `manifest.sig` and `app/<path>` for
every manifest entry; no duplicates, no extras, no `..`, absolute paths,
backslashes or control characters; at most 4000 entries, 64 MiB per file and
256 MiB in total, counted while decompressing (a zip bomb stops at the
limit).

### Rollback protection

The host keeps `state.json` next to the cache:
`{ "security_floor", "last_check" }`. The floor is recomputed from the held
bundles if the file is lost.

* A bundle is refused when `version < security_floor`, where the floor is the
  maximum of every `security_floor` of every manifest that has verified so
  far (built-in, cached, downloaded).
* An update is installed only if its version is greater than the highest
  verified version held. Old but validly signed bundles that an attacker
  replays cannot replace newer ones, and `latest` metadata that moves
  backwards is ignored.
* A server cannot influence these numbers. What a server can do is claim an
  old `version` in `/api/health` so the client picks an older bundle. That
  is why selection never goes below the floor: any bundle at or above the
  floor was declared acceptable by the project, and the choice among them is
  data about compatibility, not about trust.

State is not signed. An attacker who can write the user's application data
can replace the executable as well; that is outside this model.

### Compatibility: bundle versus server

At connect time the host reads `version` and `web_client_api` from
`/api/health` (bounded, no redirects, system TLS) and applies:

| Situation | Behaviour |
| --- | --- |
| A verified bundle has `web_client_api == server` and `server.version >= min_server_version` and `bundle.version >= floor` | Use the highest such version. |
| Server version is not a release (`dev`) | Only `web_client_api` must match. (For local development.) |
| Server `web_client_api` is **newer** than every bundle | Check for an update now. If a matching bundle arrives, use it; else stop with: "This server needs a newer Mnema app than this client has. Update the desktop client." |
| Server is **older** than every usable bundle (below every `min_server_version`, or older bundles are gone or below the floor) | Stop with: "This server runs vX; this desktop client needs at least vY. Ask the administrator to update." |
| `/api/health` has no `web_client_api` | Stop ("not compatible"). |

The chosen bundle is pinned for the lifetime of the instance window. A newer
bundle found later takes effect on the next start of that window; the app
shows "restart to use verified app vX" instead of reloading.

### Server JavaScript: not at all

Loading the instance's own JavaScript is not offered, neither silently nor
as an opt-in. Reasons: the dialog could be triggered by the very server the
feature protects against (it controls `web_client_api` and `version`); a user
who is told "the server's app is different, continue?" has no way to judge
it; and the old mode would keep the original hole one click away. The
legacy path is removed from the host rather than hidden. Whoever needs to
run an unreleased web app signs a bundle with a key of their own and builds a
host that pins that key (`MNEMA_BUNDLE_ZIP` embeds it); the pinned list is in
the source, not configurable at run time.

One address exception exists: an instance on the same machine may be entered
as `http://localhost`, `http://127.0.0.1` or `http://[::1]` (a development
stack). Anything else must be HTTPS with normal system certificate checks.

## Origin and authentication

### Options

| Option | What it takes | Verdict |
| --- | --- | --- |
| **A. Custom origin + CORS + bearer token.** The app runs at `tauri://localhost` and calls `https://instance/api` cross-origin. | Server: allow the desktop origin in CORS, accept a bearer token (new auth path), change WebSocket auth (no cookie across sites; `SameSite=Lax` blocks it; the Origin check rejects it), change the CSP `connect-src`. Web: base-URL plumbing for every `fetch`, `<img>`, `<video>`, attachment link and WebSocket, token storage in the page. | Not simple, and it weakens the browser model: new CORS allow-list entry, a second auth path, a long-lived token in JavaScript land. |
| **B. Existing native session APIs** (`internal/auth/native_*`, unmounted research code). | Mount routes, finish the broker, rewrite the web transport for the whole REST surface. | Large, and aimed at the native-crypto research mode. Not needed here. |
| **C. Proxy inside the Tauri host.** The WebView loads the app from a custom protocol; the host forwards requests to the instance. | No server change. Host: URL handler, cookie jar, response filter, WebSocket bridge. Web: one socket factory. | **Chosen.** |

### The proxy

The host registers the custom protocol `mnema` and opens the instance window
at `mnema://localhost/` (WebView2 on Windows shows it as
`https://mnema.localhost/`). Everything the page requests arrives at the
host:

* **Static files**: path in the verified bundle, else (for non-`assets/`
  paths without a file) `index.html`, exactly as `web/web.go` does. Responses
  carry the SPA CSP of `internal/httpx` (`script-src 'self'
  'wasm-unsafe-eval'`, `connect-src 'self' ipc: ...`) and the same hardening
  headers.
* **`/api/...`**: forwarded to the instance origin with a fixed `reqwest`
  client (system TLS, no redirects, no proxy, bounded body sizes, timeouts).
  Only `/api/` paths, no dot segments, are forwarded. The host sets `Origin`
  to the instance origin itself, so the server's CSRF check sees exactly
  what a browser on that instance would send. It drops any `Origin`,
  `Referer`, `Cookie` and `Host` the page supplied and forwards a short list
  (`Content-Type`, `Accept`, `Accept-Language`, `Range`, `If-None-Match`,
  `If-Modified-Since`, `X-Request-Id`).
* **Cookies**: the host keeps its own cookie jar for the instance (Set-Cookie
  is parsed, `Secure`/expiry honoured, `Set-Cookie` removed from what the
  page receives). The session token **never enters the page**, so even a
  cross-site-scripting bug in the web app cannot read or exfiltrate it. The
  jar is persisted in the application data directory (mode 600) so the user
  stays signed in; protecting it with the OS keychain/DPAPI is a follow-up.
* **WebSocket**: custom protocols cannot carry upgrades, so the page uses a
  `TrustedSocket` class with the browser `WebSocket` surface (`onopen`,
  `onmessage`, `onclose`, `send`, `close`) over three IPC commands:
  `desktop_ws_open`, `desktop_ws_send`, `desktop_ws_close`, plus a Tauri
  channel for events. The host opens `wss://instance/api/ws` with the jar's
  cookie and the instance `Origin`; frames are text, bounded in size and
  count. The chat store's existing reconnect logic is untouched.
* **Responses are data, not code.** Everything from the server is filtered
  before the page sees it: only an allow-list of headers is passed on,
  `Content-Type` of anything that could render as a document in the app
  origin (HTML, SVG, XML, scripts, unknown) is replaced by
  `application/octet-stream` with `Content-Disposition: attachment`, and
  every proxied response carries `Content-Security-Policy: sandbox;
  default-src 'none'` and `X-Content-Type-Options: nosniff`. Without this, a
  hostile server could answer an attachment URL with HTML and run it in the
  app origin, with access to IndexedDB (the MLS state) and IPC. The same
  rule protects the child window used for attachments.
* **Navigation** is limited to the app origin; HTTPS links elsewhere go to the
  system browser after a confirmation, as today. The attachment child window
  and the gaming surfaces use the same protocol and the same bundle.

### Why this keeps the browser model intact

* CSRF: unchanged. The Origin the server sees is the instance's own, set by
  trusted host code.
* Cookies: unchanged flags; the host is a stricter cookie holder than a
  browser (page cannot read it).
* CSP: unchanged for browsers; the desktop app gets an equivalent policy
  from the host.
* Rate limits and lockouts: unchanged; requests come from the user's own
  address and are counted as theirs.
* No allow-list entry such as `tauri://localhost` is added to
  `CORS_ALLOWED_ORIGINS`.

### Web app changes

`web/src/lib/trustedDesktop.ts` reads the identity the host injects before
any page script (`window.__MNEMA_TRUSTED_APP__`, defined by an
initialization script, frozen): `version`, `source` (`builtin` or `cache`),
`web_client_api`, `key_id`, `server_version`. The app uses it to pick
`TrustedSocket`, to hide the browser's "reload for the new version" banner
(replaced by the restart hint), to show **Verified app vX** in the account
menu and to enable the gaming bridge on the local origin. In a browser none
of this exists and nothing changes.

### Isolation between instances

All instances share the local origin, so browser storage is shared. The MLS
state database is already named from `(community id, account id, device id)`
and everything else the app stores is per-account UI preference. A
per-instance origin is not possible on Windows with Tauri's custom-protocol
mapping (it filters `https://<scheme>.localhost`, no sub-domains). The
session cookie and the jar are per instance.

## Threat model for desktop users

| | Active or compromised server |
| --- | --- |
| **No longer possible** | Injecting or replacing JavaScript, WebAssembly, CSS or HTML of the app. Reading the session cookie from the page. Turning an attachment or API answer into code in the app origin. Downgrading to an older, vulnerable app below the `security_floor`. Serving a bundle with another key. |
| **Still possible (unchanged)** | Seeing metadata (who talks when, frame sizes, layers, quality choices, connection timing). Reading text messages and attachments. Asserting group membership and relaying MLS handshakes: adding a member, swapping a leaf, steering a newcomer into its own group, forcing group restarts, withholding heartbeats (see [SECURITY.md](../SECURITY.md)). Denial of service: dropping messages, refusing connections, lying in `/api/health` so that the client refuses to connect. Sending hostile *data* (messages, link previews, ICE servers) that the unchanged app code must handle. |
| **Still possible for the supply chain** | Whoever holds the signing key, or the GitHub account that publishes desktop installers, can ship a malicious app. The key is separate from the server; compromise of a community server does not give it. The installer itself is not signed yet (no Windows code-signing certificate), so how the user obtained it still matters. |
| **Freeze** | A network attacker can withhold updates; the client keeps what it has. The `security_floor` only moves when a newer bundle is seen. |

Browser users remain exactly as in SECURITY.md: the server serves the code.

## Verification plan

* `internal/webbundle`: build, sign, verify, tamper (changed file, extra file,
  missing file, bad signature, wrong key, bad paths, size limits) at the
  repository coverage level.
* CI: build a bundle from `web/dist` with an ephemeral key, verify it with
  the tool, and check that `web/desktop-compat.json` agrees with the server.
* Desktop: Rust unit tests for manifest and zip parsing, signature, floor,
  selection against server health, response filter, cookie jar and header
  allow-lists; UI tests; tamper test on a cached bundle; a local run against
  a throwaway stack. Anything that needs WebView2 (custom protocol mapping on
  Windows, media permission prompts, the gaming overlays) is listed as
  Windows-only in the desktop repository's status page.
