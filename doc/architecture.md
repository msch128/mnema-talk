# Architecture

Mnema Talk is one Go binary that serves the REST API, a WebSocket hub, a
WebRTC selective forwarding unit (SFU) and the built Vue web app. State lives
in PostgreSQL; uploads and avatars live in S3-compatible object storage
(SeaweedFS in the default `docker compose` stack).

```
            Browser (Vue 3 SPA)
     HTTPS / WSS │        │ UDP (DTLS-SRTP)
                 ▼        ▼
   ┌───────────────────────────────────────┐
   │ mnema-talk  (single Go binary)        │
   │  http router ─ REST handlers          │
   │  ws hub ───── events, presence,       │
   │               voice signaling         │
   │  sfu ──────── Pion WebRTC forwarding  │
   │  web.go ───── embedded web/dist       │
   └──────────┬─────────────────┬──────────┘
              ▼                 ▼
      PostgreSQL 18       SeaweedFS (S3 API)
```

## Backend components

| Package | Role |
|---|---|
| [`cmd/server`](../cmd/server/main.go) | Entry point: loads the config, sets up JSON logging, connects the database and storage, starts the retention worker, the SFU, the update checker and the HTTP server, shuts down gracefully. |
| [`internal/config`](../internal/config/config.go) | Reads every environment variable, applies defaults and validates them; the server refuses to start on a bad value. See [operations.md](operations.md#configuration-reference). |
| [`internal/server`](../internal/server/server.go) | Router wiring (chi): middleware, `/api/health`, `/api/metrics`, `/api/legal`, the API docs, the WebSocket endpoint, the authenticated and admin route groups, and the admin System tab ([`system.go`](../internal/server/system.go), [`system_update.go`](../internal/server/system_update.go), [`system_selfupdate.go`](../internal/server/system_selfupdate.go)). Integration tests live here too. |
| [`internal/httpx`](../internal/httpx/middleware.go) | Middleware: request IDs, security headers (CSP, HSTS in production), CORS, CSRF origin check, body limits, rate limits ([`ratelimit.go`](../internal/httpx/ratelimit.go)), client IP from trusted proxies ([`request.go`](../internal/httpx/request.go)), JSON errors. |
| [`internal/auth`](../internal/auth/service.go) | Password hashing, signed session cookies with a per-user `token_version` (revocation), login lockout, invite codes, admin seeding, user administration, password re-authentication for sensitive actions ([`reauth.go`](../internal/auth/reauth.go)). |
| [`internal/chat`](../internal/chat/handler.go) | Categories, channels (text and voice), layout, messages, replies, threads, reactions, mentions, read state, notification settings, search, members. |
| [`internal/media`](../internal/media/handler.go) | Uploads with content sniffing and an allow-list ([`policy.go`](../internal/media/policy.go)), authenticated media serving through the app, admin storage stats and pruning, and the retention worker (off unless `MEDIA_RETENTION_DAYS > 0`). |
| [`internal/s3`](../internal/s3/s3.go) | S3 client (AWS SDK v2); creates the bucket only when missing. |
| [`internal/db`](../internal/db/db.go) | pgx pool and the embedded, numbered migrations in [`internal/db/migrations/`](../internal/db/migrations/). Each applied file's checksum is recorded in `schema_migrations`; an edited migration stops startup. |
| [`internal/events`](../internal/events/events.go) | The `Publisher` interface between feature packages and the hub (`Broadcast`, `SendToUsers`), with an in-memory recorder for tests. |
| [`internal/ws`](../internal/ws/hub.go) | WebSocket hub: one connection per browser session; chat events, presence and idle state, typing, voice rooms ([`voice.go`](../internal/ws/voice.go)), mute/deafen state, speaking indicators and WebRTC signaling for the SFU. |
| [`internal/sfu`](../internal/sfu/sfu.go) | Pion WebRTC SFU: one room per voice channel, forwards RTP without transcoding; per-viewer subscriptions ([`subscriptions.go`](../internal/sfu/subscriptions.go)); announced addresses from `WEBRTC_NAT_1TO1_IP`, re-resolved every 5 minutes ([`announce.go`](../internal/sfu/announce.go)). |
| [`internal/linkpreview`](../internal/linkpreview/linkpreview.go) | Link cards: fetches title, description and image of public pages with an SSRF guard (http(s) on 80/443 only, every resolved address checked, also on redirects) and a cache. |
| [`internal/update`](../internal/update/checker.go) | Release check against GitHub every 30 minutes (ETag, backoff on 403/429), image-tag reach analysis ([`image.go`](../internal/update/image.go)) and the client for the optional updater sidecar ([`updater.go`](../internal/update/updater.go)). |
| [`internal/version`](../internal/version/version.go) | Version and commit baked in at build time (`VERSION` / `REVISION` build args). |
| [`web/web.go`](../web/web.go) | Embeds `web/dist` with `//go:embed` and serves the SPA. |

## Data flow: chat

1. The browser sends `POST /api/channels/{id}/messages` (or `/upload` for an
   attachment). The request passes the CSRF origin check, the session check
   and the per-user rate limit.
2. [`chat.InsertMessage`](../internal/chat/post.go) stores the message and its
   mentions in one transaction (an upload's media row joins the same
   transaction).
3. [`chat.PublishMessage`](../internal/chat/post.go) loads the stored message,
   broadcasts `message_create` through the `events.Publisher` and answers 201.
4. The hub writes the event to every open WebSocket; the
   [chat store](../web/src/stores/chat.js) inserts it, updates unread and
   mention counters and plays the notification according to the channel's
   notification setting.

Every channel has a text chat, voice channels included (the chat panel under
the Talk stage). Edits, deletes, reactions, typing and read state follow the
same pattern: REST for changes, WebSocket events for fan-out. Media is never
served from S3 directly: `GET /api/media/{id}` checks the session and streams
the object through the app.

## Data flow: voice, cameras and screen share

Signaling runs over the existing WebSocket; media goes over UDP straight to
the SFU (`WEBRTC_UDP_PORT_MIN`–`MAX`), or through TURN when configured.

1. **Join.** Clicking a voice channel sends `voice_join`. The hub
   ([`voice.go`](../internal/ws/voice.go)) leaves any previous room, calls
   `sfu.Join` and announces the participant. The SFU is the offerer: it sends
   `webrtc_offer` and `webrtc_candidate`; the browser answers with
   `webrtc_answer` and its own candidates.
2. **Publish.** Each peer connection has fixed receive lines, told apart by
   position: the microphone (audio), the screen (first video line) and the
   camera (second video line). In 0.4.0 a second audio line carries the
   screen's sound separately (stream ID `screen:<user>`); before that, the
   screen's sound was mixed into the microphone track. Audio and screen are
   published under the user's ID as stream ID, a camera under `cam:<user>`
   ([`sfu.go`](../internal/sfu/sfu.go)).
3. **Forward.** For every incoming track the SFU creates one local track and
   copies the RTP packets to it ([`forward`](../internal/sfu/sfu.go)); a
   write error for one subscriber never ends the track for the others. No
   decoding, no transcoding, no recording. Viewers' keyframe requests (PLI/FIR)
   go to the publisher, rate-limited per track (300 ms), so a burst of
   viewers costs the publisher one keyframe.
4. **Subscriptions (opt-in).** What each viewer receives is decided per
   viewer ([`subscriptions.go`](../internal/sfu/subscriptions.go)): voice
   audio is always forwarded; cameras are on by default and can be turned off
   per person or for everyone; **screen shares are off by default** and only
   sent after the viewer clicks "Watch" (`webrtc_subscribe`). The screen's
   sound follows the screen subscription. Changes renegotiate with that
   viewer only. The hub tells the room who publishes what
   (`webrtc_media_state`) and who watches each share (`screen_viewers`), so
   clients can offer "Watch" and show viewers without receiving the video.
5. **Stop.** `webrtc_screenshare_stop` / `webrtc_camera_stop` unpublish that
   source; the screen opt-ins belong to one share and are dropped with it.
   Leaving, a failed connection or an admin kick removes the peer.

**Codec pinning.** The SFU pins the codec order of the first offer on each
video line ([`pinCodecOrder`](../internal/sfu/sfu.go)). Without it, Pion
re-offers every video line in the order of the client's screen line, where the
web client puts H.264 first; a browser would then switch a running camera
from VP8 to H.264 at the next renegotiation while the forwarded track keeps
the first codec, and viewers could not decode it. The client still reorders
its own screen line so screen shares prefer H.264 (hardware encoding) and get
a higher start bitrate ([`tuneScreenOffer`](../web/src/composables/useWebRTC.js)).

**Stream quality and volume (0.4.0).** The sharer chooses mode, resolution
and frame rate; the client applies them as capture constraints and sender
parameters (`maxFramerate`, a bitrate cap, `scaleResolutionDownBy`), so the
SFU still forwards whatever arrives. Viewers play the screen's sound in its
own element with a per-sharer volume (0–100 %, default 50 %), independent of
the person's voice volume.

## Frontend structure (`web/src`)

- **Entry**: [`main.js`](../web/src/main.js), [`App.vue`](../web/src/App.vue)
  (routing via [`lib/router.js`](../web/src/lib/router.js), layout, dialogs).
- **Stores (Pinia)**:
  [`auth`](../web/src/stores/auth.js) (current user; the session itself is an
  HttpOnly cookie), [`chat`](../web/src/stores/chat.js) (channels, messages,
  WebSocket events, unread state, presence),
  [`voice`](../web/src/stores/voice.js) (call state, devices, input mode and
  push-to-talk key, per-person volumes, camera and stage choices),
  [`appVersion`](../web/src/stores/appVersion.js) (reload banner on version
  mismatch), [`toast`](../web/src/stores/toast.js).
- **Composables**: [`useWebRTC`](../web/src/composables/useWebRTC.js) (peer
  connection, publishing mic/screen/camera, push-to-talk keys, screen offer
  tuning, stats), [`useTalkStage`](../web/src/composables/useTalkStage.js) and
  [`useVideoGrid`](../web/src/composables/useVideoGrid.js) (stage and grid
  layout), [`usePictureInPicture`](../web/src/composables/usePictureInPicture.js),
  [`useSidebarReorder`](../web/src/composables/useSidebarReorder.js),
  [`useSortableDrag`](../web/src/composables/useSortableDrag.js) and
  [`useChannelLayout`](../web/src/composables/useChannelLayout.js) (drag and
  drop with immediate save and undo), [`useNavMenus`](../web/src/composables/useNavMenus.js)
  (context menus), [`useMessageActions`](../web/src/composables/useMessageActions.js),
  [`useMessageKeyboard`](../web/src/composables/useMessageKeyboard.js),
  [`useResizable`](../web/src/composables/useResizable.js), [`useDialog`](../web/src/composables/useDialog.js).
- **Components**: [`Sidebar.vue`](../web/src/components/Sidebar.vue),
  [`ChatArea.vue`](../web/src/components/ChatArea.vue),
  [`ThreadSidebar.vue`](../web/src/components/ThreadSidebar.vue),
  [`VoiceStage.vue`](../web/src/components/VoiceStage.vue) (the Talk view),
  [`ParticipantTile.vue`](../web/src/components/ParticipantTile.vue),
  [`VoiceChatPanel.vue`](../web/src/components/VoiceChatPanel.vue),
  [`ScreenViewers.vue`](../web/src/components/ScreenViewers.vue),
  [`PipHost.vue`](../web/src/components/PipHost.vue),
  [`MemberList.vue`](../web/src/components/MemberList.vue),
  [`AdminDashboard.vue`](../web/src/components/AdminDashboard.vue) with one
  component per tab (users, layout, invites, media,
  [system](../web/src/components/AdminSystemTab.vue)),
  [`UpdateBanner.vue`](../web/src/components/UpdateBanner.vue).
- **Libraries** (`lib/`): pure, unit-tested logic such as
  [`api.js`](../web/src/lib/api.js) (fetch wrapper), [`markdown.js`](../web/src/lib/markdown.js),
  [`channelLayout.js`](../web/src/lib/channelLayout.js),
  [`voiceSession.js`](../web/src/lib/voiceSession.js) (rejoin within 30 s),
  [`noiseSuppressor.js`](../web/src/lib/noiseSuppressor.js),
  [`thirdParty.js`](../web/src/lib/thirdParty.js) (in-app license list).
- **i18n**: [`i18n/de.json`](../web/src/i18n/de.json) and
  [`i18n/en.json`](../web/src/i18n/en.json); tests fail on hard-coded UI text
  and unused keys.

## Key design decisions

- **One binary, three containers.** API, hub, SFU and web app ship together
  ([`web/web.go`](../web/web.go)); `docker compose` adds only PostgreSQL and
  SeaweedFS. Pure Go (`CGO_ENABLED=0`), so images cross-compile for amd64 and
  arm64 without QEMU ([`Dockerfile`](../Dockerfile)).
- **Forward, never transcode.** The SFU only copies RTP
  ([`internal/sfu`](../internal/sfu/sfu.go)); quality is decided by the
  sender. Media is DTLS-SRTP between each client and the SFU, **not**
  end-to-end encrypted ([SECURITY.md](../SECURITY.md#security-model-summary)).
- **Screen shares are opt-in per viewer** so an idle viewer costs no
  bandwidth ([`subscriptions.go`](../internal/sfu/subscriptions.go)).
- **Invite-only, single admin.** No public registration; the admin is seeded
  from `ADMIN_USERNAME` ([`admin_seed.go`](../internal/auth/admin_seed.go)).
- **Sessions revocable by version.** Password change or "log out everywhere"
  bumps `token_version`, invalidating every cookie and closing the user's
  sockets ([`session.go`](../internal/auth/session.go), [`token.go`](../internal/auth/token.go)).
- **Forward-only, checksummed migrations** run at startup
  ([`db.go`](../internal/db/db.go)); never edit an applied one.
- **Retention off by default.** `MEDIA_RETENTION_DAYS=0` deletes nothing
  ([`admin.go`](../internal/media/admin.go)).
- **The app never touches Docker.** Self-update goes through an opt-in
  sidecar on an internal network with a token, admin password re-auth, a
  5-minute cooldown and an audit log entry
  ([`system_selfupdate.go`](../internal/server/system_selfupdate.go),
  [`docker-compose.yml`](../docker-compose.yml),
  [SECURITY.md](../SECURITY.md#self-update-sidecar)).
- **Reload, never auto-reload.** The server sends its version on every
  WebSocket connection; a page built for another version shows a reload
  banner ([`appVersion.js`](../web/src/stores/appVersion.js)).
