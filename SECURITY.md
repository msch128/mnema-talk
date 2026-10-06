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

- **Invite-only.** There is no public registration. Only the administrator
  (configurable, default `Herzog`) can create invite codes.
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
- **Voice and screenshare.** Media is encrypted with DTLS-SRTP between each client
  and the SFU. This is **not end-to-end encryption**: the SFU decrypts packets in
  memory to forward them to the other participants (it does not record or
  transcode them). Whoever operates the server could in principle access the streams.
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
