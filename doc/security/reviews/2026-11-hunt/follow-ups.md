# Fix review: two follow-up changes (coordinator decisions)

Reviewer: independent, read-only. Baseline origin/main f58bc3fc6 (3.0.9). Each change is reviewed as its single commit, against the goal the coordinator stated.

| Change | Branch / commit | Verdict |
|---|---|---|
| A. Automod for event comments and event text | msch128/fix-event-comment-automod, 777e0acb3 | **MERGE** |
| B. One emoji with and without U+FE0F is one reaction | msch128/fix-reaction-emoji-forms, 63d00cff2 | **MERGE** |

There are no blockers and no should-fix items in either change. The NOTEs below are for the record. None of them needs to be fixed before the merge.

---

## A. 777e0acb3: automod screens event comments and the text of an event

### What was checked and how

- **Refusal shape.** The author's test compares the bodies of a comment refusal and a message refusal byte for byte, for a new text and for an edit. Both tests pass. I read `internal/automod/engine.go` `block`: the event path calls the same `Engine.Check` and the same `block`, so the status, the code and the text come from one place. The new `Edit` flag only switches the existing two sentences. The refusal names the rule, exactly as a message refusal does, and nothing more.
- **Exemptions.** `Check` is unchanged: Owner and Administrator by the global snapshot, bots, exempt roles, and exempt channels by `Post.ChannelID`. The event path passes the event's Talk as the channel (`handler_validate.go` `screen`). Covered by the author's tests (owner, Administrator role, exempt role, exempt Talk for a comment). My own test adds: an event title in an exempt Talk is created (201), and a host whose role is exempt can create an event with a word that is blocked for others (201).
- **Event without a Talk (NULL channel).** `insertHit` no longer joins `channels`. Reverting only `hits.go` makes the author's queue test fail with "automod hit not recorded ... no rows in result set". Without the change the hit would be lost.
  - Readers I read: `reports_view.go` (`ChannelID *uuid.UUID`, `omitempty`; raid alerts already have NULL), `moderation/history.go` (does not select the channel), `account_export.go` (reporter rows only), `reportcount`.
  - In my test the admin queue (`GET /api/admin/reports`) lists the event hits with `channel_name` = the event's title and no `channel_id`, and still lists a message hit with `channel_id` and `channel_name: "general"`.
  - The web queue (`AdminReportsTab.vue:80`, `AdminReportDetail.vue:139`) renders `#channel_name` and does not use `channel_id` or `message_id` for links.
  - Existing rows: no migration and no rewrite. The new query returns the same name as the old join for every message hit.
- **Actions.** Block refuses, and flag commits after the store write (`Pending.Commit`). The timeout runs through `record`, as for a message. The author tests flag+timeout. My test checks that a block rule's timeout also applies after a blocked comment. Another member's comment edit still answers 403, and no hit is recorded (my test). The permission check (`requireWrite`, `requireManageEvents`) runs before the screen.
- **Event title, description and location.**
  - A member with Manage events is blocked. This is documented in `doc/moderation.md` ("*Manage events* does not exempt") and in `doc/events.md`.
  - My test uses an event created before the rule with the now-forbidden word in its description. Editing its start time, its (clean) title and its status still answers 200: fields an edit leaves alone are not judged.
  - The web edit dialog sends only changed fields (`web/src/events/lib/eventPayload.ts` `changedFields`), so the UI is not locked.
- **Performance.** One `Check` per comment or event write, with cached rules and a cached snapshot. A comment edit adds one `GetComment` read (NOTE A4).
- **Scope sentence in doc/moderation.md.** Each listed item, verified by route call or by reading:
  - forum post titles: `post.go` `checkAutomod` (`nm.Title`) and their edits (`forum_posts.go` `ScreenText`)
  - tags, name and emoji: `handler_forum_tags.go` create and update
  - guidelines: `handler_forum_extras.go`
  - temporary Talk name: `talks_handler.go`
  - event comments, comment edits, title, description and location: by route, in the tests
  - polls: options through `ScreenText` in `poll/service.go`
  - "Not screened: profiles, wiki pages, event role names": no automod call in those handlers. The list is introduced with "such as" and is true as worded. Other unscreened text (custom status, role templates, reminders) falls under it.

### Runs

- `go test -p 1 -tags=integration ./internal/automod/... ./internal/communityevents/...`: ok.
- Full `go test -p 1 -tags=integration ./internal/server/`: ok (312 s).
- Revert check: with the non-test changes reverted, both new tests fail (201/200 where 400 is required). With only `hits.go` reverted, the queue-entry assertions fail.
- My review test (not committed): passes.
- Gates: gofmt, go vet with and without the integration tag, `make complexity-check`, `internal/repo` tests, `make openapi-check`, `make docs-check` and `check-english-only.mjs`, all green.
- No allowlist, limit or test was weakened. Files are within limits (`handler_validate.go` 193 lines). Commit type is `fix`, and the commit carries no attribution lines.

### Findings

- **NOTE A1, refusal wording for a new event.** Creating an event with a blocked word answers "Your message was not sent: ...". The wording is byte-identical to a message refusal, as required, and matches the existing precedent for tags and Talk names. For an event, though, the sentence reads oddly. (`internal/automod/engine.go` `block`)
- **NOTE A2, the event title appears as a channel in the queue.** The queue shows such an entry as `#<event title>`, with the channel's hash sign. `doc/moderation.md` says the entry names the event's title as the place, so this is documented, but a moderator may take it for a channel. (`web/src/admin/components/AdminReportsTab.vue:80`, `AdminReportDetail.vue:139`)
- **NOTE A3, a full-object PATCH re-judges unchanged text.** An API client that sends unchanged text again (a full-object PATCH) is judged again. My test: re-sending the old description answers 400. The doc says "an edit that sets them", so this is accurate. The web client is not affected.
- **NOTE A4, screening comes before the store's own refusals.** A blocked comment on a cancelled event, or past the 500/50 comment caps, records a hit (and a timeout) and answers 400 rather than 409. A comment edit also costs one extra `GetComment` read even when no rule exists. Both are minor. (`internal/communityevents/handler_validate.go` `screenComment`)
- **NOTE A5, links and mentions rules now cover event text.** A "no links" rule now also blocks a link in an event's description or location (for example a meeting link). This follows from "same rules as a message" and is intended by the decision.

### Not checked

- The web queue in a browser (read only).
- Series edits with `scope=following` and a blocked word (read only: one screen, and `commitAfter` wraps `updateFollowing`).

---

## B. 63d00cff2: one emoji with and without U+FE0F is one reaction

### What was checked and how

- **No two different emoji collapse.** This was recomputed independently in Go against the Unicode files, not against the author's vectors.
  - Unicode 17.0 `emoji-test.txt` (5,225 entries): 3,953 distinct keys among the fully-qualified emoji and components, with 0 collisions.
  - Every minimally-qualified and unqualified form gets the same `ReactionForm` as its fully-qualified emoji.
  - `ReactionForm` leaves every fully-qualified emoji unchanged.
  - Every form passes `ValidateEmoji`.
  - The same holds for 16.0 (3,790 keys, 0 collisions).
  - The picker data (emoji-picker-element-data 1.8.0) has 3,953 entries with skins, 3,953 distinct raw strings and 3,953 distinct keys, so the author's figure holds.
  - The 207-code-point table equals Emoji=Yes minus Emoji_Presentation minus the keycap bases, exactly, in both emoji-data 16.0 and 17.0.
  - Single cases I checked:
    - skin tones stay apart (👍 vs 👍🏽, also with a stray U+FE0F before the tone);
    - ZWJ families stay intact;
    - tag sequences (Scotland) stay intact;
    - flags and single regional indicators stay intact;
    - `#⃣` and `#️⃣` become one emoji, while `#` stays apart;
    - ❤︎ (U+FE0E) stays apart from ❤️;
    - components alone (skin tone, ZWJ, U+20E3, U+E007F) are unchanged.
- **Custom emoji tokens are untouched.** Checked in the Go and TS tests, and by reading: `emojitext.IsToken` and `parseEmojiToken` return early.
- **Existing rows in both forms, on a real database through the routes.**
  - In the author's tests and mine: adding again does not error and does not change the rows, removing removes both rows, and one pill is shown with each member once.
  - My test runs on the member route with two members, one holding the bare heart and one the qualified one: one pill, count 2.
  - Per-member limit by key on the member route: 18 others plus the heart in both forms allows the 20th key and refuses the 21st with 409. Taking the heart back at the limit works.
  - Per-message limit by key across three members: the 50th key fits and the bot's 51st is refused with 409. At the limit, the bot can take back and re-add an existing key in the other form.
  - Bot PUT and DELETE are covered by the author's tests and by mine.
- **Old client (3.0.9 web).** `ReactionBar.vue` highlights a pill by `users.includes(me)`, and `message_reaction` replaces the list. My test simulates it on the member route with the websocket:
  - the client sends the bare heart and both rows are taken back;
  - the event and the response carry the qualified form;
  - clicking the qualified pill toggles it;
  - the picker's 👍️ is stored as 👍.

  The old client's highlight and toggle therefore work. The bot gateway's `MESSAGE_REACTIONS_UPDATE` is built from the same `ReactionResult` (read, not run).
- **SQL and indexes.** I ran EXPLAIN ANALYZE on Postgres 18 with 200,000 rows: 100,000 on one message (5,000 members × 20 emoji) and 100,000 over 10,000 other messages. The `(message_id, user_id, emoji)` unique index is still used on its `(message_id, user_id)` prefix.

  | Query | Old | New |
  |---|---|---|
  | Presence | 0.03 ms (index-only scan) | 0.06 ms (index-only scan) |
  | Delete | not measured | 0.9 ms (bitmap scan on the user index) |
  | Per-message count | 31.5 ms | 25.9 ms (seq scan in both, a pathological message) |
  | Summary for 50 messages incl. the hot one | 62 ms | 72 ms (index scan on `idx_reactions_message`) |

  The expression defeats only the third column of the unique index, and the rows it filters are at most 20 per member. There is no measurable cost on realistic messages.
- **Export lists raw rows.** Acceptable: both rows are the member's own data.
- **Updated SQL text match** (`write_rollback_integration_test.go`). Only the predicate string changed; the case and its assertion are unchanged. The new text matches only the count query. If it matched nothing, the case would fail, and it passes.
- **Shared vectors.** Running `REACTION_FORMS_WRITE=1` regenerates `testdata/reaction_forms.json` byte for byte, and the Go test agrees without regenerating.

### Runs

- Full vitest, 2 workers: 12,060 passed, 59 skipped (pre-existing skips plus the env-gated writer). `test:tooling` passed.
- ESLint on the touched files, and vue-tsc on `tsconfig.app.json` and `tsconfig.tests.json`: green.
- `go test -p 1 -tags=integration ./internal/chat/ ./internal/chat/core/ ./internal/chat/messages/ ./internal/chat/botapi/`: ok.
- Full `go test -p 1 -tags=integration ./internal/server/`: ok (259 s).
- Revert check: with `internal/chat/messages` reverted, both new server tests and my test fail (two pills for 👍/👍️, two pills for the hearts, a row left after taking back).
- Gates: gofmt, go vet with and without the integration tag, `make complexity-check`, `internal/repo` tests, `make openapi-check`, `make contracts-check`, `make docs-check` and English-only, all green.
- No allowlist, limit or test was weakened. Commit type is `fix`, and the commit carries no attribution lines.

### Findings

- **NOTE B1, the text-presentation form stays a separate reaction.** ❤︎ (with U+FE0E) and ❤️ remain two reactions. This is conservative: it never joins two emoji, and U+FE0E asks for a different look.
- **NOTE B2, a lone U+FE0F row cannot be taken back in the UI.** A row stored before the change whose emoji is a lone U+FE0F (the old `ValidateEmoji` accepted it: U+FE0F is Mn, not Cf) would show as a pill with an empty emoji. That pill cannot be taken back from the UI, because the client sends "" and validation refuses it. This needs a deliberately odd earlier request and is theoretical. (`internal/chat/messages/reaction_key.go` `oneReaction`)
- **NOTE B3, long non-emoji strings are now refused.** Non-emoji text made of nine or more text-default characters (for example nine copyright signs) grows past 16 runes in its qualified form and is now refused on add (400). No real emoji is affected: every Unicode 17 form passes.
- **NOTE B4, bots get the canonical form back.** A bot sees reactions in the canonical form, which can differ from what it sent (👍️ comes back as 👍). `api/openapi.json` says so for PUT ("stored in its fully-qualified Unicode form"). The table row in `doc/bots.md` says only that both forms are one reaction. This is a value normalisation, not a change of shape.
- **NOTE B5, the table needs a new emoji release to change it by hand.** The table is frozen at Unicode 16.0, which is identical for 17.0. A future emoji release that adds text-default characters needs a manual update. The vitest property test catches only entries that are wrong, not entries that are missing.

### Not checked

- A live bot gateway connection (read only).
- The desktop client.
- Rendering in a browser.

---

## Method notes

- One heavy job at a time, `nice -n 10`, `-p 1`, at most 2 vitest workers. No port-exhaustion reruns were needed.
- My review tests and the Unicode cross-check were temporary files in throwaway worktrees and were not committed. The worktrees are removed.
- The Unicode data files (emoji-test.txt, emoji-data.txt 16.0 and 17.0) were taken from unicode.org.
