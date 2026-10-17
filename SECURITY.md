# Security Policy

## Supported versions

Mnema Talk is a small, self-hosted, non-commercial project. Only the current
`main` branch is supported; fixes land there. Forks and older commits are not
maintained.

## Reporting a vulnerability

Please report security issues **privately** through GitHub's private
vulnerability reporting:

> <https://github.com/msch128/mnema-talk/security/advisories/new>

(Repository → *Security* → *Report a vulnerability*.)

Helpful details: the affected component or endpoint, steps to reproduce, and
what an attacker could achieve. You can expect an acknowledgement within 7 days.

**Please do not open public issues or pull requests for security problems** and
do not disclose them publicly before a fix is available. Do not run scanners,
brute-force or load tests against instances you do not operate; run the stack
locally instead (`make up` or `make dev`).

## Security model (summary)

- **Invite-only.** There is no public registration. Only the Owner
  (configurable, default `Herzog`) and members whose role holds *Manage
  invites* can create invite codes.
- **Owner and roles.** The Owner (the one `ADMIN_USERNAME` account) holds every
  permission and nobody can edit, restrict, disable, kick, rename or overwrite
  them or change their roles, not even a role with *Administrator*. The
  System tab, the storage dashboard and other accounts' passwords are
  Owner-only. Below the Owner, members get permissions from `@everyone` and
  their roles, refined per category and channel; a member can only manage
  roles and members strictly below their highest role and cannot grant
  permissions they lack. Everything is enforced on the server: REST, media
  downloads (a file is only served to those who can view its channel, with a
  404 otherwise), the WebSocket hub (events of channels a member cannot view are
  never delivered, nor who sits in a Talk they cannot see), joining a Talk and,
  in the SFU, sending audio, camera or screen without *Speak* or *Video*.
  Permission changes take effect live: connections are told to refresh and
  members who may no longer join their Talk are removed from it. A permission
  state that cannot be loaded denies access. Details and the resolution
  order are in [doc/roles-and-permissions.md](doc/roles-and-permissions.md).
- **Sessions.** A signed session token in an `HttpOnly` cookie (`__Host-` prefix
  and `Secure` on HTTPS, `SameSite=Lax`). Each token carries the user's
  `token_version`; changing the password or "log out everywhere"
  (`POST /api/auth/logout-all`) increments it, which revokes every existing
  session of that user immediately, including copied cookies, and closes the
  user's open WebSockets (after a password change the current client
  reconnects with its fresh cookie).
- **CSRF.** State-changing requests and WebSocket upgrades must come from an
  allowed origin (`PUBLIC_URL` / `CORS_ALLOWED_ORIGINS`), on top of `SameSite=Lax`.
- **Security headers.** Strict Content-Security-Policy (no inline scripts or
  JS eval; `'wasm-unsafe-eval'` only so the bundled noise filter can compile
  its WebAssembly),
  `frame-ancestors 'none'`, `nosniff`, a restrictive referrer policy and related
  headers on every response.
- **Uploads.** File types are detected by content sniffing, not by the
  client-supplied name or MIME type. Risky types (HTML, SVG, scripts, unknown
  binaries, ...) are only ever served as downloads, never rendered inline. Media is
  served only to authenticated users, through the app, not directly from S3.
- **Abuse protection.** Per-client rate limiting, plus an escalating login
  lockout per client address and username after repeated failures (1, 5, then
  30 minutes; the tier is forgotten after an hour without failures). A much
  higher per-account cap guards against guessing from many addresses: once it
  trips, wrong passwords for that account are answered with 429 and count
  triple against their address, but the correct password always signs in (and
  resets the cap), so neither one address nor many can lock the owner out.
  Rate-limit and lockout keys treat an IPv6 /64 as one client. Login attempts
  with a username that cannot exist are rejected before they are tracked, and
  limiter memory is capped. Invite codes are checked before any password
  hashing.
- **Client addresses.** Client IPs are taken from `X-Forwarded-For` (every
  header line, right-most untrusted hop) only when the request comes from
  loopback or `TRUSTED_PROXY_CIDRS`. When private ranges are trusted (as with
  the Docker network behind a reverse proxy), the app port (8080) must not be
  reachable directly from the internet or from untrusted hosts in those
  ranges: anyone who can connect from a trusted range can set their own
  `X-Forwarded-For` and evade the per-client limits. Publish only the reverse
  proxy.
- **Voice and screenshare.** The SFU transport is DTLS-SRTP. Microphone, camera,
  screen video and screen-audio payloads are encrypted in the browser with
  keys derived from an MLS group, so an honest SFU, and anyone who only sees
  or stores the forwarded RTP, cannot read them. A browser that cannot seal a
  track does not send it. This is not protection against an active or
  compromised server: the server serves the web app's code, relays the MLS
  handshakes, asserts who is in the room and stamps each sender, and
  credentials are self-asserted user ids. Such a server can add a member,
  impersonate the committer, or swap a present member's leaf for its own
  (the member is told its media keys changed) and obtain the keys. It can also
  force a group restart (rate-limited with backoff) or stop the call by
  dropping messages. A newcomer joins the group of whichever member of the
  Talk lets it in, so such a server, or a malicious member, can also steer a
  newcomer into a group of its own. The connection details show a safety code for the current
  key epoch (derived from the MLS epoch authenticator) and who holds its keys.
  Members who compare it in person or over a channel they trust notice an
  added member, a swapped leaf or a split call; this cannot help against a
  server that also serves a modified web app. Every sealed frame is signed
  with its sender's MLS leaf key (Ed25519) and checked against the publisher
  the line belongs to, so neither the server nor another member can pass off
  media as someone else's; an id that holds two leaves verifies nothing. Each
  sending transform numbers its frames, and a receiver opens a frame only once
  (64-frame window per stream, see below for streams and layers). The previous epoch's key is kept until the
  next rotation.
  **Key lifetime.** A key never outlives 30 minutes (plus or minus a random
  two-minute jitter per epoch, so Talks do not rotate in step): once the
  epoch is that old, the committer (the lowest member id in the group and the
  room) advances it with a commit that carries an update path, so even with
  no joins or leaves every member gets a new exporter key, and the committer's
  own leaf secrets are replaced (post-compromise security for it). The
  schedule is the epoch's age, which every member knows, not a timer that
  restarts when the committer changes, and it is measured with the browser's
  monotonic clock, so a wall-clock step moves nothing. Receivers hold the
  previous epoch's key for 30 seconds after a switch within the same group,
  so media keeps playing while the commit reaches the slowest member; after
  that deadline its key and verifier set are deleted and nothing sealed or
  signed under it opens. A member removed by a commit can therefore inject
  frames under the epoch it was in for at most those 30 seconds. After a
  commit that only adds a member or rotates, a sender goes on sealing under
  the epoch it left for 1.5 seconds so receivers have the commit before the
  first frame of the new epoch arrives; after a commit that removes someone
  it seals under the new epoch at once, so a removed member never reads
  anything sealed after the commit (receivers that have not yet processed the
  commit drop those few frames for about a second). A committer that is
  connected but shows no sign of life for longer than the absence limit plus
  two maintenance intervals (a hung tab) is skipped when choosing the
  committer: the next-lowest member takes over and removes it, and it is
  let in again normally if it returns. A browser that wakes from sleep does
  not commit on the tick that finds it slept, so the messages queued meanwhile
  are handled first. The cost is one small commit
  per 30 minutes per Talk and nothing per frame. A member that showed no sign
  of life for 20 minutes is removed from the group (rekeyed out, so what it
  held no longer opens anything new) and must be let in again with a fresh
  key package when it returns. Liveness is not media (listeners are muted): every
  member signs a 78-byte heartbeat (group, id, epoch, increasing time) with its
  MLS leaf key about every 5 minutes over the existing handshake channel, and
  any other handshake it sends counts too. An older copy of a heartbeat, one
  for another epoch or one signed by another leaf counts for nothing. The
  committer whose browser itself slept starts everyone's clock over instead of
  removing people for silence it could not hear, and a returning member is
  re-admitted by the existing offer path (rate-limited with backoff), so it
  costs at most one removal per 20 minutes. This stays inside the server's
  handshake budget. The server can still withhold heartbeats to get a member
  removed; that is the same denial-of-service it already has by dropping
  messages.
  **Layers of a screen share.** A screen share may go out as up to three
  encodings (layers), each on a sending line of its own and so each sealed by
  its own transform, and the SFU hands a viewer one layer at a time, switching
  at a keyframe. Nothing about the sealing changes. Every frame's AES-GCM
  nonce is 96 fresh random bits from the browser (never derived from the layer,
  stream or counter), so layers add no way to repeat a nonce under the epoch
  key; in the worst case (a 4K share at 60 FPS in three layers, 100 members)
  about 2^25.5 frames are sealed per key in one 30-minute epoch, about 2^28.5
  with eight H.264 slices per frame, below the 2^32 invocations NIST SP
  800-38D allows for random 96-bit nonces under one key. Rotation needs more
  than one member, so a lone sender keeps its key until someone joins. The tests seal thousands of frames across layers and check that
  every nonce is a distinct draw. Each layer's transform starts a stream of its
  own whose ID carries the layer in its low two bits, so two layers of a share
  never share a stream, whatever the random rest of the ID is; the counter,
  the stream and the epoch are authenticated and signed with the frame. The
  receiver's replay window is per (epoch, stream), so when the SFU switches a
  viewer from one layer to another, or back, the counters of the other layers
  (which kept counting while not forwarded) never look like replays of each
  other and nothing is opened twice. A sender may use up to 256 streams per
  epoch (a share takes three, each reconnect re-arms all of its lines); further
  new streams are refused rather than evicting an old one. The SFU reads only
  what the transform leaves readable (the RTP descriptor, the VP8 frame tag,
  H.264 NAL types) to find keyframes and renumber packets; it never decodes or
  alters sealed bytes. Which layers exist, which one a viewer receives and
  which a sender is asked to encode are metadata the server sees, like frame
  sizes. A viewer can only change its own layer, a sender only declares its
  own share, and a sender is asked to encode at most its three layers.
  The server also sees the declared size and frame rate of each layer, each
  viewer's quality choice and the layer each viewer receives. A layer stream
  the receiver has not seen yet starts a fresh replay window, so an active or
  compromised SFU can feed a viewer old recorded frames of a layer it has not
  received yet (previously the same was true for newly joined viewers); this
  falls under the active-server limits above, not under an honest SFU.
  Video stays decodable by the
  browser's packetizer, so the codec headers it parses (H.264 parameter sets
  and slice headers, the VP8 payload header) are readable but authenticated;
  only VP8 and H.264 are negotiated while media is sealed. Metadata (who speaks
  when, frame sizes, timing) is visible to the server. Text messages and
  attachments stay readable by the server. **Desktop client.** The desktop
  client does not run the server's JavaScript. It runs a signed bundle of the
  web app that it verifies (Ed25519, a key pinned in the client; built into
  the installer or downloaded from this repository's GitHub releases; refused
  below a signed security floor), and reaches the instance only through its own
  proxy, which keeps the session cookie out of the page and hands over anything
  that could render as a document as a download. An active or compromised
  server therefore cannot inject or replace code in the desktop client; it can
  still do everything else described above (metadata, membership assertions,
  steering newcomers, denial of service, hostile data). Whoever holds the
  bundle signing key, which is not on any server, can still ship a malicious
  app, and the installer itself is not yet code-signed. Browsers are
  unchanged: the server serves the code. See
  [doc/desktop-trusted-client.md](doc/desktop-trusted-client.md). Sealed media
  in the desktop client is not yet qualified on real Windows hardware.
- **Data retention.** Nothing is deleted automatically by default
  (`MEDIA_RETENTION_DAYS=0`). Operators who enable pruning must say so in their
  privacy notice.
- **Outbound connections.** Besides link previews (`LINK_PREVIEWS_ENABLED`)
  and optional STUN/TURN, the server only asks GitHub for the latest release
  (`UPDATE_CHECK_ENABLED`, default on): an unauthenticated `GET` to
  `api.github.com/repos/msch128/mnema-talk/releases/latest` every 30 minutes
  with `If-None-Match`, a 10 s timeout, a 1 MiB response cap, redirects only
  to the same host, and a pause when GitHub answers 403/429. Only
  `tag_name`, `html_url` (accepted only for this repository's release pages),
  `published_at` and `body` are read; the body is shown to admins as plain
  text, never as HTML. No user data is sent; the privacy policy names the
  connection while it is on. `UPDATE_CHECK_ENABLED=false` makes no request.
- **Secrets.** All secrets come from environment variables. The repository is
  public and contains placeholders only; CI and the pre-commit hook run gitleaks.

## Self-update sidecar

"Update now" in the admin console is optional and **off by default** (compose
profile `autoupdate`, `UPDATER_TOKEN`). How it is built and why:

- **The Docker socket is root on the host.** Whoever can talk to it can start
  a privileged container that mounts `/`. So the app container never gets the
  socket or any other Docker API access. A separate container, the updater
  (`nickfedor/watchtower`, the maintained fork of the archived
  containrrr/watchtower, pinned by version and digest), holds it, and only
  when the operator opts in.
- **What the updater may do.** It runs with `--label-enable` and
  `--scope mnema-talk`: only containers carrying both labels
  (`com.centurylinklabs.watchtower.enable=true`, `...scope=mnema-talk`) are
  touched, which in `docker-compose.yml` is the app alone (not PostgreSQL,
  SeaweedFS, coturn, the updater itself or anything else on the host). It
  re-pulls the image tag the app already runs and recreates the container
  with the same configuration; it cannot be told to run another image.
- **When it acts.** Never on a schedule (`WATCHTOWER_HTTP_API_PERIODIC_POLLS=false`):
  only on `POST /v1/update` with `Authorization: Bearer <UPDATER_TOKEN>`.
- **Who can reach it.** No published port. It is only attached to the
  `internal` network `mnema-updater`, which it shares with the app and
  nothing else, and which has no route to the internet or the LAN (image
  pulls are done by the Docker daemon). The container runs read-only, with all
  capabilities dropped, `no-new-privileges`, and memory and PID limits.
- **Token.** `UPDATER_TOKEN` is shared by app and updater through `.env`
  only, must have at least 32 characters (`openssl rand -hex 32`), is
  rejected if it is a public example value, never appears in API responses
  or logs, and is only ever sent to `UPDATER_URL` (redirects are not
  followed). Without it the app offers no button and makes no call. Rotate it
  by changing `.env` and running
  `docker compose --profile autoupdate up -d`.
- **Who can trigger it.** `POST /api/admin/system/update` is admin-only and
  behind the same-origin CSRF check like every admin route. It asks for the
  admin's current password again (bcrypt; 5 wrong passwords lock it together
  with password changes), runs at most once every 5 minutes, only while the
  update check knows a newer release and only for exactly that version, and
  is audit-logged (`"audit":"self_update"`, admin id, versions, client
  address) whether it succeeds or not. A stolen admin session therefore cannot
  update without the password, and even then can only restart the app on the
  image tag the operator configured.
- **Residual risk.** A compromised updater image, or anyone who obtains both
  the token and a foothold on the `mnema-updater` network, could have
  containers in scope recreated; anyone who compromises the updater container
  has root on the host. Leave the profile off if you update by hand anyway.
