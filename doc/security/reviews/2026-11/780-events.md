# Review of PR #780: typed events (9 commits) and message service (7 commits)

Reviewed `0bb1ec342..e2862abf6` (typed events) and `444ac0312..ec2e1b3f9` (message service) on
`origin/msch128/audit-train-3` at `4d16f933e`, read-only, in a scratch worktree. 2026-11-11.

Verdict: **no blocker.** The wire format is unchanged for every publish site, every typed consumer still
matches, and the four posting entry points keep their steps, transactions and nonce behaviour. One
should-fix (a web guard bug that already exists on main and that the new conformance check does not catch)
and six notes.

## Findings

### F1. should-fix (already on main, not caused by this PR): the web client drops `read_state {channel_id, refresh:true}`

- Server: `internal/chat/messages/post.go:249` (`wire.ReadStateChannelRefresh`, sent by `MarkReadAfterSend`
  to the author's other sessions after every post that moved the read marker). The golden pins it:
  `{"type":"read_state","payload":{"channel_id":"<id-1>","refresh":true}}`.
- Web: `web/src/types/events.ts:259` (`isRefreshReadState`) requires `last_read_at` whenever `channel_id`
  and `refresh` are both present, so `isServerEvent` returns false and `decodeServerEvent` throws
  `ContractError`; `web/src/stores/chat/socket.ts:176` logs "WS parse error" and drops the frame.
- Scenario: a member has two tabs open and posts in tab A. Tab B receives the refresh, rejects it, and
  keeps its stale unread count for that channel. `onReadState` (`web/src/stores/chat/events.ts:71`) already
  has the branch that would handle it (`fetchReadState`); the guard never lets it through.
- Why it belongs in this review: `web/src/types/eventConformance.ts:19` passes this shape, because
  `{channel_id; refresh: true}` is structurally assignable to the declared `{refresh: true}` member of
  `ReadStateUpdate`. The check compares types only; it says nothing about what the runtime guards accept.
- Checked by running `isServerEvent` under vitest in the scratch worktree: `channelRefresh: false`, the
  other three `read_state` shapes `true`.
- Suggested fix: accept `{channel_id, refresh: true}` in `isReadStateUpdate`
  (`onlyFields(['channel_id','refresh'])`) and add it to `ReadStateUpdate` (`events.ts:34`). To close the
  gap for good, add a vitest that feeds every frame of the Go golden files
  (`internal/server/testdata/event_wire_*.golden`, `internal/ws/testdata/event_wire_*.golden`) through
  `decodeServerEvent`.

### F2. note: built-in slash commands check the channel before the content now

- `internal/slashcmd/slashcmd.go:69`. Old order: permission gate, `ValidateContent`, `ChannelForMessages`,
  `ValidateParent`. New order (`Service.prepare`): gate, `ChannelForMessages`, attachments, content, target.
- Only which error wins changes, and only when both fail: content too long or empty and the channel
  deleted between the gate and the lookup. No side effect sits between the two checks.
- No fix needed; say so in the PR text if "no behaviour change" is quoted.

### F3. note: the bot API reads the body before the permission check

- `internal/chat/botapi/botapi.go:236`. Old: `CheckBotSend` (permission, channel), then decode. New: decode,
  then `Post`; on a decode error `Posts.Refusal` runs first, so the answer is the same in every combination
  I traced (no permission + bad body gives 403, unknown channel + bad body gives 404).
- Difference: a bot without Send messages now makes the server read its body (up to `DefaultMaxBody`)
  before the 403, and a bad body costs two permission lookups. The per-bot and per-channel limiters sit in
  front as before.

### F4. note: bot API, slash commands and scheduled sends publish the message loaded inside the transaction

- `internal/chat/messages/service.go:161`. When a webhook of the channel subscribes to `message_create`,
  `insertMessage` returns the message it loaded for the outbox row and `Post` publishes that one. The REST
  route already did this; the other three re-read after commit (`PublishMessage`, `GetMessage`).
- Same JSON for a new message (nothing else can have touched it); the golden for `/shrug` and the scheduled
  send did not change apart from the publishing function's name.

### F5. note: the message service is handed its automod engine instead of looking it up per post

- `internal/server/router_build_services.go:43-45`, `internal/chat/handler.go:99`. Same engine object in
  production; `automod.New` only builds a struct, so creating it earlier in the build changes nothing.
- Differences: after `automod.Detach` (shutdown) the four paths still screen; and a `chat.Handler` built
  without `Automod` skips screening on these four paths even if an engine is attached to the pool.
  Production has one construction site and it passes the engine.

### F6. note: the typed consumers match one exact kind each, and a pointer would slip past silently

- `internal/ws/broadcast.go:47` asserts `auth.UserUpdate` (value); `internal/gateway/observe.go:137` asserts
  `auth.MemberJoined` (value). Both types have value receivers, so `&auth.UserUpdate{...}` is also an
  `events.Event`, would be delivered to clients, and would skip the hub's profile cache update (stale name
  or avatar in `voice_snapshot`) or the bot gateway's `GUILD_MEMBER_ADD`.
- No such publisher exists today (all seven `user_update` sites and the one `member_joined` site convert by
  value). Suggested: a registry test that fails on a pointer publish of these two, or match both forms.

### F7. note: a message that was stored but could not be reloaded now surfaces as `*StoredError`

- `internal/chat/messages/service.go:162`. Status is unchanged (`httpx` unwraps with `errors.As`); only the
  logged text gains "load stored message:". The scheduled sender treats it as sent without the "sent"
  notice, exactly as the old `announceSent` did.

## Clean points and how they were checked

### (1) Wire format: clean

- Read the whole production diff of the nine commits (109 Go files, every `Broadcast*`, `SendToUsers`,
  `SendEvent`, `emitApplicationEvent`, `sendToVoiceRoom*`, `sendToVoiceUser`, `pacer.send`, `RecordEvent`).
- Scratch test in `internal/ws` (deleted afterwards) marshalled the old map or struct literal and the new
  type through the old and the new encoder for 60 cases and compared bytes: all equal. It covered every
  payload that was a map before, golden-covered or not, including the variants the goldens do not drive:
  `PinRemoved`, `UserDisabled` true/false, `MemberTimeout` set/null, `AutomodTimeoutNotice`, both deletion
  notices, all four `read_state` shapes, `TalkLeader` set/null, `MemberRolesChanged` nil/filled,
  `VoiceKicked` with and without reason, `ScreenLayersWanted` with and without source, bot presence with and
  without activity, `StageHand` up/down, `VoiceRooms`, `VoiceJoin`/`VoiceLeave`, `WebrtcOffer`,
  `WebrtcCandidate` (set and nil pointer), the four `null` signals, `Pong` (large and zero values),
  `UserStats` (int64).
- Omitted versus empty: the only `omitempty` that replaced an always-present key is `VoiceKicked.reason`.
  Every site that used to send the key unconditionally passes a value that cannot be empty
  (`joinRefusal`, `lobbyRefusal`, the two kick constants, string literals); the one site that left it out
  (`kickClientPeer`) still does.
- Types that were structs before only gained `EventName`; the renamed domain types (`auth.UserUpdate`,
  `auth.MemberJoined`, `core.MessageCreate/Update`, `dm.DMCreated/MemberAdded/MemberRemoved`,
  `communityevents.EventUpdate`, `ws.WebrtcOffer/Candidate`) keep their JSON because none of the underlying
  types, in the repo or in pion, has a `MarshalJSON` (grepped at base and head; pion's is on the field type
  `SDPType`, which survives).
- `bot.InteractionResponse` was compared by reading: keys in alphabetical order, same value types,
  `embeds` nil stays `null`.
- No golden file changed between the pinning commit and the last events commit. In the message-service
  commits only two `@ site` lines changed (`publishCreated` instead of `slashcmd.post` and
  `later.announceSent`); payloads are byte-identical.
- Event types no golden drives (all verified as above): `dm_call_end`, `dm_member_added`,
  `dm_member_removed`, `event_delete`, `event_reminder`, `event_signup_notice`, `interaction_response`,
  `media_handshake`, `permissions_changed`, `system_update`, `talk_leader`, `talk_moved`, `user_stats`,
  `webrtc_offer`, `webrtc_candidate`.

### (2) Consumers: clean (see F6 for the latent trap)

- Hub (`broadcast.go:47`): old `user_update` + `auth.User`/`*auth.User`; all publishers now send
  `auth.UserUpdate` by value. The short form `wire.UserDisabled` was a map before and was skipped then too.
- Push (`push/service.go:97`), thread follow sync (`thread_follow_sync.go:53`), webhook `translate`
  (`dispatch_delivery.go:27`): old `core.Message`/`*core.Message` under `message_create`/`message_update`;
  the only types with those names are `*core.MessageCreate` and `*core.MessageUpdate` (pointer receivers,
  so a value cannot be published). Webhook-authored creates are still not re-delivered. `message_delete`,
  `message_reaction` and `voice_state_update` are matched by name as before.
- Gateway (`observe.go`): channel events marshal the event itself (same bytes); `memberOf` copies the same
  four fields the old JSON round trip produced (`avatar_url` is `omitempty` on both sides).
- Nil pointers that would have panicked in the old push and webhook code are now ignored.

### (3) Message service: clean apart from F2 to F5

Steps in order, old and new (identical unless marked):

| Step | REST post | Bot API | Slash command | Scheduled send |
|---|---|---|---|---|
| Rate limit (middleware) | unchanged | unchanged | unchanged | n/a |
| Nonce replay (View gate, lookup) | same | none | none | none |
| Permission gate | Send (+Attach), `Deny` | Send, `CheckChannel` | Send, `Deny` | active account, then Send, `Deny` |
| Channel exists | same | same | **moved before content (F2)** | same |
| Attachment IDs | same | n/a | n/a | n/a |
| Content | `ValidateContent` | `ValidateBotMessage`; **decode moved before the gate (F3)** | `ValidateContent` | as stored, unchecked |
| Parent / reply target | same | same | parent only, same | same |
| Mention policy | member, same snapshot | bot + allowed_mentions | member | member |
| Automod | before the transaction, same (F5) | same | same | same |
| In transaction | slow mode / lock / DM rules, insert, nonce row, mentions, thread follow, claim attachments, outbox row | same minus nonce and claim | same | same plus delete of the scheduled row |
| Publish `message_create` | after commit, once | after commit, once (F4) | once (F4) | once (F4) |
| Mark read | yes | yes | yes | no (`KeepReadMarker`) |
| Response / follow-up | 201, or 200 on replay | 201 | message id | `scheduled_message_update` "sent" |

- Outbox: all four go through `insertMessage`, which records the event last in the same `pgx.BeginFunc`
  as the insert, after the caller's step. Nothing is published before `BeginFunc` returns.
- Double publish: the three callers lost their own `BroadcastChannel`; `Post.announce` is the only one.
- Duplicate nonce: found before the Send gate, it answers 200 with the first message and publishes nothing;
  another channel gives 400, a deleted message 410. A concurrent duplicate fails the primary key, the whole
  transaction rolls back (message, slow-mode stamp, outbox row) and the replay answers; if the row vanished
  meanwhile the answer is an empty 200, as before.
- Slow-mode exemption, new-member wait and mention policy are now values instead of context keys; nothing
  in the insert path reads the user from the context (grepped automod, mentions, audit, outbox).
- Build order: `newChat` runs inside `newCore`, before `newBots` and `newLater` call `chatH.Posts()`; the
  fields `Posts()` copies (`DB`, `Perms`, `Events`, `Online`, `Automod`) are final by then.

### (4) Generated web shapes and guards: one real mismatch (F1), otherwise clean

- The event names of `ServerEventShapes` and `ServerEventPayloads` are the same 65; nothing is skipped by
  the intersection. `presence_snapshot` and `webrtc_offer` are excluded on purpose.
- Ran the runtime guards against the server's real shapes: automod timeout notice, lowered stage hand,
  `PinRemoved`, `community_updated` with empty icon, `interaction_response` with `embeds: null` are
  accepted. `member_roles_changed` with `role_ids: null` and `webrtc_candidate` with a `null` payload would
  be rejected, but the server sends neither (`SetSelfRole` always returns a non-nil slice when something
  changed; `room_attach.go:46` skips nil candidates).
- Not run: `vue-tsc` on the web app (CI does).

### (5) Commands, all on `4d16f933e`

- `go build ./...`: ok
- `go vet ./...` and `go vet -tags=integration ./...`: ok
- `make staticcheck` (v0.8.2, both modules): ok
- `go test -p 2 ./internal/events/... ./internal/chat/... ./internal/webhook/... ./internal/push/...`: ok
- `go test -tags=integration -run 'Wire|Event|Post|Bot|Slash|Scheduled|Webhook' ./internal/server/`:
  ok, 139 tests, 26 s
- Extra: `go test -tags=integration ./internal/chat/messages/ ./internal/later/ ./internal/slashcmd/
  ./internal/webhook/ ./internal/events/...` ok; `go test -run 'Wire|Golden' ./internal/ws/` ok
