# Security review: outgoing webhooks and conversations' call chats (PR #803, head 52442783c)

Reviewer: an agent that did not write the change. Read-only; own worktree at 52442783c, now removed; throwaway Docker only.
Commits reviewed: 8b74c7f13 (fix), 52442783c (fault tests name the new reload query).
The reviewer's revert script and logs stayed in a local scratch folder and are not published.

## Verdict

**SHIP.** The fix is correct and complete for every path I could find from a chat event to outgoing_queue.

Before the release, make one test change (finding 1). Each of the two guards stops the leak on its own, but no test fails when only one is removed, and the older test never fails at all.

A migration for leftover webhooks is not needed (question 2), but I recommend a cheap guard at delivery (finding 2).

## 1. Is the fix complete?

### Every path from an event to the queue

| Path | Events | Protection after the fix |
|---|---|---|
| Transactional: core.RecordEvents → Dispatcher.Wants (cached routes), then Dispatcher.Record (SQL insert into outgoing_queue) | message_create, message_update, message_delete, message_reaction. This includes edits, deletes by the author or a moderator, the expiry sweeper (removeLocked → RecordMessageDelete), bulk deletes and reactions. | Two guards. Reload's routes leave out private channels, so Wants is false. Record's insert joins `channels c ... AND c.type <> 'dm' AND c.dm_id IS NULL`. Either guard alone stops the row. |
| Hub tap: Hub.BroadcastChannel / scoped publish → Hub.Tap → Dispatcher.Observe | Non-transactional, translated events. Only voice_state_update is translated and not transactional; message_update sent by the hub (auto-delete file purge, the Owner's media delete) is of a transactional type, so Observe only wakes the workers. | Two guards. The hub sends a private channel's events to DMTap, never Tap (ws/perms.go:65-71, ws/event_scope.go:59-64, using perm.Snapshot.IsPrivate = IsDM or IsDMTalk). Observe's targets come from the Reload routes, which are now filtered. |
| Broadcast(ev) with channel uuid.Nil | Global events | Observe drops `channel == uuid.Nil`. |
| Delivery (process → loadActiveWebhook → getOutgoing) | Rows already in outgoing_queue | **No channel check** (finding 2). Rows written after the fix cannot exist for a private channel. |

### Which channels count as private

- A conversation is `type = 'dm'`.
- A call Talk is `dm_id IS NOT NULL`. The constraint `channels_dm_talk_is_temp` (migration 0079) ties dm_id to temporary voice Talks.
- talks.go:121 is the only insert that sets dm_id.
- Nothing ever sets dm_id to NULL: I grepped, and the only change is the ON DELETE CASCADE from the conversation.
- The permission snapshot derives dmTalks from `dm_id IS NOT NULL` (perm/snapshot_load.go:145).
- So a call Talk always has dm_id NOT NULL, and the SQL condition and the hub's IsPrivate describe the same set.

Other kinds are not private by kind:
- Community Talks with permission overwrites, forums and threads are channels an Administrator configures and can read. An outgoing webhook there is an admin decision, as the API allows.
- Thread replies carry their channel's id, so they follow the channel.

## 2. Leftover webhooks from older releases

- **Conversations:** the API refused them from the start. The guard came with direct messages in 1.1.0 (cd6cd53d2).
- **Call Talks:** they exist from 2.2.0 (migration 0079). The admin route only refused them from 2.9.17 (89472a6ca, `dm_id IS NULL` in handler.go:106). From 2.2.0 to 2.9.16, an Administrator could create an outgoing or incoming webhook on a live call Talk, if they knew its id.
- **What happens to such webhooks now:**
  - Both webhook tables reference `channels(id) ON DELETE CASCADE` (0018:14, 0019:11), and outgoing_queue cascades from outgoing_webhooks (0023).
  - A call Talk is temporary: it is removed when it empties, and TalkManager.sweepStale removes every `is_temp` channel at start (talks_live.go:188-215).
  - So any such webhook went with its Talk, at the latest at the restart of an upgrade. An instance running 3.0.13 cannot still hold one unless someone edited the database by hand, or a stale-Talk removal failed and kept being retried.
- **Recommendation:** no migration with an audit entry. It would find nothing on a normal instance, and an audit entry for "nothing" adds noise.
  - Make delivery itself refuse private channels (finding 2). That covers hand-edited rows, a failed sweep, and any future path.
  - For the owner's own instance, a read-only check is enough:
    ```
    SELECT w.id FROM outgoing_webhooks w JOIN channels c ON c.id = w.channel_id
    WHERE c.type = 'dm' OR c.dm_id IS NOT NULL
    ```
    and the same for webhooks.
  - If you want a record anyway: a startup log line, at WARN, when Reload's unfiltered count differs from the filtered one. Prefer that over a migration.

## 3. Do the tests fail without the fix?

I broke the fix in my worktree, ran both tests, and restored the files (revert5.txt):

| Code | TestAWebhookOfACallChatQueuesNothing (new) | TestAnOutgoingWebhookDoesNotHearACallChat (existing) |
|---|---|---|
| Head 52442783c | PASS | PASS |
| Both guards reverted (`git show 8b74c7f13 -- dispatch.go dispatch_outbox.go \| git apply -R`) | **FAIL** | PASS (it never catches the leak) |
| Only the Record join reverted | PASS | PASS |
| Only the Reload filter reverted | PASS | PASS |

### Finding 1 (LOW, test quality; fix before the release)

- **The existing test is ineffective.** TestAnOutgoingWebhookDoesNotHearACallChat (dm_talk_privacy_integration_test.go:178) passes on the leaking code. `rc.none` waits too briefly for the delivery, as the author's commit message says. Either delete it, or make it wait for the queue: count outgoing_queue rows after the post answered, as the new test does.
- **The new test catches only the loss of both guards.**
  - Its first post runs before Reload, but Wants still answers from the stale cache. So with the Record join removed, nothing calls Record, and the test passes.
  - After Reload, the Reload filter alone keeps Wants false.
- **Smallest fix: add two narrow checks.**
  - Call `Dispatcher.Record` directly in a transaction with a private channel's id and a webhook whose channel_id is that channel. Assert 0 rows; this fails without the join.
  - After `Reload`, assert `Wants(callTalk, "message_create")` is false; this fails without the filter.
  - Each guard then has a test that fails without it.

### Finding 2 (LOW, defence in depth)

- **Where:** delivery (dispatch_delivery.go: process → loadActiveWebhook → getOutgoing, outgoing_store.go:109-112) loads the webhook without its channel.
- **Effect:** a row already in outgoing_queue for a webhook whose channel is private would still be delivered. Today such a row can only come from before the fix, and the cascade removes it at restart.
- **Fix:** add `JOIN channels c ON c.id = w.channel_id AND publicChannel` to the delivery's load, or a check in loadActiveWebhook. Drop the row with a counted reason, e.g. "private_channel", like message_ended.

## 4. Gates

- go build ./...: exit 0
- go vet -tags=integration ./internal/webhook/ ./internal/server/: exit 0
- go test ./internal/webhook/ (unit): exit 0
- **internal/webhook integration suite**, -p 1: exit 0 (ok, 13.3 s)
- **Privacy-related internal/server integration tests**, -p 1, `-run 'Webhook|DMTalk|CallChat|CallTalk|Canary|Visibility|Private|Conversation|DM'`: exit 0, 91 tests passed, 0 failed. This includes the canary sweep, the call-Talk class tests, webhooks_private and dm_talk_privacy.

## Not checked

- The full internal/server suite at this head (only the privacy subset).
- The bot gateway's own path. It is fed by the same Hub.Tap, so private channels reach it only through DMTap, which goes to push and thread sync. I did not run a dedicated bot test beyond TestABotsCommandsDoNotReachACallChat in the subset.
- No production data was read.

## Outcome

Both findings were fixed in the same pull request before it merged (92a1e7be2): delivery refuses a queued row of a private channel and counts it as `private_channel` in `mnema_webhook_events_dropped_total`, Reload logs a warning when webhooks of private channels are ignored, and each of the three guards has its own test that fails when only that guard is removed. Released in 3.1.0.
