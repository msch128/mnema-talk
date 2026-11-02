# Security Policy

## Supported versions

Mnema Talk is a small, self-hosted, non-commercial project. Security fixes go
to:

- the **latest 1.x minor release** (for example the newest `1.y.z`), and
- the **previous minor release for 90 days** after the newer minor was
  published, so an operator has a quarter to move up.

Fixes land on `main` first and reach those lines as patch releases. With 1.1
published, 1.1.x is the latest line and 1.0.x gets fixes for 90 days after the
1.1.0 release. `0.9.x` and older releases get no fixes, and neither do forks or
unreleased commits. Update to a supported line
([doc/upgrade.md](doc/upgrade.md)). Desktop clients refuse
web bundles below the release that carries the latest marked security fix, see
[doc/desktop-trusted-client.md](doc/desktop-trusted-client.md).

Which interfaces are stable and how they may change (deprecation, removal only
in a major release) is described in [doc/api-stability.md](doc/api-stability.md).

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

## Audit package

[doc/security/audit-package.md](doc/security/audit-package.md) collects what an
external security review needs in one place: scope and versions, architecture
and data flows, threat model, cryptographic design, a generated inventory of
routes and events with their authentication, the controls, the results of the
internal reviews (1.0, 1.1, 1.2), known limitations, how to run a test instance
and suggested focus areas.

## 1.0 internal security review

Before 1.0.0 the project reviewed its own code in six lanes: authentication and
sessions, permissions, content and media, integrations (webhooks, bots, link
previews), realtime and voice, and the supply chain (CI, signing, updates). It
is an **internal review, done on 2026-10-22, not an external audit**; no
independent audit exists yet ([doc/product-status.md](doc/product-status.md)).
That review covered the code as it was before the later scope additions
(e-mail/SMTP, two-factor sign-in, account deletion and export, Web Push and
the installable app); those were reviewed per change by automated security
review plus a final verification pass, not by the six-lane review.

Result: 0 Critical, 0 High. Every Medium finding is fixed, with a test that
fails without the fix, and the behaviour changes are listed in
[doc/upgrade.md](doc/upgrade.md#09x--100). Deliberately deferred past 1.0:

- **RT-4, frame format version 2.** Closed in 1.2.0: version 2 media frames
  (no line binding) are neither sent nor opened any more (see the media frame
  section below).
- **RT-6, regroup collusion** (Info). Two colluding members, or the server, can
  make a listener who hears nobody else regroup; rate-limited, no worse than the
  denial of service the server already has.

Closed in 1.1:

- **SUP-05, image signature check in the updater.** "Update now" now verifies
  the cosign signature and the build provenance of the new image before
  anything is pulled or restarted, and refuses otherwise; see
  [Self-update sidecar](#self-update-sidecar).
- **Legacy bot tokens without an issuer.** A token created before 1.0 is bound
  to its likeliest issuer the first time it is used after the upgrade, and the
  admin console lists the ones not bound yet; see
  [doc/bots.md](doc/bots.md#tokens). No token was revoked for this.

Remaining Info-level items of the review are not listed individually.

## 1.2 internal security review

The 1.2 additions (Talks and lobbies, stages, automod, sessions, scheduled
messages and reminders, forum and poll changes, the Discord structure import,
account export and deletion extensions) went through the same kind of
**internal review**, not an external audit. Result: 0 Critical, 0 High. Every
Medium and the Low findings that name a concrete misuse are fixed with a test
that fails without the fix. Deliberately accepted:

- **Scheduled messages burst.** Messages scheduled for the same second go out
  in one sweep per member (at most 50 pending per member). Slow mode, the
  direct message gate, blocks, automod and the permission checks still apply at
  send time; only the per-request rate limiter does not, because there is no
  request. The cap bounds the burst.
- **Duplicate ids in a Discord template.** A template with two roles or
  channels of the same Discord id can attach overwrites to the wrong imported
  object. Real templates have unique ids and the fetch is pinned to
  discord.com; the preview and the plan hash only guard against a change
  between preview and apply. Review the imported channels' permissions after
  an import, as the import page says.
- **Moderation timeline and Manage reports.** The per-member moderation
  timeline (reasons, role changes, Talk kicks) is readable with *Manage
  reports* alone, independent of *View audit log*. This is intended: handling
  a report needs the history. Grant the permission accordingly.
- **Stage and reaction rate gates reset on reconnect.** The windows for stage
  commands and reactions start over when a member's last connection closes.
  They are bounded by the connection rate limit, like the voice gate.

## Direct messages (privacy model)

Direct messages (1.1) are conversations of two to ten members. Their privacy
model, in short; the design is in [doc/architecture.md](doc/architecture.md#direct-messages).

- **Who can read.** Members of the conversation, nobody else: the Owner,
  Administrators and moderators have no route to it (REST, WebSocket, search,
  media, push, audit all test membership). A moderator sees only what a member
  chose to report: the reported message and up to 20 around it, copied at the
  time of the report.
- **Not end-to-end encrypted.** Like channel text, a conversation is readable
  by the server and by whoever operates it or holds a backup or database
  dump. Operators can read the `messages` table; this is the same trust as for
  channel text, and there is no plan for text E2EE. Attachments of a
  conversation are served only to its members.
- **Blocks cannot be probed.** Every refusal about another member (a block,
  their `dm_policy`, a disabled account, an unknown id) is one 403 text, and
  `can_send` and every list say nothing about a block.
- **Abuse limits.** 3 tries a minute and 10 new conversations a day, a cap of
  two messages before the other side has answered, at most 10 members per
  conversation and 100 group conversations per member, *Send direct messages* as a permission (default on),
  a community switch (default on) and a per-member `dm_policy`.
- **Side channels removed.** Private messages do not move the public message
  counters or the public message numbering, are absent from the audit log,
  bot and webhook events and the admin media list.
- **Server-side enforcement** of all of it; the UI only hides what a member
  cannot do.

## Anonymous polls

An anonymous poll hides who chose what from every member, moderator and
administrator in the app. Mnema never stores which option a member chose: only
that the member voted (`poll_voters`, no timestamp) and ballots without a
member (`poll_anon_ballots`) that are folded into per-option counters. An
operator with direct access to the database server (WAL, transaction ids or
statement logs) could, in theory, still correlate the timing of a vote. Do not
rely on it against that operator. An anonymous poll pushes no running tally
at each vote (it announces the result when it ends), but the counts of a
running poll are readable, so in a very small audience the choice of the last
voter follows from the result.

## Blocks and reports

A block is private: the blocked member is not told, and nothing about it is
visible to them (see "Blocks cannot be probed" above). The blocker's web app
collapses the blocked member's messages, and their mentions, replies and
messages create no inbox entry, mention count or push for the blocker (the
mentions inbox excludes direct messages entirely; in any conversation, group
direct messages included, unread counts, mention counts and last-activity time
leave out messages by members the reader blocked, and push recipients skip
blockers). This is decided when the entry is listed or the push is sent, on the server. Reports are the other
way: a member reports a message they can see, and the report stores its own
copy of that message and up to 20 around it, so editing or deleting the
original does not change what the moderators read. Only the Owner,
Administrators and holders of *Manage reports* (off by default) can open the
queue, and reading a report needs no access to the channel. A report of a
direct message shows only that copy, never the rest of the conversation.
Deleting an account blanks its text in every report copy and cuts the links
to the account. Details: [doc/moderation.md](doc/moderation.md#blocking-and-reports).

**Automod and moderator notes (1.2).** Automod entries use the same queue. A
blocked channel message is kept in the entry as the text that was refused;
for a direct message automod keeps no text at all, only who broke which rule.
Moderators with *Manage reports* can also write private notes about a member.
A note is moderator data: the member never sees it, it is not in their data
export, the audit log records that it was written but never what it says, and
when the account is erased the notes stay, attached to the tombstone, until a
moderator deletes them. Details: [doc/moderation.md](doc/moderation.md#automod).

## Forwarding (a reference, not a copy)

A forwarded message stores a reference to the original (`forward_of`) and the
forwarder's own comment, never the original's text. The quote is produced when
a reader asks for it (`GET /api/messages/{id}/forward`) from what that reader
may see at that moment, so a hidden channel's text cannot reach someone through
a forward, whoever forwarded it: not in message lists, search, the inbox, push,
the export, WebSocket events, or the payloads of bots and webhooks, none of which holds an id or any text of
the original (only `forward: {forwarded: true}`). Every refusal of a
forward is one 403 text, so a forward cannot probe channels, conversations or
blocks. The ids of the original are returned only to readers who may see it. Saved messages are a
private list of references with the same read-time check, conversations by
membership.

## Channel slugs (speaking addresses)

Channel addresses (`/c/general`) are slugs. What they could reveal, and what limits it:

- **Visibility.** `slug` and `previous_slugs` are part of the channel object, so only members who may see a
  channel receive them. There is no endpoint that resolves a slug; the web app resolves against the channels it
  already holds, and an unknown address and a channel the member may not see give the same answer.
- **Collisions.** A slug is unique on the server, former slugs included. When a name collides, the channel's own
  slug gets a suffix from the first 6 hex characters of its own id (more only if that collides too) (`plans-0f3a9c`), never a running number, so the
  suffix does not count other channels. A channel manager (the *Manage channels* permission) can still infer that
  some channel already uses a slug when theirs gets a suffix; this is accepted, as managers administer the
  channel structure.
- **Temporary Talks.** The leader of a temporary Talk may rename it without *Manage channels*. Its slug is fixed
  at creation (`talk-` and the first 8 hex characters of its id), is never changed by a rename and never enters
  the slug history, so a leader learns nothing about other channels. No name-based slug can take that shape.
- **Hoarding.** A channel keeps its newest ten former slugs; older ones become free again and their links no longer
  resolve. Only renames by members with *Manage channels* reach the history.

## Security model (summary)

In short: members join by invitation only. Microphone, camera, screen video
and screen audio are end-to-end encrypted in the browser (MLS, keys rotate every
30 minutes, signed frames, a safety code to compare), so an honest server
cannot read them; an active or compromised server is out of scope because it
serves the web app's code. Text messages and attachments are readable by the
server. The desktop client removes server-served JavaScript by running a signed
web bundle. Release images are signed with cosign. The sections below give the
details and the limits.

- **Invite-only.** There is no public registration. Only the Owner
  (configurable, default `admin`) and members whose role holds *Manage
  invites* can create invite codes.
- **Community icon (public, like the name).** `GET /api/community/icon` needs
  no session, also on an invite-only server: the community name next to it is
  public already (`GET /api/health`, the sign-in page, link previews) and an
  icon is meant to be seen. Only the Owner and Administrators set it. The upload
  is decoded and redrawn as two square PNGs, so metadata and anything hidden in
  the container are gone and SVG never gets in; the route answers a fixed set of
  sizes from stored files, has its own per-address rate limit and carries no
  member data.
- **Vanity link (`/join/<name>`).** One optional short address the Owner or an
  Administrator sets. Alone it only leads to the sign-in page and never
  bypasses invite-only registration. If the Owner switches on *Let this link
  create accounts*, it is a deliberate permanent public invite (unlimited, no
  expiry; the guessable name is public by design, so the strong-code floor does
  not apply to it) that goes through the same registration, limits and raid
  protection as any invite; off or removed, it stops working at once. Its
  preview (`GET /api/join/<name>`) is public and shares the invite preview's
  rate limits and says only whether the link exists, the community name and
  whether it takes sign-ups; the name is never reflected into a page unescaped
  and an unknown name gets the generic page.
- **Invite preview (the one public read).** `GET /api/invites/{code}` needs no
  session so an invite link can show the community's name before sign-up. It
  answers `{"valid": true, "community_name", "expires_at"?}` or exactly
  `{"valid": false}`, and nothing else: no creator, no use counts, no id.
  Unknown, malformed, expired, revoked and used-up codes give byte-identical
  answers, so the endpoint cannot tell an attacker that a code once existed. It
  is rate limited per client address (30 per minute, IPv6 per /64, 429 with
  `Retry-After`), for hits and misses alike, so the answer never depends on
  whether a code exists and nothing is shared between clients (no global brake
  that one attacker could burn to lock invitees out). Distributed guessing is
  made infeasible by the codes, not by a limiter: a generated code has 12
  characters from a 31-character alphabet (about 59 bits), and a custom code must
  be at least 12 characters with 7 or more distinct ones, mixing letters and
  digits unless it has 16 or more characters, and not a common word with little
  around it. Even 2^16 addresses at 30 lookups a minute try about 2^21 codes a
  minute, which covers about 2^40 in a year, far below the 2^59 of a random code.
  Older weak custom codes no longer work (preview and registration treat them
  as unknown); the admin invite list marks them (`weak`: disabled, regenerate).
  Limits are per client address and, for IPv6, additionally per /48. The endpoint answers
  `Cache-Control: no-store`, and a code is only looked up when it has the shape
  the server issues. The community name is the only thing it discloses and the
  Owner chooses it.
- **Community rules.** When the Owner or an Administrator writes rules, members
  who have not accepted the current version (`users.rules_accepted_version`
  below `instance_settings.rules_version`) lose what a timeout takes: sending
  messages, reactions, uploads, joining a Talk and speaking or sharing in one.
  This is part of the permission snapshot, so it holds in REST (403
  `RULES_NOT_ACCEPTED`), in the WebSocket hub (`voice_kicked` with reason
  `rules_not_accepted`, also when the rules change while someone is in a Talk)
  and in the SFU, and fails closed: a member the snapshot has not seen yet
  counts as not having accepted. Reading stays possible. The Owner and bots are
  exempt. Empty rules text enforces nothing, so an upgrade or clearing the
  rules can never lock anyone out. Only the Owner and Administrators change the
  texts, every change is audited, and the rules are shown as sanitized
  Markdown.
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
- **Plain-HTTP deployments.** With a production or staging `APP_ENV` and a
  `PUBLIC_URL` that is not `https://`, session cookies lose `Secure` and the
  `__Host-` prefix. The server starts anyway, logs a warning and shows it under
  Admin > System (see `doc/operations.md`).
- **Sessions.** A signed session token in an `HttpOnly` cookie (`__Host-` prefix
  and `Secure` on HTTPS, `SameSite=Lax`). Each token carries the user's
  `token_version`; changing the password or "log out everywhere"
  (`POST /api/auth/logout-all`) increments it, which revokes every existing
  session of that user immediately, including copied cookies, and closes the
  user's open WebSockets (after a password change the current client
  reconnects with its fresh cookie). Each token also has its own id; signing
  out records it in a small table until the token would have expired, so a
  copied cookie of that one session stops working at once, and signing out
  also closes the WebSockets opened with that session. Tokens issued
  before this existed carry no id and stay valid until they expire. An open
  WebSocket is re-checked every two minutes against the token version, the
  session id and the token's own expiry, and ends at the expiry; if the
  database cannot answer three checks in a row, the socket is closed rather
  than kept on trust.
- **E-mail address, confirmation and password reset.** An e-mail address is
  optional and belongs to its account alone: no API response about other
  members, the member list or the admin user list carries it, and the audit
  log never stores it. Setting or removing it needs the current password (the
  same lockout as changing the password). A new address starts unconfirmed;
  two accounts may hold the same unconfirmed address, so adding one reveals
  nothing, and only a confirmed address is unique. Mail needs `SMTP_HOST`
  (credentials from the environment or a file, never in the database; TLS is
  required and certificates are verified, `SMTP_TLS=none` is refused in
  production; recipients and subjects are checked against header injection).
  Without it no mail is sent and a saved address is never used. Confirmation
  and reset links carry a 256-bit random token in the URL fragment (never in a
  request line, access log or Referer); only its SHA-256 is stored; a token is
  single use (one atomic UPDATE), expires (24 hours for confirmation, 30
  minutes for reset), is bound to its purpose and, for confirmation, to the
  address it was sent to (a reset token also to the account's session version, so a password change, an Owner password action, "log out everywhere" or a session revocation kills every open reset link), and a newer link retires the older one. Reset: the
  request answers `202` with an empty body for every well-formed address, known
  or not, confirmed or not, disabled account or not, and does its work after
  responding, so neither content nor timing tells accounts apart; it is limited
  per client address (10 per hour, IPv6 per /64, 40 per /48) and, silently, per
  address (5 per hour) and per account (5 mails per hour, counted in the
  database so it survives restarts and attacker-chosen keys). Only an active
  account with a confirmed address gets a link. Confirming a reset spends the
  token and sets the password in one transaction, applies the usual password
  policy (the Owner needs 14 characters), revokes every session (token version)
  and known-device cookie, closes open WebSockets and clears the account's
  login lockouts; it signs nobody in. Mail delivery failures never reach the
  client. The Owner's "send test mail" is Owner-only, limited to five per ten
  minutes and audited without recipient.
- **Two-factor sign-in (TOTP).** Optional for every account; the Owner's
  settings recommend it prominently. RFC 6238 (HMAC-SHA1, 6 digits, 30 seconds,
  one step of clock skew either way), enrolled with a QR code drawn in the
  browser (no image service sees the secret) and confirmed with a first code;
  turning it on returns ten single-use recovery codes, once. The secret is
  stored encrypted (AES-256-GCM, key derived from `JWT_SECRET`, bound to the
  account); recovery codes are stored as keyed HMACs (59-bit codes, so a
  database copy alone cannot be searched for them). Rotating `JWT_SECRET`
  therefore makes stored secrets unreadable: those accounts need a reset
  (below). Sign-in: a correct password for such an account answers `202` with a
  challenge instead of a session (nothing about the account is revealed before
  the password is right). The challenge is 256 random bits (only its hash is
  stored), lives five minutes, is bound to the account and to its token version
  at that moment (a password change, "log out everywhere" or a disable in
  between kills it), is spent by a successful sign-in and takes five code
  attempts in total, counted atomically. An authenticator code works once (the
  newest accepted step is stored, so a replay of the same or an older code is
  refused); a recovery code works once. Wrong, expired, replayed, used and
  unknown all answer the same `401`. The known-device cookie and the session are
  only issued after the second step. Brute force: attempts of one account are
  serialised, so parallel requests cannot all pass the lockout check; ten wrong
  codes lock the account's second step (escalating, at most 15 minutes); and,
  persisted in the database so a restart does not reset it, 20 wrong
  authenticator codes in any 24 hours refuse authenticator codes for the
  account until the oldest ages out (429), while recovery codes keep working
  (they have their own, much higher budget of 100 wrong guesses a day). Someone
  who knows the password can therefore guess at most about 20 six-digit codes a
  day (about 0.01 % chance), and cannot lock the owner out: a recovery code or
  an Owner reset still gets in. Five or more wrong codes not yet reported make
  the next successful sign-in show a one-time security notice, raise one audit
  entry (`auth.two_factor_alert`, no code) and send one e-mail to a confirmed
  address when the server can send mail. Turning it off or making new recovery
  codes needs the password and a current code; setting up needs the password.
  The Owner can reset a member's second factor (`user.two_factor_reset`, ends
  their sessions). That reset, setting a member's password and generating a
  temporary password for a member take over another account, so each asks the
  Owner to confirm their own password and, while their second factor is on, a
  current code (the same check and lockouts as deleting an account); a request
  without it is refused with 403 and not audited as done. The Owner's own is reset from the host with
  `mnema-talk reset-2fa <username>`. Resetting a password by mail does not skip
  the second factor, and a native sign-in has the same second step
  (`POST /auth/login/2fa`) before any grant is issued.
- **Deleting an account and exporting its data (GDPR Art. 17 and 20).** Both
  are self-service under Account & security and both need the password and,
  while two-factor sign-in is on, a current code (wrong answers count toward the
  usual lockouts), so a stolen session cookie alone cannot do either.
  *Deletion* runs in one transaction and cannot be undone. The Owner account
  cannot be deleted (ownership is not transferable). What goes: profile,
  avatar, banner, e-mail address, sessions (native ones too), tokens, invites,
  reactions, mentions of the member, settings, roles and overwrites, blocks (as
  blocker and as blocked), the saved list, thread follows, direct message
  memberships (a conversation nobody else is in goes with its messages and
  files), every file
  the member uploaded (database rows and objects), pending and finished exports,
  and the second factor. Reports the member sent or received stay for the
  moderators with the member's text, note and links blanked. What stays: the account row as an anonymised tombstone
  (`deleted-<id>`, "Deleted member", disabled, unusable password hash) and the
  member's messages, emptied of text, link cards and pins (and of the earlier
  versions kept for moderators' edit history), so threads and other
  members' replies stay readable; other members' texts that merely quote or
  @mention the old name are not rewritten. Audit entries about the account keep
  action, time and ids; their names and details are blanked by the one update
  the append-only audit trigger permits (and only inside a transaction that sets
  `mnema.audit_scrub`, never changing ids, actions or times). A test walks every
  foreign key to `users` and fails when a table that refers to a person is
  forgotten. The name `deleted-…` is reserved. A deleted account is final:
  every administrative action treats it as missing (404), the account queries
  count it as inactive, and a database trigger refuses any update that would
  re-enable it or give it a password, name, address, avatar, role or ban. *Export* is built in the
  background into the object store under `exports/` (not touched by the orphan
  cleanup), limited to one per account per 24 hours by a database lock, is
  streamed into storage while it is made (memory stays at one page of rows; the
  bounds are enforced while writing), can be downloaded once within 24 hours
  (the file is spent and deleted only after it was sent completely; an
  interrupted download can be repeated, and only one download holds it at a
  time), is swept when expired, and is removed with the account. Because the
  download changes state, it refuses requests the browser marks as coming from
  another site (`Sec-Fetch-Site`), so a link or image on another page cannot
  burn it.
  It holds the member's own profile, settings, roles, messages (channel names
  only for channels they can still view), reactions, upload list, blocks,
  reports they sent, saved message ids and notes, thread follows, a security
  summary and audit entries about the account as "you"/"staff" without any
  other person's name; it is bounded to 200 000 messages and 64 MiB.
- **WebSocket flood limits.** Every client event has a payload cap (1 KiB for
  ping, typing, speaking and the like; larger only for an SDP answer, an ICE
  candidate, a diagnostic and a media handshake), and a frame too big for its
  type is dropped before it is decoded. On top of the per-connection limits,
  an account has shared limits across all of its connections (frames, events
  and media handshakes per second), so several sockets of one member do not
  multiply the allowance. The relay of media handshakes checks only their
  framing (an MLS key package, welcome or public message, or a heartbeat or
  capability message of the exact size); the content stays opaque to the
  server, and anything that is not shaped like one of these is not fanned
  out to the room. ICE candidates in a client's answer SDP get the same
  vetting and budget as trickled ones (no loopback, link-local, multicast,
  unspecified or name addresses, at most 64 per peer).
- **CSRF.** State-changing requests and WebSocket upgrades must come from an
  allowed origin (`PUBLIC_URL` / `CORS_ALLOWED_ORIGINS`), on top of `SameSite=Lax`.
  The one exception is the webhook execute route
  (`POST /api/webhooks/{id}/{token}`): it never reads the session cookie and is
  authenticated by its secret alone, so there is nothing to forge.
- **Webhooks** are an admin tool, decoupled from roles and permissions: only
  the Owner and Administrators create or manage them (incoming and outgoing),
  in any channel; no other permission opens them. Whoever holds a webhook's
  token can post with that webhook's settings, nothing more: each webhook has
  its own switches (mass mentions off by default, member mentions, attachments,
  embeds and username override on by default), and an outgoing URL receives its
  channel's events until an administrator changes or deletes it, whoever set it
  (who did is recorded and shown). The execute URL is a bearer secret: 256
  random bits, stored as a SHA-256 hash, compared in constant time and shown
  once; regenerating changes only the token. Wrong ids and tokens are rate
  limited per client address, valid ones per webhook and per channel. A webhook
  posts as a hidden bot account that cannot sign in and is no member; files go
  through the same size and type checks as uploads; embeds are validated, shown
  as sanitized text and links and never fetched from their hosts; nobody who
  cannot see the channel is notified. The token is redacted from access and
  error logs (`/api/webhooks/{id}/[redacted]`). Details and the Discord
  compatibility table: [doc/webhooks.md](doc/webhooks.md).
- **Bots** (see [doc/bots.md](doc/bots.md)) are accounts that cannot sign in
  and authenticate with an API token. Only the Owner and members with *Manage
  bots* create and manage them; *Manage bots* can only be granted by the Owner,
  and a role with Administrator or admin-area permissions can only be given to
  a bot by the Owner. A bot's permissions come from its roles and channel
  overwrites through the same engine as members', but it does not inherit
  `@everyone`: a new bot can reach nothing, and a private channel stays hidden
  from it. A token is 256 random bits, shown once, stored as a SHA-256 hash,
  optionally expiring, revocable, with a last-used time; revoking a token,
  disabling or deleting a bot ends it at once, open connections included.
  Tokens travel in the `Authorization` header only (never in a URL), are
  never logged and never accepted on the cookie routes, which stay behind the
  session and the CSRF origin check (the bot routes read no cookie, so there
  is nothing to forge). Managing a bot needs at least the bot's own rights,
  and every permission change is checked by outcome in one place: only the
  Owner can make a bot elevated (Administrator or an admin-area permission,
  anywhere), and nobody can give a bot more than they hold themselves,
  server-wide or per channel. The gateway checks what a bot may see for every
  event at the moment it is sent, not at connect time; a resume cannot revive
  a session whose token was revoked. Slash commands are run only by members
  who may send in the channel, only for bots that can view and send there,
  and an admin can switch any command off; ephemeral replies are never stored.
  Built-in commands (`/poll`, `/me`, `/shrug`, `/tableflip`, `/unflip`,
  `/spoiler`, `/nick`, `/thread`, `/msg`, `/timeout`, `/ban` and `/kick`) run on the server behind the
  same checks: the right to send in the channel, a rate limit and options
  validated against the command's definition. The moderation commands check the
  global permission first (*Timeout members*, *Ban members*, *Kick members*;
  the last is off for @everyone and every existing role), then call the same
  service as the admin routes, so the role hierarchy and the Owner's
  protection hold, and write the same audit entry. **Polls** keep anonymous votes anonymous by
  construction: the query that builds a poll never selects voter ids for an
  anonymous poll, so the message, lists, search, the mentions inbox, the
  WebSocket, the gateway, outgoing webhooks and the data export of other
  members cannot carry them; a member's own choice is returned only to that
  member. A vote needs *View channels* and *Add reactions* and is refused
  after the poll ended; concurrent votes by one member cannot leave a
  single-choice poll with two answers, and a vote cannot land after the end.
  Deleting an account removes its votes and the polls it posted.
  Threat model and limits: a bot is as trusted as the roles it was given; a
  leaked token is a leaked bot (revoke it), and the Owner's explicit choice to
  give a bot administrator power is warned about in the interface.
- **Outgoing webhooks and SSRF.** A webhook URL chosen by a member makes the
  server send requests, so `internal/ssrf` guards them: http(s) only, names
  resolved by the server, every answer must be a public unicast address
  (loopback, RFC 1918, link-local, CGNAT, multicast, unspecified, ULA,
  reserved, tunnel ranges and the cloud metadata addresses are refused, also
  as IPv4-mapped IPv6), the connection goes to the vetted address by number
  (DNS rebinding cannot swap it), redirects are not followed, no proxy
  variables are honoured, responses are read to a cap and never stored, and
  delivery logs carry short codes instead of network errors. Operators can
  open specific local addresses with `WEBHOOK_ALLOW_PRIVATE_TARGETS` (off by
  default; metadata and reserved ranges stay closed). Deliveries are signed
  (`X-Mnema-Signature`, HMAC-SHA256 over `t.body`), belong to one channel, send only
  its events to a URL that only administrators can set, and are rate and
  concurrency limited. Pending deliveries are
  stored in PostgreSQL (payloads are the events the server already holds; the
  signing secret is not copied into the queue), bounded per webhook and in
  total, and expire after 2 hours; a retry is signed with the time of the
  attempt actually made, so receivers' replay windows stay valid.
- **Security headers.** Strict Content-Security-Policy (no inline scripts or
  JS eval; `'wasm-unsafe-eval'` only so the bundled noise filter can compile
  its WebAssembly),
  `frame-ancestors 'none'`, `nosniff`, a restrictive referrer policy and related
  headers on every response. Styles are as strict as scripts: `style-src 'self'`
  and `style-src-attr 'none'`, so there is no `'unsafe-inline'` and no
  `'unsafe-hashes'`; injected markup cannot add a `<style>` element or a
  `style=""` attribute. The desktop shells apply this strict policy only for
  web bundles >= 1.1.0; older bundles keep the old policy. The app's own code needs neither (Vue style bindings go
  through the CSSOM, which the policy does not restrict, and component styles
  are files). The one library that wrote inline styles, `emoji-picker-element`,
  is pinned to an exact version and patched while the web app is built
  (`web/scripts/emojiPickerCsp.ts`): its stylesheet becomes a constructable
  stylesheet (`adoptedStyleSheets`) and its `style` bindings are applied through
  the CSSOM. Each of the two patches must match the package source exactly
  once, or the build fails. The Playwright fixtures fail every end-to-end spec
  on any `securitypolicyviolation` event or CSP console message, and
  `e2e/tests/csp.spec.ts` visits the emoji picker, Markdown and code blocks,
  every admin panel and the API reference under the real policy.
- **Message rendering.** Markdown is parsed into a tree of plain nodes
  (`web/src/lib/markdown/`) and only one module turns that tree into HTML: every
  string is escaped, tags come from a fixed table, and generated elements (mention
  pills, future custom emoji) pass an attribute allowlist with a pattern per
  attribute (an image source must be a same-origin path). Links are limited to
  `http`, `https` and `mailto`; a link whose text does not name its destination
  asks before it opens, however it is activated. Code is highlighted by
  highlight.js in its core build with a fixed language list and no
  auto-detection; its HTML output is re-tokenised and only
  `<span class="hljs-…">` pairs survive (anything else, or unbalanced markup,
  falls back to the escaped code), and large blocks are not highlighted.
  Parsing is a hand-written scanner with bounded nesting and a size limit, with
  tests for hostile input; there is no regular-expression backtracking on user
  text. The only `v-html` sink is `MarkdownContent`.
- **Installable app and service worker.** The web app ships a manifest and a
  small service worker (`/sw.js`, same origin, so it fits the CSP unchanged:
  `script-src 'self'`, `worker-src 'self'`). It keeps the app shell (the page
  and its hashed assets, icons) for offline starts and answers nothing else:
  it never touches `/api/`, other origins or non-GET requests, so no message,
  profile, media or session data is ever stored by it. Both files are served
  with `Cache-Control: no-cache`, so a deploy reaches installed apps on the next
  start. The desktop client never registers it.
- **Web Push.** Pushes are encrypted for the receiving browser (RFC 8291,
  `aes128gcm`) and signed with the server's VAPID key (RFC 8292), so the push
  service (Google, Mozilla, Apple, Microsoft) cannot read them; they carry the
  channel name and sender and, unless the member or `PUSH_PREVIEWS=off` says
  otherwise, the first 140 characters of the message. Recipients are decided on
  the server when a message is sent: the author never, never a member who
  blocked the author, a thread reply only to the thread's followers (unless the
  member is addressed), only members who may
  view the channel in the current permission snapshot (a failed snapshot sends
  nothing), with the member's effective notification level, mutes and Do Not
  Disturb applied. The VAPID private key lives in the database (`push_vapid`),
  is never logged or returned, and is created on first start. A subscription
  endpoint is member-supplied, so it is treated like an outgoing webhook URL
  but stricter: https on port 443 only, no literal private address at
  registration, and at every send the same resolve-vet-connect guard
  (`internal/ssrf`) with no allowlist, no redirects and no response shown, so
  the server cannot be used as a request proxy or to scan the network. Failed
  or gone subscriptions are removed; a browser signing out removes its own, and
  registering a browser under another account moves it, so the next person on a
  shared browser is not sent the previous account's messages. A device is
  bound to the session that registered it: once that session is signed out,
  expired or revoked (password change or reset, "log out everywhere", admin
  revoke, disabled account: the user's `token_version` moves on), the device is
  no longer sent anything, checked on every send, and removed within a minute.
  At most 10 devices per account; registering and testing are rate limited.
- **Custom emoji.** An uploaded emoji is never stored as sent: the server sniffs
  PNG, GIF or WebP by content, refuses SVG, JPEG and animated WebP, bounds the
  canvas (1024 px) and the pixel budget of an animation (canvas times frames)
  before decoding, redraws every frame at most 128 x 128 and writes a new
  PNG or GIF, so metadata, trailing bytes and polyglot payloads cannot survive
  (256 KiB limit before and after). The images are served to signed-in members
  from the app's own origin with their stored type, `nosniff` and a sandbox CSP.
  Only the Owner and holders of *Manage emoji* can change them; every change is
  in the audit log.
- **Profile pictures.** Avatars and banners are never stored as uploaded: the
  server decodes them, crops (square, or 3:1 for a banner), shrinks (512x512,
  or 1500x500) and writes a new file, so EXIF (GPS position, camera model),
  XMP, trailing bytes and polyglot payloads cannot survive. Animations stay
  within a pixel and frame budget before any frame is decoded. Avatars stored
  before 1.1 were redrawn by a one-time background job after the update
  ([upgrade notes](doc/upgrade.md#10x--110)). Chat attachments are the
  exception: they are stored byte for byte, so a photo sent as a file keeps its
  metadata.
- **Uploads.** File types are detected by content sniffing, not by the
  client-supplied name or MIME type. Risky types (HTML, SVG, scripts, unknown
  binaries, ...) are only ever served as downloads, never rendered inline. Media is
  served only to authenticated users, through the app, not directly from S3.
  File names lose path parts, control characters and invisible or
  bidirectional format characters (so `invoice<RLO>fdp.exe` cannot pose as a
  PDF); webhook, bot and username-override names get the same treatment.
  Storage is bounded: each account (member, bot, webhook) may upload
  `UPLOAD_QUOTA_MB_PER_DAY` (default 2048) in any rolling 24 hours, deleted
  files included, and uploads pause when `MEDIA_MAX_TOTAL_MB` (default off) is
  reached or the volume at `MEDIA_DISK_PATH` has less than `MEDIA_MIN_FREE_MB`
  (default 1024) free.
- **Abuse protection.** Per-client rate limiting, plus an escalating login
  lockout per client address and username after repeated failures (1, 5, then
  30 minutes; the tier is forgotten after an hour without failures). A much
  higher per-account cap guards against guessing from many addresses: once it
  trips, devices that signed in to that account before (a signed HttpOnly
  cookie set at login, valid a year, an HMAC under a key derived from the
  session secret, bound to the user id and the account's token_version, so it survives restarts
  and address changes but not a password change or reset, "log out
  everywhere", an administrator revoking sessions or disabling the account) are checked as
  usual, while every other device gets about one password check per minute per
  client address and about ten per minute for the whole account, and is answered
  429 otherwise; refused attempts are not charged to the shared ceiling, so one
  hostile address cannot use up the budget a legitimate new device needs and a
  distributed guesser learns next to nothing. (An attacker with as many distinct
  client addresses as the ceiling allows can still crowd out new-device sign-ins
  for the 15 minutes of the cap; the owner's known devices and `mnema-talk
  unlock` are unaffected.) Wrong
  guesses count triple against their address. The cap lasts at most 15
  minutes, and a restart clears all lockouts (they are held in memory). Password
  hashing for login, registration, re-authentication and password changes
  shares one concurrency limit; unauthenticated work (login, registration)
  waits at most two seconds for a slot, with a bounded queue and cancellation
  when the client disconnects, and otherwise answers 503 with `Retry-After`, so
  a flood of attempts neither starves the server nor piles up goroutines. Passwords need at least 10 characters, 14 for the Owner account.
  Rate-limit and lockout keys treat an IPv6 /64 as one client. Login attempts
  with a username that cannot exist are rejected before they are tracked, and
  limiter memory is capped. Invite codes are checked before any password
  hashing.
- **Client addresses.** Client IPs are taken from `X-Forwarded-For` (every
  header line, right-most untrusted hop) only when the request comes from
  loopback or `TRUSTED_PROXY_CIDRS`. The compose file trusts only loopback
  and publishes the app port on `127.0.0.1` by default. List your reverse
  proxy's address, not a whole private range: anyone who can connect from a
  trusted range can set their own `X-Forwarded-For` and evade the per-client
  limits. Production and staging refuse to start when a range is wider than
  an IPv4 `/24` or an IPv6 `/64` and the listener is not loopback; other
  environments log a warning. `APP_ENV=development` only
  listens on loopback, since it logs the generated admin password.
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
  **What a member can do.** A member of the call is inside the trust
  boundary for the media it can read, but the group's rules limit what it can
  do to the others. Every receiver checks a commit after its signature has
  verified and before it takes effect, and ignores one that breaks the rules
  (without discarding anything, as for an untrusted rival): it must come from
  the member who commits now (the lowest id in the group and the room, with the
  hung-tab skip); it may only add or remove; an add must be of an identity that
  is in the room and sent that very key package through the relay, and never
  of an identity that already holds a leaf, unless the same commit removes the
  old leaf (a member who came back); a remove must be of a member the roster no
  longer lists, one silent for about the absence limit by the receiver's own
  clock, or one the commit replaces. A browser that slept, and the member being
  removed itself, take a removal on the committer's word, since neither can
  judge silence. So another member cannot remove a present member or give
  someone a second leaf (an epoch where an identity holds two leaves is
  refused and the member starts over). A malicious committer (the lowest id)
  keeps the powers the rules give the role: it can remove a member that looks
  silent to it, delay joins or stop the call, as above. Only authenticated input discards a
  group: a handshake that merely parses, a key package, or a public message from
  an id outside the group never does. A welcome from a lower id outside my
  group replaces it only if it opens with the key package I offered and passes
  the committer rule, at most once per minute per sender (doubling to ten
  minutes); a member who holds a key package I offered can therefore still pull
  me into a group of its own, as noted above. A commit for an epoch I have not
  reached makes me start over only from one of the two lowest live members and
  naming that member's own leaf. A member counts as alive only for a verified
  heartbeat or a verified commit, never for a message that merely parses.
  **Key lifetime.** A key never outlives 30 minutes (plus or minus a random
  two-minute jitter per epoch, so Talks do not rotate in step): once the
  epoch is that old, the committer (the lowest member id in the group and the
  room) advances it with a commit that carries an update path, so even with
  no joins or leaves every member gets a new exporter key, and the committer's
  own leaf secrets are replaced (post-compromise security for it).
  **Leaf renewal.** Every other member renews its own leaf too, so that
  post-compromise security covers each member and not only the committer: once
  a member's leaf is about 30 minutes old (the same jittered period) it sends a
  commit that holds nothing but its own new update path. To keep it from racing
  the committer it waits for a quiet moment (no handshake or roster change for
  one maintenance interval, the epoch that old, and one more interval per
  member with a lower id whose leaf is due too); a leaf long overdue stops
  waiting. Receivers take such a commit from any member in the group but at
  most twice per rotation period per member, and only if the result differs
  from the old tree in nothing but the sender's own leaf encryption key (same
  identity, same signature key, no other leaf touched); anything else is
  ignored without discarding the group. The same check applies to every
  commit, the committer's included: no commit may change the identity or
  signature key of a leaf it does not remove, or the encryption key of a leaf
  other than the sender's. If two
  commits for the same epoch still meet (a renewal with the committer's commit
  or with another renewal), the committer's wins and, between renewals, the
  lower id's; a member that took the loser goes back to the state it kept for
  ten seconds and takes the winner, so the call does not split (anything else
  that has split is repaired by the existing start-over). A member's leaf
  secrets that an attacker held are therefore useless to it after about one
  period of the member being online and in the group, and a member's keys
  change at most once per period more than before: with 100 members that is
  about 100 small commits per 30 minutes, inside the handshake budget. The
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
  other and nothing is opened twice. A sender may have up to 256 live streams per
  epoch (a share takes three, each reconnect re-arms all of its lines). When
  they are used up, streams silent for more than a minute are retired to their
  highest counter (up to 1024 retired per epoch): a retired stream ID reopens
  only for a counter above that, and then treats everything below as seen, so
  no unit it opened can open again. If none is idle or the retired list is
  full, new streams are refused until the next epoch; nothing is ever evicted
  without keeping its high-water counter. The SFU reads only
  what the transform leaves readable (the RTP descriptor, the VP8 frame tag,
  H.264 NAL types) to find keyframes and renumber packets; it never decodes or
  alters sealed bytes. Which layers exist, which one a viewer receives and
  which a sender is asked to encode are metadata the server sees, like frame
  sizes. A viewer can only change its own layer, a sender only declares its
  own share, and a sender is asked to encode at most its three layers.
  The server also sees the declared size and frame rate of each layer, each
  viewer's quality choice and the layer each viewer receives. For viewers on
  Auto it also keeps a bandwidth estimate of each connection (Google
  Congestion Control over the browser's transport-wide feedback, nothing but
  packet sizes and arrival times) and the bitrate of each layer; neither
  leaves the server. A layer stream
  the receiver has not seen yet starts a fresh replay window, so an active or
  compromised SFU can feed a viewer old recorded frames of a layer it has not
  received yet (previously the same was true for newly joined viewers); this
  falls under the active-server limits above, not under an honest SFU.
  **The line is part of the frame (version 3).** A sealed unit names the line
  it was made for (microphone, camera, screen, screen audio, and a screen's
  layer) in one byte that is part of both the AES-GCM AAD and the signed
  bytes, under its own signature domain. A receiver knows what a line carries
  (the SFU names the stream; each receiving transform is armed with it) and
  opens a unit only if it names that source; a screen unit must also carry
  the layer its stream was made for. An active SFU can therefore no longer
  move a unit from a sender's microphone line to its screen audio, from
  its camera to its screen, or relabel one layer as another: the unit stays
  sealed and signed but does not open. Only the sender's own MLS leaf key
  can sign for a line. The replay windows, stream retirement, nonces and key
  lifecycle are unchanged.
  **Version 2 is gone (1.2.0).** Frames before version 3 carried no line, so an
  active SFU could move a sender's units between its lines. Since 1.2.0 a
  sender seals version 3 always and a receiver opens nothing else: a unit with
  the old `M` `2` magic is dropped without opening and counted per publisher, and
  the web app tells the member once per call that someone uses an outdated
  client ("Someone in this Talk uses an outdated Mnema client ..."). There is no
  negotiation left to attack, so a downgrade or a replay of old frames opens
  nothing, and a server that withholds capability messages can no longer keep an
  epoch on a weaker format. The version-3 signature domain and wire layout are
  unchanged, so 1.0.x and 1.1.x tabs (which open version 3 and send it once every
  member has advertised it) still hear a 1.2 member. What breaks is the other
  direction: a tab that predates 0.8.2 cannot open version 3 at all, and a 1.0.x
  or 1.1.x tab seals version 2 at the start of every epoch until it has heard a
  capability message from each member, so its media is dropped by 1.2 members
  for that moment (usually a second or two; longer if the server delays the
  message). Members therefore keep sending the capability message (`M` `C`, bits,
  Ed25519 signature over group, id and bits, with the member's leaf key) when they
  join or meet a member they have not told, and with each heartbeat; older clients
  decide their format by it, and a 1.2 member answers it like any other. The security floor of the signed web
  bundle is raised to 1.2.0 with this change (`Security-Floor: raise`), so a
  desktop client that has run a 1.2 bundle never falls back to an older bundle
  that still sealed version 2. The server cannot enforce this: it relays media it
  cannot read, and the format is checked by the receiving clients.
  Also known and accepted: two colluding members, or the server, can make a
  listener who hears nobody else regroup by naming a newer epoch in unopened
  frames; this is rate-limited with a backoff and no more than the denial of
  service the server already has.
  Video stays decodable by the
  browser's packetizer, so the codec headers it parses (H.264 parameter sets
  and slice headers, the VP8 payload header) are readable but authenticated;
  only VP8 and H.264 are negotiated while media is sealed (Firefox prefers VP8,
  because its H.264 encoder is an optional download). A browser that cannot seal
  (no `RTCRtpScriptTransform` or no Ed25519 in WebCrypto) is refused at the
  join, before any device is requested: there is no unsealed fallback in any
  engine. Metadata (who speaks
  when, frame sizes, timing) is visible to the server. Text messages and
  attachments stay readable by the server. **Desktop client.** The desktop
  client does not run the server's JavaScript. It runs a signed bundle of the
  web app that it verifies (Ed25519, a key pinned in the client; built into
  the installer or downloaded from this repository's GitHub releases; refused
  below a signed security floor, or below any bundle version it already
  verified for the server's contract), and reaches the instance only through its own
  proxy, which keeps the session cookie out of the page and hands over anything
  that could render as a document as a download. The desktop shells apply the
  strict web CSP (`style-src 'self'; style-src-attr 'none'`) only for web
  bundles >= 1.1.0; older bundles keep the old policy. Each server runs in an
  isolated browser storage; old desktop storage from before multi-server support
  is wiped once and never handed to a server. An active or compromised
  server therefore cannot inject or replace code in the desktop client; it can
  still do everything else described above (metadata, membership assertions,
  steering newcomers, denial of service, hostile data). Whoever holds the
  bundle signing key, which is not on any server, can still ship a malicious
  app, and the desktop installers are unsigned at the operating-system level (no Authenticode on Windows, no notarization on macOS, no package signature on Linux; a known gap: it needs code-signing certificates that must be bought; the `SHA256SUMS` file in the same release detects a damaged download but is no proof of origin, because whoever can replace the ZIP can replace the checksums). **Desktop self-update.** The client replaces itself through the official Tauri
  updater: the installer runs only if its minisign signature verifies against a
  public key pinned in the client (not the bundle key), the signed version equals the
  announced one, the version is newer than the installed one, and the download is HTTPS from
  github.com. The private key lives in a protected GitHub environment (tags
  `v*` only) and an offline backup. Rotation: ship a release that pins the new
  key, signed with the old key. A leaked CI key (bundle or updater) is revoked
  remotely by a statement signed with an offline root key (`desktop-trust/`, see
  the desktop doc); a client that missed a release needs a manual install. Whoever holds that key can ship
  malicious code to every installed client. Browsers are
  unchanged: the server serves the code. See
  [doc/desktop-trusted-client.md](doc/desktop-trusted-client.md). Sealed media
  in the desktop client is not yet qualified on real Windows hardware.
- **Data retention.** Nothing is deleted automatically by default
  (`MEDIA_RETENTION_DAYS=0`). Operators who enable pruning must say so in their
  privacy notice.
- **Moderation.** Timeouts, bans and kicks need their own permissions and a
  higher role than the member; the Owner cannot be targeted. A kick signs the
  member out everywhere and drops them from their Talk; they can sign in again. A timeout is applied in
  the permission snapshot, so REST, the WebSocket hub and the SFU all honour it
  and overwrites cannot undo it. A ban is a separate state from a disabled
  account and is checked wherever an account may act (sign-in, sessions,
  WebSockets). The checks and the writes happen in one
  transaction under the permission lock. Slow mode is enforced in the message
  insert's transaction. See doc/moderation.md.
- **Audit log.** Administrative actions (roles, permissions, channels,
  invites, members, webhooks, settings, updates) and, in later entries,
  moderation actions, logins, Talk joins and leaves and message deletions and
  edits are recorded in an append-only table (doc/moderation.md). It never
  stores passwords, tokens, secrets, hashes, invite codes or session ids, and
  strips credentials and queries from URLs. It **does** keep the text of
  deleted and edited messages and a history of who was in which Talk when, so
  operators must name it in their privacy notice. Only the Owner and holders
  of *View audit log* can read it; the database refuses updates, and nothing
  is deleted unless `AUDIT_RETENTION_DAYS` is set (default `0`: keep forever).
- **Edit history.** Each edit keeps the text it replaced (at most 50 versions
  per message, `message_edits`). Only holders of *Manage messages* in the
  channel and the Owner can read it (`GET .../messages/{id}/edits`, checked
  against the live permission snapshot, rate limited per member); it is not in
  message lists, events, search, push or webhook payloads. The author's own
  data export includes their earlier versions and nobody else's. It goes with
  the message and with the author's account, and `EDIT_HISTORY_RETENTION_DAYS`
  can expire it. The audit log's `message.edit` entry is a separate record
  (see above). doc/moderation.md#edit-history.
- **Link previews.** Previews use the same address list as outgoing webhooks
  (`internal/ssrf`: metadata, IPv4-compatible, IPv4-translated, discard-only,
  multicast, private, NAT64 and similar ranges are refused after DNS
  resolution, also on redirects). A fetch lasts at most 6 s in total; a host
  that runs into that deadline is not fetched again for 5 minutes, and one
  member holds at most two fetches at a time.
- **Link previews and video playback.** A card is built from what the page
  says about itself: Open Graph and Twitter card tags, the plain description,
  the theme colour and, where a page advertises one, its oEmbed endpoint (JSON
  only; the endpoint's HTML snippet is never read, let alone rendered). Every
  extra fetch (oEmbed, the Steam store API for price and recommendations) goes
  through the same SSRF-guarded client with the same size, time and
  concurrency limits, and results are cached per link (6 hours, failures 5
  minutes). YouTube and Vimeo links ask the provider's oEmbed endpoint first,
  so a consent page in front of the watch page cannot empty the card. All text
  is plain data that the client escapes; colours and links are validated
  again in the browser, and images always load through the same-origin image
  proxy. **Playback** is opt-in per click: a card shows only the thumbnail until
  a member presses play, and nothing from YouTube or Vimeo is requested before.
  The player address is built by the server from the video id in the posted
  link, for exactly two hosts, `www.youtube-nocookie.com/embed/ID` and
  `player.vimeo.com/video/ID?dnt=1`, and checked again by the client against
  the same patterns; an address that a fetched page suggests never becomes a
  frame. The iframe is sandboxed (`allow-scripts allow-same-origin
  allow-presentation allow-popups allow-popups-to-escape-sandbox`: no top
  navigation, no forms, no downloads) and delegates only autoplay, encrypted
  media, picture-in-picture and fullscreen, never camera, microphone or
  location. The web app's CSP gained one directive for this,
  `frame-src https://www.youtube-nocookie.com https://player.vimeo.com`;
  `frame-ancestors 'none'`, `default-src 'self'` and the rest are unchanged.
  Pressing play tells the provider the member's IP address, and the frame is
  loaded with `referrerpolicy="strict-origin-when-cross-origin"` (the page's
  own `no-referrer` would make YouTube refuse the player), so the provider
  sees this server's origin, not the path. The desktop clients keep their
  policy as it is, without `frame-src`: their pages never reach the network
  themselves (see doc/desktop-trusted-client.md), so there the card shows the
  thumbnail, and pressing play opens the link in the system browser. A
  link written as `<https://…>` gets no card at all. The public pages of this
  server (the root and `/invite/CODE`) carry Open Graph tags for messengers:
  the community name, for a working invite the welcome text cut to 200
  characters as plain text, and the Mnema icon (the community icon, once the
  Owner set one), all HTML-escaped; an unusable
  code, or a caller over the invite preview limit, gets the generic page.
  Invite pages ask for `noindex`.
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
  socket or any other Docker API access. Separate containers (below) hold it,
  and only when the operator opts in.
- **Two containers.** The *updater* (`cmd/updater`, started from the app
  image with another entrypoint) decides: it verifies the new image (below),
  pulls it and moves the local tag. The *recreator* (`nickfedor/watchtower`,
  the maintained fork of the archived containrrr/watchtower, pinned by version
  and digest, `WATCHTOWER_NO_PULL`) only restarts the app from the local image;
  it never fetches anything and has no network route out.
- **What they may do.** Both only touch containers carrying the labels
  `com.centurylinklabs.watchtower.enable=true` and `...scope=mnema-talk`,
  which in `docker-compose.yml` is the app alone (not PostgreSQL, SeaweedFS,
  coturn, themselves or anything else on the host). They update the image tag
  the app already runs and recreate the container with the same
  configuration; they cannot be told to run another image, and the updater
  refuses any image outside the configured release repository.
- **When it acts.** Never on a schedule (`WATCHTOWER_HTTP_API_PERIODIC_POLLS=false`):
  only on `POST /v1/update` with `Authorization: Bearer <UPDATER_TOKEN>`.
- **Who can reach it.** No published port. The updater is attached to the
  `internal` network `mnema-updater`, which it shares with the app and
  nothing else, to an `internal` network shared with the recreator only, and
  to `mnema-updater-egress`, its only way out (image registry and Sigstore).
  The recreator has no route out. Both run read-only, with all capabilities
  dropped, `no-new-privileges`, and memory and PID limits.
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
- **Residual risk.** A compromised updater or recreator image, or anyone who
  obtains both the token and a foothold on the `mnema-updater` network, could
  have containers in scope recreated; anyone who compromises either container
  has root on the host (both hold the Docker socket; the updater, which also
  talks to the internet and parses registry and Sigstore data, is the larger
  surface of the two). Leave the profile off if you update by hand anyway.
- **Image verification (SUP-05, since 1.1).** Before anything is pulled or
  restarted the updater resolves the tag the app runs to one digest and checks
  that digest, and only that one:
  - the keyless cosign signature that `release.yml` stores next to the image
    (a Sigstore bundle with the Fulcio certificate, the Rekor entry and an
    RFC 3161 timestamp, checked offline against the Sigstore trusted root,
    fetched through TUF) was made by this repository's release workflow:
    issuer `https://token.actions.githubusercontent.com`, subject exactly
    `https://github.com/msch128/mnema-talk/.github/workflows/release.yml@refs/heads/main`;
  - the BuildKit SLSA v1 provenance exists for every platform image, names that
    image as its subject, and records a GitHub Actions run and source
    repository of this project. BuildKit does not sign it; it is covered by
    the signature because the signed index lists it by digest;
  - **the release is the one that was asked for, and a newer one.** A valid
    signature says who built an image, not which release it is, so a tag that
    is moved back (or a registry mix-up) could offer an older, signed, known-
    vulnerable release. The release version recorded in the signed provenance
    must equal the version the admin confirmed (the app sends it with the
    request) and be strictly newer than the version the running container
    reports; an unknown version on either side is refused, so there is no
    downgrade through "Update now".
  Only then it pulls `repository@digest` (the Docker daemon re-checks the
  content hash), points the local tag at it and lets the recreator restart
  the app from the local image without pulling, so the bytes that run are the
  bytes that were verified and a tag that moves meanwhile changes nothing.
  The check **fails closed**: an unsigned, wrongly signed or tampered image,
  missing or foreign provenance, an image of another repository, or a registry
  or Sigstore that cannot be reached all end with HTTP 422 or 502, no pull,
  no restart and an error in the updater's log. The admin who pressed
  "Update now" sees that the update was refused and that nothing changed, and
  the app logs `SELF-UPDATE REFUSED` with the audit fields. The identity is
  pinned in the code and cannot be changed from the admin console; a fork that
  publishes its own images sets `UPDATER_IMAGE_REPOSITORY`,
  `UPDATER_CERT_IDENTITY_REGEXP` (anchored) and `UPDATER_SOURCE_URL`
  ([doc/operations.md](doc/operations.md#verifying-a-release-image)).
  What it does not give: the signature proves that this workflow built the
  digest, not that the code in it is free of bugs; Sigstore's public-good
  instance and the registry are trusted; the image that is already running is
  not re-checked; and images released before signing was added carry no
  signature, so "Update now" refuses them (update by hand). A digest-pinned
  install is excluded from "Update now" by design.
