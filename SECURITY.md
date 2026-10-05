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
  session of that user immediately, including copied cookies.
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
  lockout per client address and username after repeated failures, and a much
  higher per-account cap against guessing from many addresses. Guessing from one
  address therefore cannot lock the account out for everyone else. Invite codes
  are checked before any password hashing. Client IPs are taken from
  `X-Forwarded-For` only when the request comes from `TRUSTED_PROXY_CIDRS`.
- **Voice and screenshare.** Media is encrypted with DTLS-SRTP between each client
  and the SFU. This is **not end-to-end encryption**: the SFU decrypts packets in
  memory to forward them to the other participants (it does not record or
  transcode them). Whoever operates the server could in principle access the streams.
- **Data retention.** Nothing is deleted automatically by default
  (`MEDIA_RETENTION_DAYS=0`). Operators who enable pruning must say so in their
  privacy notice.
- **Secrets.** All secrets come from environment variables. The repository is
  public and contains placeholders only; CI and the pre-commit hook run gitleaks.
