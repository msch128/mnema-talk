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
| [`internal/perm`](../internal/perm/perm.go), [`internal/roles`](../internal/roles/handler.go) | Roles and permissions: the permission bits, the in-memory `Snapshot` that resolves base, role and overwrite permissions (and the hierarchy rules), the cache with live invalidation, and the HTTP API for roles, member roles and channel overwrites. See [roles-and-permissions.md](roles-and-permissions.md). |
| [`internal/chat`](../internal/chat/handler.go) | Categories, channels (text and voice), layout, messages, replies, threads, reactions, mentions (by name, `@all`/`@here` and `@Role`, with a fan-out cap) and the mentions inbox, read state, notification levels ([`notify.go`](../internal/chat/notify.go)), search ([`search.go`](../internal/chat/search.go)), members. |
| [`internal/onboarding`](../internal/onboarding/handler.go) | The first-run side of the community: name, welcome text and rules, who accepted which rules version, dismissed tips and the public invite preview. |
| [`internal/media`](../internal/media/handler.go) | Uploads with content sniffing and an allow-list ([`policy.go`](../internal/media/policy.go)), authenticated media serving through the app, admin storage stats and pruning, and the retention worker (off unless `MEDIA_RETENTION_DAYS > 0`). |
| [`internal/s3`](../internal/s3/s3.go) | S3 client (AWS SDK v2); creates the bucket only when missing. |
| [`internal/db`](../internal/db/db.go) | pgx pool and the embedded, numbered migrations in [`internal/db/migrations/`](../internal/db/migrations/). Each applied file's checksum is recorded in `schema_migrations`; an edited migration stops startup. |
| [`internal/events`](../internal/events/events.go) | The `Publisher` interface between feature packages and the hub (`Broadcast`, `SendToUsers`), with an in-memory recorder for tests. |
| [`internal/ws`](../internal/ws/hub.go) | WebSocket hub: one connection per browser session; chat events, presence and idle state, typing, voice rooms ([`voice.go`](../internal/ws/voice.go)), mute/deafen state, speaking indicators and WebRTC signaling for the SFU. |
| [`internal/sfu`](../internal/sfu/sfu.go) | Pion WebRTC SFU: one room per voice channel, forwards RTP without transcoding; per-viewer subscriptions ([`subscriptions.go`](../internal/sfu/subscriptions.go)); announced addresses from `WEBRTC_NAT_1TO1_IP`, re-resolved every 5 minutes ([`announce.go`](../internal/sfu/announce.go)). |
| [`internal/webhook`](../internal/webhook/), [`internal/ssrf`](../internal/ssrf/) | Webhooks: incoming URLs per channel in Discord's execute format (messages authored by a hidden bot account per webhook), outgoing signed events with a durable retry queue and delivery log, and the guard for outbound requests to member-chosen addresses (resolves names itself, refuses non-public addresses, connects to the vetted address). See [webhooks.md](webhooks.md). |
| [`internal/audit`](../internal/audit/), [`internal/bot`](../internal/bot/), [`internal/gateway`](../internal/gateway/) | The audit log with per-entry visibility classes, bot accounts, tokens (bound to the member who issued them) and slash commands, and the Discord-style bot gateway. See [bots.md](bots.md) and [moderation.md](moderation.md). |
| [`internal/metrics`](../internal/metrics/) | Process-wide counters (HTTP, realtime, SFU, storage, rate limits) rendered in the Prometheus text format without a client library; every labelled family has a series cap. Feeds `/api/metrics` and the live operator view in the admin System tab. See [operations.md](operations.md#monitoring-and-alerts). |
| [`internal/webbundle`](../internal/webbundle/), [`cmd/webbundle`](../cmd/webbundle/) | Builds, signs (Ed25519) and verifies the web app bundle that the [trusted desktop client](desktop-trusted-client.md) runs instead of server-served JavaScript. |
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
   [chat store](../web/src/stores/chat.ts) inserts it, updates unread and
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
a higher start bitrate ([`tuneScreenOffer`](../web/src/composables/useWebRTC.ts)).

**Layers of a screen share (0.8).** A sharer can send its screen as up to three
separate encodings of the same capture ("layers", best first), and each viewer
receives exactly one of them. The SFU offers three more receive lines for this,
after the four above: the screen's lower layers are the third and fourth
video line ([`screen.go`](../internal/sfu/screen.go),
[`layers.go`](../internal/sfu/layers.go)). They are plain lines, not RTP
simulcast (`rid`): the SFU is the offerer and a browser cannot add simulcast to
an answer, whereas separate lines work in every browser and need no SDP
munging. A client that does not know them answers them inactive, so old and
new clients interoperate in both directions. Per viewer the SFU forwards one
layer on one outgoing track and switches layers only where a keyframe of the
new layer starts (a VP8 keyframe tag or an H.264 parameter set, both of which
the browser's media transform leaves readable), asking the publisher for one
when it switches. Sequence numbers, timestamps and VP8 picture IDs and
`TL0PICIDX` are rewritten so the viewer sees one continuous stream; the sealed
payload is never touched. The sender declares the layers (`webrtc_layers`,
announced as `screen_layers`), a viewer picks one with `quality` on
`webrtc_subscribe` and is told what it receives (`screen_quality`), and the
sender is told which layers anyone needs (`screen_layers_wanted`) so it can
stop encoding the others. A layer nobody needs any more stays wanted for eight
seconds, so switching back and forth does not restart an encoder each time.
**Auto.** A viewer who leaves the choice to the server starts on layer 0 and is
moved by what its browser reports ([`adapt.go`](../internal/sfu/adapt.go)):
the loss fraction and round trip time of its RTCP receiver reports (about
every second), how often it asks for a keyframe, and a bandwidth estimate.
Lasting loss (5 % twice in a row, 15 % at once), a round trip time 250 ms above
its recent minimum or three keyframe requests in five seconds move it down one
layer; the reports of the three seconds after any move are ignored (a keyframe
burst loses packets on its own). After 12 seconds without trouble it tries one
layer up, and a try that fails within eight seconds makes the next one wait
twice as long (up to two minutes), so a link that carries the lower layer but
not the higher one does not flap. A viewer who picked a layer is never moved.
The SFU cannot read the media; it never estimates from it.

*Bandwidth* ([`bwe.go`](../internal/sfu/bwe.go)). A link that is too slow for
a layer but queues instead of dropping shows neither loss nor a higher round
trip time for a long while. So each viewer connection carries Pion's Google
Congestion Control estimator (`pkg/gcc` of `pion/interceptor`): the SFU stamps
the transport-cc sequence number on every outgoing packet (beneath the
authorization gate, so a refused packet never leaves a gap the browser would
report as lost), the browser answers with transport-wide feedback (arrival
time of each packet) and the estimator turns both into a bitrate. It is only
read: the pacer is a pass-through, so nothing is delayed or dropped, and
audio, cameras and viewers who chose a layer are unaffected. The cost is the
estimator's history of 250 packets and two small goroutines per connection.
The estimate is compared with the bitrate each layer has at the SFU (bytes
arriving per layer, measured once a second per share, smoothed; a layer the
sender switched off keeps its last size). Delivery that keeps up makes the
estimate about 1.5 times the bitrate sent, so an estimate below 1.2 times the
current layer's bitrate for four reports in a row counts as trouble and moves
the viewer down one layer. The estimate cannot rise above 1.5 times what is
being sent, so it can never show that a better layer fits; a viewer moved down
this way therefore stays put for a minute unless the estimate is above 1.5
times the better layer's bitrate for four reports, and then tries once more
under the usual backoff. The estimates are counted in
`mnema_sfu_bandwidth_estimate_reports_total{range}` (seven fixed ranges) and the
moves they cause in `mnema_sfu_bandwidth_layer_moves_total`. Browsers
that send no transport-wide feedback leave the estimate at "unknown" and the
rules above are the only ones. `BW_KBPS` of the load test gives each viewer a
link with a rate and a queue to try this.

Cameras and audio are not layered (small pictures, and every layer costs the
sender an encoder).

The web client ([`lib/screenLayers.ts`](../web/src/lib/screenLayers.ts),
`syncLayers` in [`useWebRTC.ts`](../web/src/composables/useWebRTC.ts)) sends
the share as the streamer chose it on layer 0 and up to two smaller ones
(1080p, 720p, 480p or 360p, at most 30 fps, at least a quarter smaller than the
layer above) from the same capture with `scaleResolutionDownBy` on the extra
lines. The lower layers stay switched off (`active=false`) until the server
says a viewer needs them, and layer 0 is switched off when everybody watches
a lower one. It uses these lines only when `server_info.screen_layers` says the
server has them. Viewers pick Auto or a layer in a menu on the stage, labelled
with the sizes the sender declared. The sender watches its capture: when the
width, height or frame rate it delivers (a resized window) has changed and
stayed so for a second, it sizes the best layer for the new picture and
declares the layers again; the server announces them only if they differ, and
the pickers follow live. A viewer's explicit choice is a position among the
layers and survives new sizes, but not a different number of layers or other
frame rates. Each layer's frames are sealed in a stream
of their own (see SECURITY.md), so a switch needs no coordination between
sender and receiver.

**Stream quality and volume (0.4.0).** The sharer chooses mode, resolution
and frame rate; the client applies them as capture constraints and sender
parameters (`maxFramerate`, a bitrate cap, `scaleResolutionDownBy`), so the
SFU still forwards whatever arrives. Viewers play the screen's sound in its
own element with a per-sharer volume (0–100 %, default 50 %), independent of
the person's voice volume.

**Media key lifecycle (0.8).** The browsers of a Talk form an MLS group and
derive the media keys from it; the server only relays the handshakes and
stamps the sender. The member with the lowest id commits a fresh key about
every 30 minutes, every member renews its own leaf key, members that showed no
sign of life for 20 minutes are removed and re-admitted when they return, and
every sealed frame is signed with its sender's leaf key. The connection
details show a **safety code** for the current epoch so members can compare
keys out of band. The server validates who may commit but never sees keys.
The code is [`web/src/lib/media/`](../web/src/lib/media/). Details and limits:
[SECURITY.md](../SECURITY.md#security-model-summary).
[`doc/crypto-state.md`](crypto-state.md) is an unused persistence foundation.

## Notifications, search and onboarding (0.9)

Three features that cut across the chat path. All of them are enforced on the
server; the client only renders the result.

**Notification levels.** A member sets `all`, `mentions` or `mute` per channel
and per category, a mute optionally with an end time. The stored value
`inherit` (the default) follows the category, then `all`; the level a client
receives is always the effective one. Settings are rows in `channel_reads` and a
category counterpart (migration 0035) and are changed through
`PUT /api/channels/{id}/notifications` and `PUT /api/categories/{id}/notifications`.
Levels only decide who is *notified*: unread state and the mentions inbox
(`GET /api/mentions`) are independent, so a muted channel still shows what was
addressed to the member.

**Mentions.** A message stores one `message_mentions` row per notified member,
with a `kind` (by name, `@all`/`@here`, through a role) and the mentioned roles
in `message_role_mentions` (migration 0036), so a pill survives a role rename.
Webhooks and bots mention roles too, subject to `allowed_mentions`. An author
without the right to mention everyone notifies at most 50 members per message
([`mentions_cap.go`](../internal/chat/mentions_cap.go)).

**Search.** `GET /api/search` runs trigram-indexed substring matching on
message text (`pg_trgm`, one index since migration 0038) with filters (channel,
author, mentions, attachment kind, date window). Results are limited to the
channels the caller can read, using the same permission snapshot as message
listing; a query has a length and term cap, a database statement timeout and
at most two searches in flight per member.

**Onboarding and rules.** The Owner (or Administrators) writes a welcome text,
rules and a start channel through `/api/admin/onboarding` (migration 0037).
Publishing rules creates a rules version; a member who has not accepted the
current one is pending. Enforcement is part of the permission resolver
([`internal/perm`](../internal/perm/snapshot.go)): a pending member loses what
a timeout takes (posting, reacting, uploading, joining a Talk) and receives
`RULES_NOT_ACCEPTED`, in REST, the hub and the SFU alike. Empty rules enforce
nothing, which is why an upgrade never locks anyone out; the Owner and bots are
exempt. `GET /api/invites/{code}` (the one public read) serves the community
name for the sign-up page; it is rate limited and refuses weak legacy codes.

## The 1.0 contract

From 1.0.0 the public surfaces are versioned with semantic versioning
([api-stability.md](api-stability.md)): the REST API as `api/openapi.json`, the
WebSocket event registry in [`internal/events`](../internal/events/types.go),
bot and webhook payloads, the desktop bridge, environment variables and metric
names. The contract is enforced mechanically, not by convention:

- `make openapi-check` fails when `api/openapi.json` is stale and `make
  openapi-diff` fails on an incompatible change against the latest release
  tag, unless a commit carries a `BREAKING CHANGE:` footer.
- Event names are constants in `internal/events`; a golden file
  (`internal/events/testdata/events.golden.json`) and a comparison with
  `web/src/types/events.ts` fail the build when the registry and the client
  types drift.
- The alert rules in [`deploy/monitoring/mnema-alerts.yml`](../deploy/monitoring/mnema-alerts.yml)
  are checked with `promtool`, and an integration test fails when a rule names
  a metric `/api/metrics` does not export.
- Upgrade and restore drills install each supported release from GHCR, seed
  data through its own API, move to the build under test and check the data;
  a fresh-install check follows [deployment.md](deployment.md) on an empty
  directory.

The database schema, `internal/...` packages and the operator-only admin
endpoints are not part of the contract.

## Capacity

Measured on a 4-core home NAS with the default `APP_MEM_LIMIT=1g`: one browser
sharing a 640x360 screen at 20 fps to 99 members in one Talk used about 28 %
of one core and about 500 MB of app memory, each receiver getting about
320 kbit/s (0.7.5). App memory grows with receivers x forwarded video sources,
because Pion keeps a retransmission history of about 1.5 KB per packet for every
outgoing video track. Since 0.7.7 that history shrinks with the number of
connected members (1024 packets up to 16, halving as the count doubles, at
least 128 from 65 on; see `internal/sfu/nack.go`), so 100 receivers x 10
cameras stay around 200 MB instead of 1.5 GB. The price in large calls is that
a late retransmission request goes unanswered and the picture shows an artifact
until the next keyframe. Memory is returned after the call. With about 16 real browsers plus 84
lightweight members (100 total) the app used about 1.6 cores. Real E2EE browser
clients were verified with up to 24 viewers in one call; beyond that the single
test machine, not the server, was the limit. These are single runs, not a
qualified capacity guarantee.

The measurements are reproducible with `make load-test` (see
[`loadtest/README.md`](../loadtest/README.md)): one sharer, real browsers and
lightweight Pion viewers, with join/reload stall checks, optional packet loss
and jitter, server heap and goroutine sampling, and thresholds that fail the
run. It can also run against a remote instance.

## Frontend structure (`web/src`)

- **Entry**: [`main.ts`](../web/src/main.ts), [`App.vue`](../web/src/App.vue)
  (routing via [`lib/router.ts`](../web/src/lib/router.ts), layout, dialogs).
- **Stores (Pinia)**:
  [`auth`](../web/src/stores/auth.ts) (current user; the session itself is an
  HttpOnly cookie), [`chat`](../web/src/stores/chat.ts) (channels, messages,
  WebSocket events, unread state, presence),
  [`voice`](../web/src/stores/voice.ts) (call state, devices, input mode and
  push-to-talk key, per-person volumes, camera and stage choices),
  [`appVersion`](../web/src/stores/appVersion.ts) (reload banner on version
  mismatch), [`toast`](../web/src/stores/toast.ts).
- **Composables**: [`useWebRTC`](../web/src/composables/useWebRTC.ts) (peer
  connection, publishing mic/screen/camera, push-to-talk keys, screen offer
  tuning, stats), [`useTalkStage`](../web/src/composables/useTalkStage.ts) and
  [`useVideoGrid`](../web/src/composables/useVideoGrid.ts) (stage and grid
  layout), [`usePictureInPicture`](../web/src/composables/usePictureInPicture.ts),
  [`useSidebarReorder`](../web/src/composables/useSidebarReorder.ts),
  [`useSortableDrag`](../web/src/composables/useSortableDrag.ts) and
  [`useChannelLayout`](../web/src/composables/useChannelLayout.ts) (drag and
  drop with immediate save and undo), [`useNavMenus`](../web/src/composables/useNavMenus.ts)
  (context menus), [`useMessageActions`](../web/src/composables/useMessageActions.ts),
  [`useMessageKeyboard`](../web/src/composables/useMessageKeyboard.ts),
  [`useResizable`](../web/src/composables/useResizable.ts), [`useDialog`](../web/src/composables/useDialog.ts).
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
  [`api.ts`](../web/src/lib/api.ts) (fetch wrapper), [`markdown.ts`](../web/src/lib/markdown.ts) (Discord-style Markdown: parser, renderer and the `registerTextRule` extension point in [`markdown/`](../web/src/lib/markdown)),
  [`channelLayout.ts`](../web/src/lib/channelLayout.ts),
  [`voiceSession.ts`](../web/src/lib/voiceSession.ts) (rejoin within 30 s),
  [`noiseSuppressor.ts`](../web/src/lib/noiseSuppressor.ts),
  [`thirdParty.ts`](../web/src/lib/thirdParty.ts) (in-app license list).
- **i18n**: [`i18n/de.json`](../web/src/i18n/de.json) and
  [`i18n/en.json`](../web/src/i18n/en.json); tests fail on hard-coded UI text
  and unused keys.

## Key design decisions

- **One binary, three containers.** API, hub, SFU and web app ship together
  ([`web/web.go`](../web/web.go)); `docker compose` adds only PostgreSQL and
  SeaweedFS (optional profiles add `coturn` and the update sidecar). Pure Go (`CGO_ENABLED=0`), so images cross-compile for amd64 and
  arm64 without QEMU ([`Dockerfile`](../Dockerfile)).
- **Forward, never transcode.** The SFU only copies RTP
  ([`internal/sfu`](../internal/sfu/sfu.go)); quality is decided by the
  sender. Transport is DTLS-SRTP. Microphone, camera, screen video and
  screen-audio payloads are encrypted in the browser with MLS-derived keys,
  so the SFU cannot read them; an active or compromised server can still
  subvert group membership and serves the code, and metadata stays visible.
  Text and attachments stay readable by the server
  ([SECURITY.md](../SECURITY.md#security-model-summary)).
- **Media key lifecycle.** A new epoch (and so a new key) comes from every
  join, leave or kick, and otherwise every 30 minutes (+-2 min jitter) from a
  path commit by the committer. A member with no heartbeat or handshake for
  20 minutes is removed and must be let in again. Constants in
  [`bounds.ts`](../web/src/lib/media/bounds.ts), logic in
  [`group.ts`](../web/src/lib/media/group.ts), heartbeat format in
  [`heartbeat.ts`](../web/src/lib/media/heartbeat.ts). The e2e build
  (`MNEMA_E2E=1`) lets tests shorten them; release builds cannot.
- **Frame format and line binding.** Each sealed unit is `M3 | epoch | stream
  | counter | line | nonce | AES-GCM | Ed25519 signature`; the `line` byte
  (source in the high nibble, screen layer in the low one) is in the AAD and
  the signed bytes, and each receiving transform is armed with the source the
  SFU names for its line, so a unit moved to another line does not open
  ([`frame.ts`](../web/src/lib/media/frame.ts),
  [`mediaFrameWorker.ts`](../web/src/lib/media/mediaFrameWorker.ts)). Version 2
  (`M2`, no line) is still sent and accepted for mixed-version Talks: members
  sign a capability message
  ([`capability.ts`](../web/src/lib/media/capability.ts), `M` `C`, next to the
  heartbeat), a sender seals version 3 only in an epoch where every member
  advertised it (decided per epoch, never downgraded within one), and a
  receiver refuses version 2 in an epoch where every member has.
  Refusing version 2 outright is planned for a later release
  ([SECURITY.md](../SECURITY.md)).
- **Screen shares are opt-in per viewer** so an idle viewer costs no
  bandwidth ([`subscriptions.go`](../internal/sfu/subscriptions.go)).
- **Invite-only, one Owner.** No public registration; the Owner is seeded
  from `ADMIN_USERNAME` ([`admin_seed.go`](../internal/auth/admin_seed.go)) and
  holds every permission; roles below them are managed through
  [`internal/perm`](../internal/perm/perm.go) and enforced server-side
  ([roles-and-permissions.md](roles-and-permissions.md)).
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
  banner ([`appVersion.ts`](../web/src/stores/appVersion.ts)).
