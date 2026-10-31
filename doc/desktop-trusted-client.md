# Trusted desktop client: a signed web app instead of server JavaScript

Status: implemented and shipped. The release tooling is in this repository and
needs no server change. The hosts that verify and load the bundle are in
[mnema-talk-desktop](https://github.com/msch128/mnema-talk-desktop): a Tauri
host for Windows and macOS and an Electron client for Linux.

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
| Rollback? | Monotonic semver `version`; a persisted **high-water mark** per `web_client_api` (the highest bundle version ever verified), below which no bundle is run, and a signed `security_floor`: bundles below the highest floor ever seen are refused. However the server answers. There is deliberately no "older server" override (see [Rollback protection](#rollback-protection)). |
| Server API newer or older than the bundles? | Exact `web_client_api` match, `server.version >= min_server_version`, `bundle.version >= floor`, `bundle.version >= high-water mark` of that contract. If none of the verified bundles qualifies the client checks for an update, and otherwise **refuses to connect with an explanation**. It never falls back to server JavaScript. |
| Can the instance's JavaScript still be loaded? | **No.** Not as an opt-in either: a malicious server could trigger the prompt on purpose and ask for consent in its own words. A developer-only build feature is not shipped in releases. |
| How does the local app reach the server? | A **native proxy** in the Tauri host behind a custom protocol, so the WebView still sees one origin and the web app's code (relative URLs, `fetch`, cookies in the browser model) works unchanged. The session cookie lives in the host, never in the page. WebSockets go through three IPC commands. |
| Server changes | **None to the request path or the browser security model.** The server already advertises `version` and `web_client_api` in `/api/health`. This repository adds the signing tooling, a contract file and tests that keep the two numbers in step. |

## How the first desktop client loaded the app

Before the signed bundle, `desktop_open_instance` in the desktop host validated the address (HTTPS
origin only), read `GET /api/health` (`status: ok`, `web_client_api: 1`),
and opened a window on the instance origin. The page was an ordinary
browser page: the `__Host-` session cookie, the Origin check
(`httpx.CSRF`, allow-list from `PUBLIC_URL` / `CORS_ALLOWED_ORIGINS`), the
SPA CSP (`script-src 'self'`) and the WebSocket at `/api/ws` all behave as in
a browser, which is why no server change was ever needed. The cost was the
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
  It lives in `web/desktop-compat.json` and only ever rises. The release
  workflow raises it to the release's own version when the release contains a
  security fix (see "Security floor" below); it can also be raised by hand in
  that file. A bundle's floor never exceeds its own version.
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

The signed bytes are built inside the protected job. `web-bundle` runs in the
GitHub environment `web-bundle-signing` (limited to `main`) and builds
`web/dist` itself from the release tag, so no artifact from an unprivileged job
is ever signed. `npm ci --ignore-scripts` and `npm run build` each run in a
throwaway container (the digest-pinned node image of the Dockerfile's web
stage) with all capabilities dropped, no secret, no Docker socket, only a copy
of `web/` mounted, and no network for the build. Code that runs during the
build, even from a dependency, can therefore change only that copy: it cannot
alter the job's environment, `PATH`, outputs or the signing step. The job
builds `cmd/webbundle` from the tag's own Go source in a step without the key
and hands the key to the single `webbundle build` step.

Residual risk: a poisoned npm dependency that is already in `package-lock.json`
still reaches the build output and would be signed, because the image build
uses the same lockfile (so comparing against the image proves nothing). The
lockfile pins every package with an integrity hash, Dependabot waits out a
cooldown before proposing updates, and the manifest hash is printed in the job
summary so the owner can rebuild on a second machine and compare. This job's
behaviour needs a real release run to be confirmed end to end; the container
steps were run locally against the same Dockerfile image.

#### Security floor

`web/desktop-compat.json` carries the baseline floor. Because only the three
release-please files may change in a release PR, the floor is raised **at
signing time**: the `web-bundle` job runs `scripts/security-floor.sh` over the
commits since the previous tag and, if any of them carries a
`Security-Floor: raise` trailer (the last paragraph of the commit message, so
for a squash merge the end of the pull request description), passes
`-security-floor <release version>` to `webbundle build`. The flag only
raises, never lowers, the value from the compat file. The commit subject plays
no part: a `fix(security)` title alone neither raises the floor nor can a
lookalike scope such as `docs(security-notes)` raise it by accident. A subject
with the `security` scope but no trailer only prints a warning in the job log.
So a vulnerability fix in anything the bundle ships (the web app: markdown,
media, auth, message rendering, crypto) must carry the trailer; the maintainer
checklist in `AGENTS.md` repeats this. A fix without the trailer leaves the
floor where it was, and old clients keep accepting older bundles until the next
release that raises it.

The job builds the bundle tool in its own step without the key and passes
the key only to the single `webbundle build` step.

The private key is generated once with `openssl genpkey -algorithm ed25519`,
stored only as the environment secret and in the owner's backup directory
`~/.mnema-signing/` (mode 700/600). Only the public key is committed.

### Pinning and rotation

The desktop host embeds a list of `(key_id, public key)` pairs. A manifest is
accepted if one pinned key verifies it. These compiled-in keys are the default
until the client has fetched its first signed trust statement (next section).
A key can also be retired by a new desktop release that no longer pins it, but
that path is slow and an attacker holding the key can front-run it; the
trust statement is the remedy for a leaked key.

### Key revocation: the offline root key and the trust statement

A leaked CI signing key must be revocable without shipping a new client, so a
third key exists that never touches CI: the **offline root key** (Ed25519).
Its public half is committed as `desktop-trust/root.pub` and pinned in the
desktop client. It signs one small statement:

* `desktop-trust/trust.json`: `schema`, `serial` (grows with every statement),
  `issued_at`, `expires_at` (UTC, at most 400 days after `issued_at`; the CLI
  default is 180 days), `bundle_keys` (allowed web bundle public keys, raw Ed25519 in
  hex), `revoked_bundle_keys` (key ids), `updater_keys` (allowed Tauri updater
  public keys, in the text form of `tauri.conf.json`) and
  `revoked_updater_keys` (minisign key ids, hex in file order) and `max_version`
  (optional caps, see below).
* `desktop-trust/trust.json.sig`: base64 Ed25519 signature over the exact bytes
  of `trust.json`.

Both files are served from
`https://raw.githubusercontent.com/msch128/mnema-talk/main/desktop-trust/`.
The transport is untrusted; only the signature counts. No release is needed:
merging a new statement to `main` publishes it.

How the client uses it (the desktop repository implements the same format in
Rust):

* It fetches the statement at start and at every six-hourly check, verifies the
  signature with the pinned root key before parsing anything, and keeps the
  last good statement on disk.
* **Anti-rollback.** The highest serial ever accepted is persisted. A statement
  with a lower serial is refused; an equal serial changes nothing.
* **Authoritative.** Once a client holds a statement, it accepts web bundles
  only from keys listed in `bundle_keys` and not in `revoked_bundle_keys`. The
  compiled-in keys no longer count. Bundles already held (built-in or cached)
  are checked again; those signed by a withdrawn key are dropped (and their
  cache files removed).
* **Rollback marks are per key.** The client records which key raised each
  rollback mark (highest bundle version per contract, and the security floor).
  The marks in force are the highest ones raised by keys that are still
  trusted. Revoking a key therefore clears the marks a forged high-version
  bundle raised, but never drops below what a still-trusted key reached or
  declared. Until a bundle from an allowed key at or above the mark exists, an
  older one is refused with a clear message.
* **Explicit caps.** A statement may carry `max_version` (for example
  `{"1": "0.9.0"}`, web client API number to version; sign with
  `-max-version 1=0.9.0`). When a client first takes that statement, it lowers
  its recorded marks for that API to at most the cap and drops held bundles
  above it, even if no trusted bundle exists yet. Caps are not carried over to
  later statements and a cap above the legitimate versions does nothing, so use
  it only to clear a forged mark.
* **Freshness (freeze protection).** An attacker who blocks the fetch must not
  keep a stale statement in force. A statement past `expires_at` is never
  accepted, and while the one in force has expired, or this round's fetch
  failed, the client installs **no new bundle and no updater package**. It keeps
  running the already verified installed bundle, and an expired statement still
  applies its revocations. Re-sign before expiry (below).
* **Fail closed, stay usable.** If a statement cannot be fetched or does not
  verify, the last good one stays in force (or the compiled-in keys, if there
  never was one). If the only bundle left was signed by a revoked key, the
  client says so and asks for an update instead of running it.
* **Updater.** The Tauri updater verifies installers with one public key from
  `tauri.conf.json`. The client additionally refuses any update whose signature
  names a revoked updater key id, and, for key rotation, uses the matching
  public key from `updater_keys` when the signature names one that is not the
  built-in key. Limit: the updater is only as trustworthy as the signed
  installer it checks; revocation cannot undo an installer that a client has
  already run.
* The statement is cumulative for revocations: `webbundle trust sign -prev`
  carries earlier revocations forward, and a key that was revoked cannot be
  allowed again.

Root key handling: the private key lives only at
`~/.mnema-signing/offline-root.key` (directory mode 700, file mode 600). It is
never a GitHub secret and never in CI. Keep a copy offline (USB stick or
password manager); losing it means a new desktop release is the only way to
change the root key.

#### Renewing the statement (every 3 months)

A statement is valid for 180 days. Renew it well before then, on the machine
that holds the root key, with one command, then merge the result:

    go run ./cmd/webbundle trust sign -key-file ~/.mnema-signing/offline-root.key \
      -out desktop-trust -prev desktop-trust/trust.json -renew

`-renew` keeps the allowed keys and all revocations and raises the serial.
The previous statement must carry a valid signature (`trust.json.sig`, or
`-prev-sig`) from this root key; the tool refuses an unsigned or altered file, so
nobody can get a key root-signed by editing `desktop-trust/` in a pull request.
Before signing it prints the full result, allowed and revoked keys with short ids
and `+`/`-` against the previous statement, and asks for confirmation. Read it:
every allowed key must be one you recognise.
`go run ./cmd/webbundle trust verify` prints the expiry
(`-min-valid-days N` also fails when fewer than N days remain). A client that
cannot get a fresh statement keeps working but stops installing updates. The
weekly workflow `trust-expiry.yml` runs that check with 60 days and fails
early enough to renew in time; the runbook is in
[operations.md](operations.md#renewing-the-desktop-trust-statement).

#### Runbook: a CI signing key leaked

Assume the leaked key is the web bundle key (`web-bundle-signing`). The
updater key works the same with `-updater-key` and `-revoke-updater-key`.

1. Generate a new key pair locally (never in CI):
   `openssl genpkey -algorithm ED25519 -out new.key` and
   `openssl pkey -in new.key -pubout -out new.pub`.
2. Replace the `WEB_BUNDLE_SIGNING_KEY` secret of the `web-bundle-signing`
   environment with `new.key`, commit `new.pub` as `web/desktop-signing.pub`
   and delete `new.key`. Rotate the environment's access if the leak was
   through GitHub.
3. Issue the statement on the machine that holds the offline root key:
   `go run ./cmd/webbundle trust sign -key-file ~/.mnema-signing/offline-root.key
   -out desktop-trust -prev desktop-trust/trust.json -bundle-key web/desktop-signing.pub
   -revoke-bundle-key $(go run ./cmd/webbundle keyid -pub OLD.pub)`
   (use the old key's id; `OLD.pub` is the previous `desktop-signing.pub`).
   Check it with `go run ./cmd/webbundle trust verify`.
4. Open a pull request with `desktop-trust/` and `web/desktop-signing.pub`
   and merge it. Clients pick the statement up at their next start or
   six-hourly check and stop accepting the old key at once.
5. Cut a release so a bundle signed with the new key exists
   (raise `security_floor` in `web/desktop-compat.json` if the old key's
   bundles were vulnerable). Clients whose only bundle was signed by the
   revoked key show a message and fetch the new bundle on that check.

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
`{ "floors", "marks", "last_check" }`. Both numbers are recorded **per signing
key** (by key id), so a key that is revoked later takes the marks it raised
with it:

* `floors` maps each key id to the highest `security_floor` that key ever
  declared in a verified manifest.
* `marks` maps each `web_client_api` to, per key id, the highest bundle version
  that key ever signed and this machine verified (built-in, cached or
  downloaded).

The floor and the high-water mark in force are derived from these: the highest
value of any key that is still trusted. `security_floor` and `high_water` in
the file are the older, signer-less format; they are read once as the unknown
signer (always counted) and no longer written. The marks are rebuilt from the
held bundles if the file is lost. They only rise, except in two cases that
exist to undo a forged mark: a key that a trust statement revokes stops
counting (the marks in force fall back to what still-trusted keys reached or
declared), and a statement with `max_version` lowers the recorded marks and
floors to at most the cap (see [Key revocation](#key-revocation-the-offline-root-key-and-the-trust-statement)).

* A bundle is refused when `version < security_floor`, where the floor is the
  maximum of every `security_floor` declared by a still-trusted key's verified
  manifests.
* **Strict high-water rule.** A bundle is never selected when its version is
  below the high-water mark of its contract, even if it is validly signed,
  above the floor and the only one the server's version fits. The rule is "never
  run a bundle older than one that was already verified here". Marks are kept
  per `web_client_api`, because bundles of different contracts cannot replace
  one another; a client that talks to servers of two generations keeps both.
* An update is installed only if its version is greater than the highest
  verified version held. Old but validly signed bundles that an attacker
  replays cannot replace newer ones, and `latest` metadata that moves
  backwards is ignored.
* A server cannot influence these numbers. What a server can do is claim an
  old `version` in `/api/health` so the client would pick an older bundle.
  The floor alone did not stop that, because the floor (currently only raised
  for security fixes) can lag far behind. The high-water mark does: a
  server that claims an old version now gets the refusal "needs X or newer",
  not an old bundle.

Trade-off against older servers: once the client has verified a bundle with
`min_server_version` N, a server older than N is refused even if an older
signed bundle that supports it is still in the cache. Older servers are
exactly where known-vulnerable bundles are most useful to an attacker, and the
alternatives (a per-instance opt-out, or a "compatibility mode" that runs the
older bundle) make the attacker's claim a user prompt, which the server can
word itself. The remedy is to update the server; the message names the version
needed. A reinstall of the desktop client does not lower the marks either as long as its
application data directory survives; a fresh data directory starts from the
built-in bundle of that installer, which is the newest the installer knows.
If the newest verified bundle is deleted from the cache (and a newer one is not
downloaded), the client refuses with "newest verified app no longer
available" instead of running an older one, and an update restores it.

State is not signed. An attacker who can write the user's application data
can replace the executable as well; that is outside this model.

### Compatibility: bundle versus server

At connect time the host reads `version` and `web_client_api` from
`/api/health` (bounded, no redirects, system TLS) and applies:

| Situation | Behaviour |
| --- | --- |
| A verified bundle has `web_client_api == server` and `server.version >= min_server_version` and `bundle.version >= floor` and `bundle.version >= high-water mark` | Use the highest such version. |
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
| **B. A native session API with its own web transport.** (Existed as unmounted research code; removed from this repository.) | Mount routes, finish the broker, rewrite the web transport for the whole REST surface. | Large, and aimed at the native-crypto research mode. Not needed here. |
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
  headers (the desktop shells apply the strict web CSP `style-src 'self'; style-src-attr 'none'`
  only for web bundles >= 1.1.0; older bundles keep the old policy).
* **`/api/...`**: forwarded to the instance origin with a fixed `reqwest`
  client (system TLS, no redirects, no proxy, timeouts, and bodies buffered in memory up to 128 MiB: the webview protocol cannot stream, so a larger answer is refused up front from its declared length; audio and video arrive in `Range` pieces).
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
  jar is persisted so the user stays signed in: in the macOS Keychain or
  Windows Credential Manager (service `mnema-desktop-session`, one entry per
  instance). Only where no keychain exists, or it refuses the write, the
  host falls back to a mode-600 file `sessions/<hash>.json` in the application
  data directory; a file written by an earlier version moves into the keychain
  at the next start and is deleted. A program running as the same user can
  still ask the keychain; what changes is that copies of the profile, backups
  and file listings no longer carry a live session.
* **WebSocket**: custom protocols cannot carry upgrades, so the page uses a
  `TrustedSocket` class with the browser `WebSocket` surface (`onopen`,
  `onmessage`, `onclose`, `send`, `close`) over three IPC commands:
  `desktop_ws_open`, `desktop_ws_send`, `desktop_ws_close`, plus a Tauri
  channel for events. The host opens `wss://instance/api/ws` with the jar's
  cookie and the instance `Origin`; frames are text, bounded in size and
  count. The chat store's existing reconnect logic is untouched. The path is
  `/api/ws`, with the one query the server understands, `?dm=1` (conversation
  events, 1.1), when the host announces the capability `ws_query` in the
  instance window's identity; a host without it only gets the plain path, so a
  1.1 bundle never asks an older client for a socket it would refuse.
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
  system browser after a confirmation, as today. The instance window may also
  navigate to same-origin `/api/` URLs (download and attachment links), and a
  link opened in a new window goes to the attachment child window. Both are
  answered by the proxy, so what arrives is forced into a sandboxed download
  or inline-safe media, never a document of the app origin. The attachment child window
  and the gaming surfaces use the same protocol and the same bundle.

* **OS notifications and the app badge** (host capability `notify`). The
  identity of the instance window carries `capabilities: ["notify"]`; the
  selector, gaming surfaces and the attachment window announce none, and a
  client from before this feature announces no list at all, in which case the
  app falls back to the browser's Notification and Badging APIs. The commands
  are `desktop_notify_open` (a channel that reports which toast was clicked, as
  `{channel_id, message_id}`), `desktop_notify` (`title`, `body`, `channelId`,
  `messageId`), `desktop_notify_permission` and
  `desktop_notify_request_permission` (both answer `granted`, `denied`,
  `default` or `unsupported`) and `desktop_set_badge` (`count`). They are
  allowed only in the instance window, never on the selector or a gaming
  surface. The host treats everything as untrusted text: control characters
  are flattened, title and body are truncated, ids must be plain identifiers,
  and toasts are rate limited, so a hostile server cannot show markup or spam
  the desktop. The OS owns the permission and the host reports it: on macOS
  the system asks once (UserNotifications), when the page calls
  `desktop_notify_request_permission` or at the first toast, and `default`
  means it has not been asked yet; Windows and Linux never ask and answer
  `granted` (a toast the person turned off in the system is dropped without an
  error). A page that gets an unknown-command error from these two commands (a
  client from before them) keeps treating the permission as `granted`. Clicking
  a toast is reported by the platform itself (the toast's activation on Windows,
  the notification delegate on macOS, the `click` event on Linux) and also
  brings the window forward. Windows shows a dot as the taskbar overlay, macOS
  and Linux the count on the dock or launcher icon. This needs no
  `web_client_api` bump: the web app detects the capability.
* **Incoming calls in toasts** (host capability `notify_call`, optional, on top
  of `notify`). A Talk started in a direct message rings the other members (see
  [Talks](talks.md#ringing)); the page then shows an incoming call card of its
  own and, only while the window is hidden or without focus, a toast through
  `desktop_notify` with the title "{name} is calling you" and the body "Direct
  message · Click to join" (German: "{name} ruft dich an", "Direktnachricht ·
  Klicken zum Beitreten"). A host that announces `notify_call` in the instance
  window's `capabilities` additionally gets `kind: "call"` in the arguments of
  `desktop_notify` and the command `desktop_notify_close` (`channelId`). For
  `kind: "call"` the host may keep the toast on screen until it is used (Windows:
  the incoming-call toast scenario; macOS: a time-sensitive notification;
  Linux: urgency critical and a resident notification), offer a **Join** and a
  **Dismiss** button next to the toast itself, and report the button in the
  click event as `{channel_id, message_id, action: "join"}` (`action` is absent
  for the toast itself and for Dismiss, which the host handles without telling
  the page). A click of any kind still brings the window forward. The page
  opens the conversation at the invitation for the toast, and joins the Talk as
  well for `action: "join"`. `desktop_notify_close` withdraws the toast of that
  channel when the call stopped ringing (answered on another device, cancelled,
  timed out). The host treats the arguments like the other notification
  arguments (flattened, truncated, plain identifiers, rate limited). A host
  without the capability keeps showing the call as the ordinary toast, and a
  click on it already opens the conversation. No `web_client_api` bump.
* **Unread report** (host capability `unread`, 1.1; the multi-instance
  client). One window of the client now shows one server out of several, and
  the client lists them on a rail with an unread dot and a mention count each.
  A host that announces `unread` in the instance window's `capabilities` gets
  `desktop_report_unread` (`unread`: channels with unread messages that may
  notify, `mentions`: unread mentions; both whole numbers, 0 to 999) instead of
  `desktop_set_badge`, and sums the counts over its instances for the taskbar
  or dock badge. `Platform.reportUnread` / `supportsUnreadReport`
  (`web/src/lib/platform.ts`) do the feature detection, so a client without the
  capability keeps getting `desktop_set_badge`, and a browser gets neither. No
  `web_client_api` bump. Allowed only in the instance window, like the other
  notification commands.
* **Mark all as read** (from the rail's menu, desktop 1.3). The host cannot
  reach the chat store, so it runs
  `window.dispatchEvent(new CustomEvent('mnema-desktop:mark-all-read'))` in the
  page of that server. The instance window (`role: 'instance'` only) marks every
  channel and conversation read through the same path as the sidebar
  (`useDesktopMarkRead`); the new counts reach the rail by the unread report
  above, which fires whenever the read states change. Nothing is sent back.

### Why this keeps the browser model intact

* CSRF: unchanged. The Origin the server sees is the instance's own, set by
  trusted host code.
* Cookies: unchanged flags; the host is a stricter cookie holder than a
  browser (page cannot read it).
* CSP: unchanged for browsers; the desktop app gets an equivalent policy
  from the host (the desktop shells apply the strict web CSP `style-src 'self'; style-src-attr 'none'`
  only for web bundles >= 1.1.0; older bundles keep the old policy).
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

### Several servers at once

Since the 1.1 client one window shows several servers: a **rail** on the left
(a page of the host, independent of every bundle and server) lists them with an
unread dot and a mention count each, and the page of one server sits next to
it. One account per server, no federation; at most nine, reached with Ctrl+1 to
Ctrl+9 (Cmd on macOS), reordered by dragging or Alt+arrow, removed from the
tile's menu. Servers stay fully separate:

* **A page is tied to its server by its webview label** (`i-<id>`, the id is the
  first 8 bytes of SHA-256 over the instance address, hex). The protocol
  handler and every host command look the server up from the label of the
  calling webview and never from an argument, so a page cannot name another
  server's session, socket, counters or toasts. The capabilities are by
  webview: pages (`i-*`) get the socket, notification, unread and gaming-bridge
  commands, only the rail gets `rail_*` (switch, add, move, remove, reconnect)
  and only the page for adding a server gets `desktop_open_instance`.
* **Session**: one cookie jar per server, stored in the keychain under the id
  of that server (one entry per instance, as before). One per origin: the
  `/api/health` document carries no community id, so none is part of the key.
* **Browser storage** is separate per server, because the pages share the
  local origin and would otherwise share IndexedDB (the MLS state) and
  `localStorage`: a WebView2 user-data folder per server on Windows, a
  `WKWebsiteDataStore` per server on macOS 14 and newer, a session partition
  (`persist:inst-<id>`) per server in the Electron client. Where a platform
  cannot separate the storage (macOS before 14) the client runs one server
  rather than share it. The host's own pages use a storage of their own, too.
* **Sockets**: `desktop_ws_*` act on the calling server's relay only, with its
  own budget. The relay opens `/api/ws` and, when the page asks, `/api/ws?dm=1`
  (conversation events); the page announces nothing else the host would accept.
* **Bundles**: the verified bundle is chosen per server from its own
  `/api/health`, so a 1.0.x and a 1.1 server can run side by side, each with
  the web app that fits it and the style policy of that bundle version. The
  high-water mark stays one per `web_client_api`. A server that is too old or
  too new keeps its place and shows the refusal in its own tab only; "Reconnect"
  in its menu asks again.
* **Counts and the badge**: pages report `unread` and `mentions`; the badge of
  the taskbar or dock is the sum of the mentions. A toast names its server once
  there are several, and using it brings that server forward and reaches only
  its page.
* **Gaming** (Windows overlay, shortcuts, sidepeek) belongs to one server at a
  time: the one that owns the Talk the person is in. Another server's state is
  ignored while that Talk is live, and commands go back to that server's page
  with its own token.
* **Background servers** are hidden, not stopped: their relay socket, counters
  and calls keep running, the engine only stops painting them.
* **Removing a server** wipes its keychain entry (and the fallback file), its
  browser storage (the engine's storage is cleared, then the profile folder or
  data store is deleted, with a sweep at the next start for what a cut-short
  removal leaves), its sockets, toasts and gaming token, and its entry in the
  list. The list (`instances.json`) holds addresses only.
* **Updating from the single-server client**: that client kept every server in
  one shared browser storage and recorded none of them (only trust state,
  bundles and per-origin sessions), so the storage belongs to no one server and
  its owner cannot be proven. It is **never handed to a server**: it is wiped
  once, on the first start of the new version, and every server starts with a
  storage of its own. Stored sessions stay per exact origin as a sign-in
  convenience. The cost is one new sign-in and a new device key for the
  end-to-end media; messages live on the server and are unaffected.

A server that ended the session (the `kicked` event of `/kick`, a password
change, a sign-out elsewhere) leaves a stale cookie in the jar, as a browser
does: requests are answered 401 until the person signs in again, which replaces
the cookie. Nothing reconnects by itself with a refused session.

## Updating the client itself

The signed web bundle covers the web app. The program around it (the shell) is
replaced by its own, separate mechanism, so that users never have to download
an installer by hand after the first time.

* **Feed and key.** On Windows the installer is a per-user NSIS package; macOS
  has a signed `.app.tar.gz` and Linux signed AppImages. All are built by the
  [mnema-talk-desktop](https://github.com/msch128/mnema-talk-desktop) release
  workflow on a `vX.Y.Z` tag. Installed clients read
  `https://github.com/msch128/mnema-talk-desktop/releases/latest/download/latest.json`
  at every start and every six hours (the interval of the bundle check). The
  feed is not trusted: it only names an installer and a minisign signature.
  The updater key (Ed25519, minisign format; the official Tauri updater) is a
  **different key from the bundle key** and its public half is pinned in
  `tauri.conf.json` of the client. The Electron client for Linux applies the same
  rules in TypeScript; a `.deb` installation is owned by the package manager, so
  the client only announces a newer version.
* **What is verified.** The installer runs only if its signature verifies
  against the pinned key, the version recorded inside the signature equals the
  version the feed announces (`requireSignedVersion`: an old signed installer
  cannot be passed off under a new version number), the version is strictly
  newer than the installed one (no downgrade), and the download URL is HTTPS on
  `github.com`. Where the bytes came from decides nothing. "On next start"
  keeps the verified installer on disk and verifies it again against a freshly
  fetched feed before it runs; an installer that changed on disk, or that a newer
  release replaced, is deleted.
* **Install.** Per user (`currentUser`), passive mode: no administrator prompt,
  the app restarts afterwards. This does not depend on a platform signature.
* **User control.** Update now, On next start, Skip this version (a newer
  version is offered again), retry after a failure.
  `MNEMA_DESKTOP_NO_UPDATE_CHECK=1` turns the check off.
* **Key custody.** The private key is a secret of the protected
  `desktop-updater-signing` environment of the desktop repository (usable by
  tags `v*` only) and in the owner's offline backup. It is separate from the
  bundle key and from every server.
* **Rotation and revocation.** Clients trust exactly the key in their own
  config. To rotate, publish a release that carries the new public key but is
  signed with the **old** key, and only then switch to the new key. A client that
  never received that release cannot install releases of the new key and needs a
  manual install. If the updater key is compromised, a trust statement signed
  by the offline root key revokes it and lists its replacement (see "Key
  revocation"); clients refuse installers signed by a revoked key and accept the
  replacement key. A bad release is replaced by a higher version, never by
  lowering.
* **Residual risk.** Whoever holds the updater key, or both the GitHub account
  and a way around the protected environment, can ship malicious code to every
  installed client. A network attacker can withhold updates (freeze) but not
  forge them. The first installer download still relies on how the user got it
  (see the installer gap below).

## Threat model for desktop users

| | Active or compromised server |
| --- | --- |
| **No longer possible** | Injecting or replacing JavaScript, WebAssembly, CSS or HTML of the app. Reading the session cookie from the page. Turning an attachment or API answer into code in the app origin. Downgrading to an older, vulnerable app: below the `security_floor`, or below the highest bundle this client already verified for the server's contract. Serving a bundle with another key. |
| **Still possible (unchanged)** | Seeing metadata (who talks when, frame sizes, layers, quality choices, connection timing). Reading text messages and attachments. Asserting group membership and relaying MLS handshakes: adding a member, swapping a leaf, steering a newcomer into its own group, forcing group restarts, withholding heartbeats (see [SECURITY.md](../SECURITY.md)). Denial of service: dropping messages, refusing connections, lying in `/api/health` so that the client refuses to connect. Sending hostile *data* (messages, link previews, ICE servers) that the unchanged app code must handle. |
| **Still possible for the supply chain** | Whoever holds the signing key, or the GitHub account that publishes desktop installers, can ship a malicious app. The key is separate from the server; compromise of a community server does not give it. The installer itself is not Authenticode-signed yet (see "Known gap" below), so how the user obtained it still matters. |
| **Freeze** | A network attacker can withhold updates; the client keeps what it has. The `security_floor` and the high-water marks only move when a newer bundle is seen. |

Browser users remain exactly as in SECURITY.md: the server serves the code.

### Known gap: the installers have no platform signature

The signed bundle protects the web app, not the program around it. The Windows
installer has no Authenticode signature and the macOS app is ad-hoc signed, not
notarized, because the project does not buy certificates. Consequences:
SmartScreen and Gatekeeper warn on first run, and nothing ties the program to
the publisher on the user's machine. Later updates are verified by the pinned
minisign key instead.
The release carries a `SHA256SUMS` file next to the ZIP, which is only
protection against a corrupted download: it is published by the same account in
the same release, so whoever can replace the ZIP can replace the checksums.
It does not prove who built the file. Anyone who needs more than that builds the client from source. The bundle key pinned in the
executable is what makes the rest of the client trustworthy after install; the
executable itself is trusted as far as its download was.

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

## Rules acceptance

Community rules (`/api/onboarding`, see [roles-and-permissions.md](roles-and-permissions.md))
and custom emoji (`/api/emoji`, `/api/admin/emoji`) reach the shipped desktop
clients through the trusted proxy like any other `/api` call, so members accept
rules and use emoji in the web app as usual.
