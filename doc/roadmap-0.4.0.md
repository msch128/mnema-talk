# Roadmap 0.4.0 — spec and handoff

Single source of truth for the 0.4.0 work. Each workstream lives on its own
`claude/0.4-*` branch and lands on `main` as small PRs. If a branch stops
midway, its `doc/wip/<name>.md` lists what is done, what is left and how to
verify; continue from there. Release 0.4.0 at the end with a `Release-As: 0.4.0`
footer (see the 0.3.0 release for the pattern).

Rules for every change: conventional commits (release-please), commits authored
by the repo owner's git identity, **no Co-Authored-By or other AI trailers**,
`make check`-equivalent green locally before pushing (see AGENTS.md), e2e run in
real Chromium for UI changes (local runs without Docker: see e2e/run.sh; CI's
Chrome has an H.264 encoder, local Chromium 141 does not).

## Talk / chat
- Voice chat opens BELOW the stage via chat icon in Talk header; closed by default; height drag-resizable (like side panels); X closes; unread dot on icon; deep link /v/<id>/chat
- Right member list stays, independently collapsible
- Backend: voice channels get full text chat (was blocked since first commit)
- Voice channels show unread/mentions in sidebar like text channels; notification settings apply
- Threads in voice chats like text channels
- Control bar floating, auto-hides after ~3 s without mouse
- Participant strip under a stream collapsible (remembered)
- Own stream: own preview on stage, pauses when tab inactive (as today)
- One screen share per person (confirm + replace)
- Tiles: avatar centered never clipped; name+admin centered bottom; time only on hover/focus; LIVE top-right + monitor icon; speaking ring + glow

## Stream
- Viewer stream-audio volume separate from voice, 0–100 %, default 50 %, remembered per sharer
- Streamer gear menu: Gaming (1440p60) / Bildschirm (Source 15) / Custom; resolution 720/1080/1440/Source; fps 15/30/60; mute stream audio; Erweitert = live codec/res/fps/bitrate; no Nitro icons
- Default new stream 1080p/30, not persisted across streams; only streamer can change quality

## Admin / update
- Admin tab "System": version, update check (poll every 30 min, ETag, backoff), health (DB+migration, S3+usage, voice/SFU live, server resources)
- Self-update: isolated opt-in updater sidecar (compose profile), token, internal network only, admin click + password re-auth, rate limit, audit log; app never gets docker socket; never auto-update
- Reload banner for ALL users on version mismatch; never auto-reload

## Quality / infra
- Coverage >= 80 % Go and web, enforced in CI
- Split big files (useWebRTC.js, VoiceStage.vue, hub.go) by domain, no behaviour change
- Docs: README + doc/ (docs/ stays gitignored on purpose: private local docs), architecture, operations, upgrade notes per version
- Pipeline: caching (Go, npm, Playwright, Docker layers), timeouts, concurrency
- Multi-arch release images: amd64 + arm64 (+ arm/v7 if whole stack supports it), cross-compile not QEMU
- Desktop app: plan document only (doc/desktop-app.md)
- No extra features beyond this

## Process
- Several PRs sequentially, each green locally + CI before merge; final release 0.4.0 (Release-As)
- Blockers: choose safest variant, note for morning
- Morning report: short in chat with screenshots + NAS command
- Commits authored as Marius, no Claude trailers

## Workstreams and branches

| Branch | Scope | Status |
|---|---|---|
| `claude/0.4-voice-chat` | Backend: voice channels accept messages/uploads/threads/read state; UI: chat panel below the stage, resizable height, unread dot, sidebar unread for voice channels | in progress |
| `claude/0.4-stream-quality` | Fix: stage volume slider controls stream audio (0–100 %, default 50 %); streamer gear menu (mode/resolution/fps/mute/advanced stats), default 1080p/30, one share per person | in progress |
| `claude/0.4-tiles` | ParticipantTile redesign (centered avatar, time on hover/focus, LIVE top-right) | in progress |
| `claude/0.4-system` | Version plumbing (build args → internal/version + web), reload banner for all users, admin System tab (health, update check every 30 min), optional isolated self-update sidecar | in progress |
| `claude/0.4-ci` | CI caching/timeouts/concurrency, multi-arch release images (amd64, arm64, arm/v7 if the stack supports it) | in progress |
| — | Talk polish: floating control bar auto-hide (3 s), collapsible participant strip, overall spacing vs. Discord | todo |
| — | Refactor: split useWebRTC.js, VoiceStage.vue, internal/ws/hub.go by domain, no behaviour change | todo (after Talk work lands) |
| — | Coverage ≥ 80 % Go and web, then enforce in CI | todo |
| — | Docs: README features/update path, doc/architecture.md, doc/operations.md, doc/upgrade-0.4.md, doc/desktop-app.md (plan only) | todo |
| — | Release 0.4.0 (`Release-As: 0.4.0`) | todo |

## Security requirements (self-update) — non-negotiable
- The app container never gets the Docker socket or any Docker API access.
- Updater sidecar is opt-in (compose profile), pinned version, reachable only on the
  internal compose network with a ≥32-char token, may only update the mnema-talk
  container.
- Trigger: admin only, CSRF/origin checked, password re-auth, rate limited, audit
  logged, only when an update is available, never automatic.
