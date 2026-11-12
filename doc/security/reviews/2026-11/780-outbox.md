# Review of PR #780 (msch128/audit-train-3): outbox, health, connection id

Reviewed at branch head 4d16f933e against origin/main, restricted to
13a8eea65 (outbox), 4326855b6 (health), 8580061fc and dda7f2bd9 (connection id).
Read-only: nothing edited, committed or pushed. The scratch worktree is removed.

**Verdict: no blocker. 3 should-fix (all small), 9 notes.**

## Tests run (all green)

- Unit: `go test -p 2 ./internal/webhook ./internal/httpx ./internal/ws ./internal/safego ./internal/community`,
  `./internal/server -run 'Outbox|Webhook|Health|Conn'`, `./internal/sfu -run 'Conn|ViewerGuard|PeerWithout|PeersOf'`.
- Integration (`-tags=integration -count=1`, Postgres 18 via Docker): `./internal/webhook ./internal/httpx ./internal/ws`,
  and `./internal/server -run 'Outbox|Webhook|Health|Conn'` plus `-run 'Outgoing|Liveness'`. The second filter was needed:
  the outbox tests in the server package are named `TestOutgoingDelivers...` and the liveness test `TestLiveness...`,
  so the filter in the task does not select them.
- Query plans: `EXPLAIN (ANALYZE, BUFFERS)` on a scratch Postgres 18 with the two tables' DDL from migrations
  0019/0023, 20 webhooks and 8000 queue rows.

## Should-fix

### S1. A sweep or bulk delete writes one INSERT per deleted message inside the transaction
`internal/chat/channels/auto_delete.go:197-203`, `internal/chat/messages/moderation.go:152-156`

Scenario: a channel with auto-delete and a webhook subscribed to `MESSAGE_DELETE` has a backlog. One sweep batch is
200 messages (`autoDeleteBatch`), up to 25 batches per run: 5000 single-row round trips, each batch's 200 inside the
transaction that holds `FOR UPDATE` on the 200 messages. One second later the dispatcher's cap deletes all but the
newest 500 again. Before this commit the same sweep cost no database write (memory buffer, capped at 250 per channel).
Bulk delete has the same shape with at most 100.

Fix: one statement per batch, e.g. a `RecordMessageDeletes(ctx, p, tx, channel, []deleted)` that builds the payloads
in Go and inserts with `SELECT w.id, ... FROM outgoing_webhooks w CROSS JOIN unnest($ids, $payloads)`.

### S2. The liveness probe writes an access-log line per call
`internal/httpx/middleware.go:136`

`Logger` skips only `r.URL.Path == "/api/health"`. `doc/deployment.md:239-241` recommends
`/api/health/live` every 30 s for Kubernetes; Swarm and autoheal probe more often. Each probe is one `http` info line.

Fix: skip `/api/health/live` in the same condition.

### S3. A dependency name the web app does not know hides the feature list again
`web/src/types/rest.ts:4991` (generated from `internal/server/health.go:37`, `enums:"database,storage"`),
used by `web/src/stores/serverFeatures.ts:11-13`

Scenario: a later server adds a third name to `unavailable`. A web app or desktop bundle of this release then fails
`isServerHealthUnavailable`, falls through to `decodeServerHealth`, which throws, and the app starts without a
feature list: the bug this commit fixes, for old clients on newer servers.

Fix: accept any string in `unavailable` on the client (drop the enum from the Go tag or loosen the guard by hand),
or state in `doc/api-stability.md` that the list is closed.

## Notes

### N1. Claim order is transaction start, not commit; the doc and a test name say otherwise
`internal/webhook/queue.go:115` (`ORDER BY next_attempt_at, seq`), `doc/webhooks.md:267-269`,
`internal/webhook/outbox_integration_test.go:165`

`next_attempt_at` defaults to `NOW()`, which is the start of the writing transaction. Scenario, only while rows are
backed up: edit E1 begins, edit E2 of the same message begins later but takes the row lock first and commits, then
E1 commits (final text is E1's). The queue holds E2 (seq 1) and E1 (seq 2), yet E1 is claimed first because it began
earlier, so the receiver ends on E2's text. The `timestamp` field is right (taken at `Record`), so the doc's advice to
sort by it still holds, and four workers deliver in parallel anyway.
Fix: either set `next_attempt_at` explicitly to `clock_timestamp()` in `Record`, or reword the doc sentence.

### N2. Two owners of the caps can deadlock
`internal/webhook/queue.go:63-77` (`storeEvents`, voice path) and `queue.go:88-104` (`capQueue`, new)

Both delete the oldest rows in multi-row statements and now run on two goroutines. With several webhooks over 500
and the table over 5000 at once, `storeEvents` can hold one webhook's rows and wait in its global delete for rows
`capQueue` holds, which waits for the first. Postgres aborts one after a second: either the caps are retried on the
next tick, or a batch of up to 100 `VOICE_STATE_UPDATE` events is dropped as `store_error`. Not reproduced; reasoned
from the statements.
Fix: `storeEvents` only inserts and sets `capsDue`; `applyCaps` is the single owner.

### N3. The caps run on the ticker goroutine without a timeout
`internal/webhook/dispatch.go:210-212`, `dispatch_outbox.go:85`

A database that hangs stalls the reload, poll and prune ticks behind `applyCaps`. Fix: a `context.WithTimeout` of a
few seconds.

### N4. A send now fails when the queue insert fails
`internal/webhook/dispatch_outbox.go:60-67`

Intended and tested (`TestAMessageWhoseEventCannotBeWrittenIsNotStored`), and only for channels that have a
matching webhook. Worth knowing: chat in such a channel depends on `outgoing_queue` being writable. The entry points
that publish through `PublishMessage` (uploads, forwards, forum posts, polls, bot interaction replies) load the
message twice, once in the transaction and once for the reply; only `Service.Post` reuses the first load.

### N5. Long outages (unchanged behaviour)
`internal/webhook/dispatch.go:232`, `queue.go:164-171`, `queue.go:109-126`

`Run` prunes rows older than 2 hours before the workers start, so a server that was down longer delivers nothing of
what the outbox kept. A dead receiver with 500 rows holds the head of the queue: 10 s timeout, 4 workers, about
21 minutes before another webhook's newer rows are reached. Both predate this PR; the outbox makes the first one
more visible because the doc now promises the event is stored.

### N6. The web app notices an outage only at start
`web/src/stores/serverFeatures.ts:76-101`

The recheck runs only while `unavailable` is non-empty, so storage that fails after a healthy start shows no notice
until a reload. The recheck request has no timeout: a request that never settles leaves `pending` set and the notice
up until a reload. Fix for the second: `signal: AbortSignal.timeout(10000)` on the health request.

### N7. A few per-peer lines still carry no conn_id
`internal/sfu/room_attach.go:232`, `room_attach.go:278`, `internal/sfu/room_signal.go:336`, `room_signal.go:340`
(these four also lack `user`), and the panic sites `internal/sfu/sfu_join.go:321` (`sfu_peer_drop`),
`internal/sfu/nack_responder.go:217`, `internal/ws/voice_presence.go:117` (`ws_voice_grace`).

### N8. Account erasure does not match reaction events in the queue (not from this PR)
`internal/auth/account/account_erase_steps.go:12`

The step deletes queue rows by `data->>'user_id'` and `reply_to.user_id`. A queued `MESSAGE_REACTIONS_UPDATE`
carries the member's id in `reactions[].users` and stays. Same before the outbox; listed because the queue is now the
only place these rows pass through.

### N9. Health can wait for the database for up to the cache window (unchanged)
`internal/server/health.go:196`

For up to 2 s after the database dies the cached result still says it is up, and `names.Get` queries with the
request's context and no timeout while holding the names lock. The name is cached for a minute, so this needs both
caches to expire together.

## Clean points and how each was checked

**Every write path has exactly one row, in the same transaction.** Grepped every `INSERT/UPDATE/DELETE` on
`messages` and `message_reactions` and every construction of the four event types, then matched publish sites to
record sites:

| Path | Writes through | Record site |
| --- | --- | --- |
| Member REST send, bot API send, slash commands, scheduled sends | `Service.Post` -> `insertMessage` | `messages/post.go:83` |
| Uploads, forwards, forum post create, polls, bot interaction replies, Talk invites, call notes | `InsertMessage` -> `insertMessage` | `messages/post.go:83` |
| Incoming webhook post | `InsertMessage` with `WebhookID` | none, as before (`post.go:80`, `dispatch_delivery.go:28`) |
| Edit (member and bot API) | `EditMessage` | `message_mutations.go:33` |
| Pin, unpin | `setPinned` | `pins.go:151`, only when the state changed |
| Forum post update | `UpdateForumPost` | `forum/forum_posts.go:146` |
| Delete (member, moderator, bot API) | `DeleteMessageAs` | `message_mutations.go:121` |
| Bulk delete | `bulkDelete` | `moderation.go:153` |
| Auto-delete sweep | `sweepBatch` | `channels/auto_delete.go:199` |
| Reactions (member toggle, bot set) | `changeReaction` | `message_mutations.go:230`, only when changed |

No path publishes one of the four events without a record, and none records twice. `Observe` returns early for the
four types (`dispatch.go:276-281`), so the publication cannot queue a second row. Paths that never published an
event still publish none: thread replies removed by a root's cascade, channel deletion (its queue rows go with the
webhook by `ON DELETE CASCADE`), account erasure. Discord import creates channels and roles only, no messages.
Poll votes publish their own event type, which webhooks never received.

**Rollback leaves nothing.** The row is written with the caller's `tx`; `TestAMessageThatIsNotStoredLeavesNoEvent`
passes, and the publication only happens after `BeginFunc` returned nil.

**Zero webhooks, disabled webhook.** `RecordEvent` asks the cached route list first (`core/outbox.go:47`); the list
holds enabled webhooks only (`dispatch.go:160`), so no SQL runs and no row is written. `Record` filters on
`w.enabled` and the event list again in SQL. Rows of a webhook switched off meanwhile are dropped at claim
(`dispatch_delivery.go:99-106`).

**Table growth and retention.** A delivered or finally failed row is deleted in the transaction that logs the
attempt (`queue.go:129-134`); the delivery log is trimmed per webhook in the same transaction. Bounds 500 per webhook
and 5000 in total, applied within a second of the first publication, and once at start.

**Query plans.** Claim: index scan on `outgoing_queue_due`, one row, 0.07 ms. Prune: `outgoing_queue_created`.
Purge: `outgoing_queue_webhook`. Record: one insert plus the foreign key check; the lock is taken above the sort, so
in id order. The two cap statements scan and sort the whole table: 3.6 ms and 3.9 ms at 8000 rows, at most once a
second and only while events flow. No index is missing.

**Lock contention on the insert.** `FOR KEY SHARE` on the webhook row does not conflict with the `FOR NO KEY UPDATE`
of the delivery bookkeeping (`outgoing_store.go:295`), so deliveries never hold up a send. It waits only for a
webhook or channel delete, which is short and ends with no row.

**Lost wake-up.** Workers claim before they wait (`dispatch_queue.go:115-125`), so a restart delivers at once; the
poll tick signals every second, so a publication that never happened costs at most 1 s plus the 20/s pacing.
`TestAMessageEventSurvivesACrashBetweenCommitAndDispatch` passes.

**Health.** `/api/health/live` is a constant body behind middleware that touches no dependency (request id, client
ip, metrics, logger, recover, security headers, CORS, CSRF). The 503 body adds only `status` and `unavailable` to
what the 200 says; version, name, icon and features are the same public values. `Cache-Control: no-store` is set for
every `/api/` path (`httpx/middleware.go:264`). The storage check is a cached flag (`cmd/server/storage.go`). The web
app keeps asking every 30 s while degraded, also when a recheck fails, and clears the notice on the first 200, so it
does not stay degraded after storage is back (apart from N6).

**Connection id.** It is used only by the two loggers, the safego panic line and `SFU.Join`; grepped
`connID|conn_id|ConnID` over `internal` and `cmd`: no event payload, no HTTP body, no metric label.
`TestConnectionIDIsInNoEvent` passes. The 101 response does not carry `X-Request-Id`: gorilla/websocket 1.5.3 writes
only its own headers and the `responseHeader` argument, which is nil (`server.go:219-230` of the library). A
client-chosen id never becomes the conn_id (`ws/client.go` `nameConnFor`), and is logged once after passing
`^[A-Za-z0-9._-]{1,128}$` (`httpx/middleware.go:27`), so no newline or control character reaches the log. The logger
is bound once per connection and per peer; the diff adds no log call and no allocation on the packet paths
(`guardedWriter.Write`, `forward`, `layerWriter`): the id is read only inside recover or error branches. Panic lines
carry it (`safego.go:119`, `TestPanicOfAConnectionsSiteNamesTheConnection`).
