# Mnema Talk 1.0 scorecard

How far the app is from a solid 1.0 for a small private community on one
server. Every item is a concrete, checkable criterion. An area's score is the
share of its checked items; the total is the weighted sum. `make scorecard`
prints it (`scripts/scorecard.mjs`).

Rules: tick an item only when it is merged to `main`, covered by a test (or,
for pure UI, verified in a browser), and deployed. 1.0 ships at **≥ 90 %**
with no unchecked item in "Voice & screenshare" or "Security".

## Security (weight 10)

- [x] Invite-only registration, admin creates invites
- [x] HttpOnly `__Host-` session cookie, token_version revocation on password change
- [x] CSRF origin check on every non-GET API request and on the WebSocket
- [x] Strict CSP and security headers, no `v-html` without escaping
- [x] Uploads typed by sniffing, risky types download-only
- [x] Secret scan, govulncheck, npm audit and CodeQL clean in CI
- [x] Login lockouts keyed per client IP + username, with a separate per-account cap
- [ ] Real client IP reaches the app behind the reverse proxy (no hairpin NAT address)
- [x] Admin cannot be locked out by failed logins from other clients
- [x] "Log out everywhere" revokes all sessions server-side
- [x] One `APP_ENV` definition for production checks
- [x] Invite check before password hashing on registration

## Text chat (weight 14)

- [x] Send, edit, delete own messages
- [x] Replies and threads
- [x] Reactions
- [x] Markdown with escaping, spoilers, code
- [x] Image/video/file uploads with inline preview
- [x] Keyset history paging with a capped message window
- [ ] Resync of channels, members and messages after a WebSocket reconnect
- [ ] Network errors and 5xx never log the user out (only 401 does)
- [ ] Thread replies paged
- [ ] Deleting a message or channel removes its media from S3
- [ ] Links containing `(@` render correctly

## Chat comfort (weight 12)

- [ ] Unread badges per channel, persisted server-side
- [ ] "New messages since" divider and mark as read
- [ ] @mentions with mention badge and highlight
- [ ] Browser notifications (permission prompt, per-channel setting: all / mentions / mute)
- [ ] Typing indicator
- [ ] Search with filters (channel, author, has attachment) and jump to message
- [ ] Link previews for http(s) URLs (server-side fetch, SSRF-safe)

## Navigation & interaction (weight 10)

- [ ] URL routes for channels, voice channels and single messages; shared links resolve after login
- [ ] Opening a voice link shows the talk without joining; talk chat viewable without joining
- [ ] Right-click menu on messages (reply, react, thread, edit, copy, copy link, mark unread, delete)
- [ ] Right-click menu on channels and categories (mark read, copy link, notifications, edit, delete)
- [ ] Right-click menu on members (profile, mention, volume, local mute, admin actions)
- [ ] Message hover shows only reaction, reply and edit buttons; every action also in the menu
- [ ] Every icon button has a tooltip and an `aria-label`
- [ ] Toasts instead of `alert()`/`confirm()`, confirmation dialogs for destructive actions
- [ ] Connection-lost banner with automatic reconnect

## Voice & screenshare (weight 16)

- [x] Join/leave, mute, deafen, push-to-talk, per-user volume
- [x] Voice resume after a reload within 30 s
- [x] Noise gate with a live meter that always shows the raw input level
- [x] SFU registers interceptors (NACK, RTCP reports, TWCC) and forwards PLI/FIR to the publisher
- [x] New screenshare viewers get a keyframe from the publisher immediately
- [x] Rejoin and late unregister never remove the new peer
- [x] SFU track IDs scoped per peer; rooms cannot be deleted during a join
- [ ] Client join/leave race-free (no ghost in call, no leaked mic)
- [ ] Voice re-established or visibly ended after a WebSocket reconnect; voice ends on logout
- [ ] Push-to-talk ignores typing, releases on blur, listeners never leak
- [ ] Audio setting changes apply to a live call
- [ ] Screenshare start/stop without dropping audio (renegotiation without a full rebuild)
- [ ] SFU and WebSocket hub unit tests

## Video (weight 5)

- [ ] Webcam on/off in a talk, video tiles for all participants
- [ ] Bandwidth adapts (simulcast or layer selection) so 4K60 shares stay watchable

## Moderation & admin (weight 8)

- [x] Invites and media management in the admin panel
- [ ] User management: list, disable/enable, reset password, end sessions
- [ ] Kick from talk, ban
- [ ] Admin deletes other users' messages
- [ ] Rename and reorder channels and categories

## Mobile, accessibility & language (weight 8)

- [ ] Responsive layout down to 390 px (drawer for channels and members)
- [ ] Voice usable on mobile
- [ ] Dialogs have `role="dialog"`, focus trap and Escape
- [ ] All message actions reachable by keyboard
- [ ] UI in German (default) and English, switchable per user; no hard-coded UI strings
- [ ] Voice view named "Tafelrunde" / "Roundtable" consistently

## Operations (weight 10)

- [x] Health checks for app, Postgres and SeaweedFS
- [x] Versioned, locked migrations; image release pipeline
- [x] Media retention off by default
- [x] Automatic nightly backup of Postgres and S3 data with retention
- [x] Documented and tested restore
- [ ] TURN server support for users behind restrictive NATs
- [ ] Metrics or monitoring (at least an uptime check and error-rate alert)
- [ ] Range requests for media (video seeking, Safari)

## Quality (weight 7)

- [x] CI: lint, unit and integration tests, race detector, Docker build
- [x] Frontend unit tests for stores and libs
- [ ] Tests for `useWebRTC` join/leave and reconnect behaviour
- [ ] End-to-end smoke test (login, send message, join talk) in CI
- [ ] First release cut (0.x) with changelog
