# Fix review H-L6-01 round 2, and phase 2 plus fix review H-L6-03

Reviewer: independent fix reviewer (wrote none of the code, tests or findings). Read-only: nothing in the repository changed, nothing pushed. All scratch tests and planted files were in the reviewer's worktree, never committed, and deleted before the full suite ran.

Inputs: reviews/fixreview-H-L6-01.md (round 1), findings/H-L6-03.md, branches msch128/fix-h-l6-01 (43696aa9e, a1fa2adf5 on 6e4164178) and msch128/fix-h-l6-03 (d9760e384 on a1fa2adf5).

## Summary

| Job | Verdict |
|---|---|
| A: H-L6-01 round 2 (43696aa9e test, a1fa2adf5 docs) | **MERGE**. SHOULD-FIX 1 and 2 and NOTE 6 of round 1 are resolved. Only NOTEs remain. |
| B1: H-L6-03 phase 2 | **STANDS** at S3. It reproduces on the base, is not documented or intended, is reachable with the seeded Moderator and Admin roles, and is not a duplicate of H-L6-01. |
| B2: H-L6-03 fix (d9760e384) | **MERGE AFTER FIXES**. No BLOCKER. One SHOULD-FIX: a removal from a conversation's call Talk is still read by the Owner and Administrators, against doc/talks.md and against the fix's own new OpenAPI sentence. |

## Job A: H-L6-01 round 2

### Scan still finds the same actions

A scratch dump test listed `newActionScan(...).found` and `errs` on the real tree at 6e4164178 and at a1fa2adf5. Both give 139 actions and 0 errors, and the sorted lists are byte-identical (`diff` empty).

### Planted writers (a throwaway package under internal/, removed afterwards), run with `go test -run TestEveryWrittenActionHasAClassOfItsOwn ./internal/audit` at a1fa2adf5

| Plant | Result |
|---|---|
| `import aud ".../internal/audit"`; `aud.Entry{Action: "secret.alias"}` | caught: "secret.alias ... has no row of its own" |
| `import . ".../internal/audit"`; `Entry{Action: "secret.dot"}` | caught |
| same dot import, `Entry{Action: "secret." + x}` | caught ("secret.*" has no row) |
| `"INSERT INTO audit_log (action, actor_name) VALUES ($1, $2)"` outside internal/audit/audit.go | caught: "an INSERT into audit_log whose action is not a literal" |
| `"insert into audit_log (action) values ('secret.lower')"` | caught: "secret.lower ... has no row" |
| `"insert into audit_log (action) values ($1)"` | caught ("not a literal") |
| `"INSERT INTO audit_log (action) " + "VALUES ($1)"` (split after the table name) | caught (fails closed) |

So the scan now fails for all four cases the round-1 review asked for. Round 1 showed these passed before the change, and the author's commit message shows the same.

Remaining gaps (NOTE). None of these exists in the tree today (grep):

- NOTE A1: the same package imported twice (`"…/audit"` and `au2 "…/audit"` in one file). `auditName` returns the first name only, so `au2.Entry{Action: "secret.twicealias"}` was missed. This is legal Go but unusual.
- NOTE A2: the table name written another way is missed: `public.audit_log`, `"audit_log"` (quoted identifier), or `"INSERT INTO " + "audit_log …"` (split before the table name).
- NOTE A3: `rawInsert` matches the raw source text `lit.Value`, not the unquoted string. So `"…\n\tVALUES ('secret.mixed')"` with an escaped tab before VALUES fails the new `\bvalues` and is reported as "not a literal". That fails closed, but the message is misleading. `strconv.Unquote` first would fix it.
- NOTE A4: the literal captured is the first value, not the `action` column's. `INSERT INTO audit_log (target_type, action) VALUES ('user', $1)` was reported as the action `user` (it failed, but for the wrong reason). If that first literal happened to be a classed action, the INSERT would pass. This predates the change.
- Round-1 NOTEs 1 to 4 (type alias, non-literal entries, calls through function values, name-only helper matching) are unchanged, as the commit says.

### Description sentence (a1fa2adf5, `listAuditLog`)

The new sentence is true: forum.* is Channel class, and the catch-all `%` row is VisPrivileged, which `restrictTo` hides for every reader who is not privileged. `make openapi-check` passes. NOTE A5: the lists stay illustrative, not complete. Channel class also covers `channel.*`, `poll.*`, `event.talk_*` and `moderation.bulk_delete`. The Owner-and-Administrators class also covers `category.*`, `layout.apply`, `bot.*`, `import.*`, `report.*` and `moderation.note_*`. "…depends on the entry's class (doc/moderation.md)" points the reader to the full table, so this is acceptable.

Gates for Job A: see the end (run on d9760e384, which contains a1fa2adf5). Commit types are `test:` and `docs:`, with no attribution lines.

## Job B1: H-L6-03 phase 2

Repro: the finding's command on a1fa2adf5, with the fix's test file checked out from d9760e384:

```
--- FAIL: TestTalkRemovalFromAPrivateTalkReachesOnlyAuditorsWhoSeeIt
    auditor reads the Talk's id: true, want false
    auditor reads the Talk removal of okapitalk: true, want false
    auditor reads the Talk removal of okapiold: true, want false
```

Attempts to defeat the claim:

- **Documented or intended?** No. doc/moderation.md classes `voice.*` as Channel class for "who was in which Talk". At the base, the class table's reason for `user.talk_kick` ("moderation of a member") fits the admin-wide kick, which names no Talk. Nothing documents that a per-Talk removal names the Talk to every auditor. The OpenAPI text at the base only said "Recorded in the audit log as user.talk_kick".
- **Reachable?** Yes, over REST, for any holder of View audit log. The per-Talk route is open to anyone with Disconnect members in the Talk.
- **Seeded roles.** Migration 0034 seeds Moderator (Manage messages, Disconnect members, View audit log, Timeout members; 0097 adds Manage reports and Kick members) and Admin (adds channel, role, invite and member management and Ban). Neither gets Administrator. A Talk with a role overwrite that denies View channel to @everyone is therefore invisible to both, and both read the audit log. A scratch test with a role holding the seeded Moderator bits behaved exactly like the plain auditor: it read every per-Talk removal of the private Talk at the base.
- **Duplicate of H-L6-01?** No. H-L6-01 is about forum.* entries. This one is a different action and writer, and it concerns presence in a Talk. Round 1 raised it as a separate SHOULD-FIX.
- **Lower severity?** The reader needs an admin-area right, and only the Talk's id leaks, not its name. The role-based reach (both seeded staff roles), the named member, the named moderator, and the bypass of the documented rule keep it at S3. S4 is arguable, but the same fact is hidden for voice.join and voice.leave, so the inconsistency is real.

**Verdict: STANDS (S3).**

## Job B2: H-L6-03 fix review (d9760e384)

### Cause removed

The per-Talk writer (internal/auth/memberadmin/users_admin_talk.go:108) now writes `voice.disconnect` with `ChannelID` set. `voice.%` is Channel class. Old rows of the old shape are classed at read time by `legacyTalkRemoval` (internal/audit/filter.go:115-120), a WHEN placed ahead of every table row.

History check (`git log -S talk_kick`): the only writer that ever put `channel_id` at the top level of a `user.talk_kick` entry's details is the per-Talk writer, added in f3d9e0ba7 (#472) with exactly this shape. The admin-wide entries come from the middleware. In internal/audit/middleware.go the top-level keys are only `path` (unknown routes), `params`, `request`, `request_unparsed`/`request_truncated`, `before`, `after` and `changes`. A request body that carries `channel_id` sits under `request`. So the discriminator is sound.

### Every reader, every row shape, paging and group filters (scratch integration test at d9760e384)

Readers: the Owner, an Administrator (role with Administrator), an auditor with Staff (sees the private Talk), a plain auditor (View audit log only), and a member with the seeded Moderator bits. A plain member got 403.

Rows stored directly (user.talk_kick unless named), with what each reader saw:

| Row | Owner / Admin | Staff auditor | Auditor / Moderator |
|---|---|---|---|
| legacy, private Talk id | shown | shown | hidden |
| legacy, same id in upper case | shown | shown | hidden |
| legacy, id in braces `{…}` | shown | hidden | hidden |
| legacy, `"not-a-uuid"`, `""`, number 5, object, quote/semicolon text | shown | hidden | hidden |
| legacy, nil uuid | shown | hidden | hidden |
| legacy, JSON null (`->>` gives NULL, so it falls to the table) | shown | shown | shown |
| legacy, open Talk | shown | shown | shown |
| legacy, Talk made private after the row was written | shown | shown | hidden |
| legacy, Talk deleted afterwards (via the API) | shown | hidden | hidden |
| admin-wide shape (`params`, `request.channel_id`, `before`) and `{}` | shown | shown | shown |
| voice.disconnect, private Talk (ChannelID set) | shown | shown | hidden |
| voice.disconnect, open Talk | shown | shown | shown |
| voice.disconnect without ChannelID | shown | hidden | hidden |
| user.disable with a top-level channel_id (not the legacy action) | shown | shown | shown |

Every cell matched the expected rule. No malformed value failed the listing: no 500, since the inner CASE keeps the cast from running. Paging with `limit=2` and the `before` cursor gave exactly the same rows in the same order as one `limit=200` page, for every reader. The group filters `action=user` and `action=voice` gave exactly the visible rows of that prefix for every reader. Unit test `TestRestrictToComparesTheColumnAsUUID` still passes.

### SQL and the Go class function

`ClassOf` stays per action, so for a legacy row it says Auditors while the SQL says Channel. This is by design and documented at internal/audit/classes.go:32-35. `ClassOf` has no production caller (grep: only tests, internal/server/audit_hardening_integration_test.go:73 and visibility_audit_forum_integration_test.go), and no Go-side filtering of audit rows exists (no live feed, webhook or export of audit rows by class). So the two do not diverge for any reader today. NOTE B1: the comment on `restrictTo` still says "the same rule as ClassOf". A future Go consumer would need a row-level mirror (action plus details) with a test that pins it to the SQL.

### SHOULD-FIX B1: a conversation's call Talk

The author did not check this case. The per-Talk route also works in a conversation's call Talk. A member of the conversation who holds Disconnect members server-wide (a role, for example the seeded Moderator) removed the other member: 204. The resulting `voice.disconnect` row carries the call Talk's id as ChannelID. Scratch test results:

| Reader (not a member of the conversation) | at a1fa2adf5 (base) | at d9760e384 (fix) |
|---|---|---|
| Owner | reads it (user.talk_kick) | reads it (voice.disconnect) |
| Administrator | reads it | reads it |
| auditor with View audit log | reads it | hidden |

doc/talks.md (call Talks): "Nothing about it reaches the audit log (who joined it, renamed it or when it went), because that would tell the Owner who called whom." internal/server/audit_presence.go:50 skips call Talks for voice.join and voice.leave for exactly this reason. The cause is that `restrictTo` returns early for privileged readers (internal/audit/filter.go:92), while `ViewableChannels` keeps call Talks from Administrators.

The fix narrows an existing leak (auditors no longer read it), so it is not a regression. But it leaves the Owner and Administrators reading who was in which call. That makes the new OpenAPI sentence on `disconnectFromTalk` ("read only by auditors who can view the Talk") false for call Talks.

Smallest fix: in `disconnectFromTalk`, do not record the entry when `talks.IsDMTalk` says the Talk is a conversation's, and fail closed on a lookup error, as `writePresence` does. Add the case to the new test. Old rows of call Talks stay readable to the Owner and Administrators only, which is acceptable to mention in the changelog. The moderation history (Manage reports) also lists such a removal as `talk_removal` without the Talk. That holds before and after the fix and would go away with the same change.

### Moderation history

`voice.disconnect` maps to `talk_removal` (internal/moderation/history.go:69). The history query reads only the reason, duration and role fields (history.go:177-181), never channel_id. The new test checks that the body does not contain the Talk id. Unchanged for old rows.

### Every consumer of user.talk_kick

grep over Go, TS, Vue, JSON, SQL and docs found:

- routes.go:67 (admin-wide kick, unchanged)
- classes.go (Auditors row)
- history.go (talk_removal)
- the en and de labels "Removed from Talk" / "Aus Talk entfernt" (now also used for `voice.disconnect`, identical text)
- doc/moderation.md
- the tests

No webhook event, export or alert uses the action name. The GDPR account export (internal/auth/account/account_export_data.go:85) exports rows by actor or target, with details. That is unchanged and is the member's own data. The console (web/src/admin/components/AdminAuditTab.vue) filters by group prefix only and does not render details.channel_id specially.

NOTE B2: new per-Talk removals now fall under the console's "voice" group, no longer "user". An admin who filters "user" no longer finds them, while old ones stay under "user". This is acceptable and follows from the class decision. doc/moderation.md's events table now lists `voice.disconnect`.

The labels test (web/src/i18n/auditLabels.test.ts) was emulated with a script over the same regex: `voice.disconnect` is found and labelled in en and de, nothing is missing. vitest was not run (no `npm ci` in this worktree).

### Cost of the extra predicate (200,000 rows, ANALYZE, best of five, reader sees 5 of 50 channels)

| Distribution | Query | without legacy WHEN | with it |
|---|---|---|---|
| uniform over the 139 written actions plus voice.disconnect, half with a top-level channel_id | first page (51) | 0.28 ms | 0.28 ms |
| | `action=user` | 0.36 ms | 0.34 ms |
| | `action=voice` | 15.7 ms | 16.4 ms |
| nothing visible (all auth.login, full backward scan) | 51 | 73.9 ms | 72.1 ms |
| all 200,000 rows legacy user.talk_kick of Talks the reader cannot see | 51 | 0.21 ms (shown before) | 303 ms (now hidden, full scan) |
| | `action=user` | 2.9 ms | 147 ms |

The plan is unchanged: a backward scan of `audit_log_pkey` with the CASE as a filter, stopping at the limit. The predicate costs nothing on rows of other actions, because `action = 'user.talk_kick'` is checked first. The last scenario is an artificial worst case: 200,000 old per-Talk removals, all hidden. It costs about 1.1 µs per legacy row (regex and jsonb extraction) on top of a deep scan that only happens because those rows are now correctly hidden. NOTE B3: acceptable for an admin-only, paged route.

### Changed existing test (talk_disconnect_integration_test.go:105-107)

The old count was `action = 'user.talk_kick'` by actor, 4. The new count is `action = 'voice.disconnect' AND channel_id IS NOT NULL` by actor, 4. That is stricter: it also proves ChannelID is set. It is not weakened.

### Revert checks (each part on its own, at d9760e384)

- `filter.go` back to a1fa2adf5: the new test fails ("Talk's id: true", "okapiold: true"). The scratch cross product fails for every legacy row, including the malformed ones, for the Staff auditor, the plain auditor and the Moderator.
- `users_admin_talk.go` back: `TestDisconnectMembersFollowsTalkOverwrites` fails ("audit entries: 0, want 4"). The new test passes, as the author says, because the read-time rule hides the reverted writer's rows too.
- `history.go` back: the new test fails ("the moderation history lacks the Talk removal").

Everything was restored afterwards.

### Public API (doc/api-stability.md)

Audit action names are not listed as a surface on their own. The REST surface covers the shape of `GET /api/admin/audit-log` (`action` is a string with an example, not an enum), and that shape is unchanged. "Documented behaviour is part of the contract", though, and the base's OpenAPI description of `disconnectFromTalk` named `user.talk_kick`. NOTE B4: a dashboard that counts per-Talk removals by `action=user.talk_kick` misses the new ones. That is acceptable for a confidentiality fix, but the release note should say plainly that per-Talk removals are now recorded as `voice.disconnect`. The commit subject, which release-please uses, already does.

### Other

- NOTE B5: a row whose details were blanked by an account erasure loses `channel_id` and falls back to Auditors class. It then names no Talk, the same as an admin-wide kick. Correct as the author describes.
- Documentation: doc/moderation.md (events table, class table, admin actions list), the classes.go comment and the OpenAPI texts match, except for the call-Talk case (SHOULD-FIX B1).
- Gates: no allowlist entry, no limit raised, no test skipped. `voice.disconnect` was added to the class pin list. Commit type is `fix`, no `!`, no attribution line.

## What I ran

- Scan dump at 6e4164178 and a1fa2adf5 (unit, scratch).
- Planted writers against `TestEveryWrittenActionHasAClassOfItsOwn` at a1fa2adf5.
- Phase 2 repro: the finding's command at a1fa2adf5.
- Scratch integration tests at d9760e384: the reader-by-row cross product with paging and group filters, and the call-Talk removal. The call-Talk test was also run at a1fa2adf5.
- Revert checks as listed.
- Timing on 200,000 rows in the integration database, three distributions, plus EXPLAIN (ANALYZE, BUFFERS).
- Gates at d9760e384: gofmt (clean); go vet with and without `-tags=integration` on internal/audit, internal/auth/memberadmin, internal/moderation and internal/server (clean); `make complexity-check` exit 0; `go test ./internal/repo/ ./internal/audit/ ./internal/moderation/ ./internal/auth/memberadmin/` ok; `node scripts/check-english-only.mjs` ok; `make openapi-check` exit 0.
- Full suite once: `go test -p 1 -tags=integration -count=1 ./internal/server/ ./internal/audit/ ./internal/moderation/ ./internal/auth/memberadmin/` at d9760e384: all ok (internal/server 249 s).

## What I did not check

- The console in a browser (API level only), and vitest (the labels test was emulated).
- Rows on any real instance (none contacted).
- Group conversations' call Talks separately from two-person ones (same code path).
- Other packages' integration suites.

## Round 2 (H-L6-03): delta 23e164337 on d9760e384

Verdict: **MERGE AFTER FIXES**. One small SHOULD-FIX (R2-1). No BLOCKER. SHOULD-FIX B1 of round 1 is resolved.

### What changed

`disconnectFromTalk` now calls `auditTalkRemoval`. That helper writes nothing when `talks.IsDMTalk` says the Talk belongs to a conversation, and nothing when the lookup fails (it logs a warning instead). As a result, the moderation history lists no removal from a call Talk. Old `user.talk_kick` rows from call Talks are documented rather than hidden. The `restrictTo` comment is corrected. doc/upgrade.md, doc/talks.md, doc/moderation.md and the OpenAPI text are updated.

### Checks

- **The test fails before and passes now.** `TestARemovalFromACallTalkIsNotAudited` passes at 23e164337, together with `TestTalkRemovalFromAPrivateTalkReachesOnlyAuditorsWhoSeeIt` and `TestDisconnectMembersFollowsTalkOverwrites`. With only users_admin_talk.go reverted to d9760e384 it fails with exactly the lines the commit names:
  - Owner and Administrator each read the call Talk's id and the removal.
  - The moderation history lists the removal.

  The test covers my round-1 scenario: a conversation member with Disconnect members from a role, and readers outside the conversation.
- **A lookup error records nothing, and the removal still happens.** The kick runs inside `Moderated` before `auditTalkRemoval` is called (users_admin_talk.go:96-106), so a failed lookup still answers 204 and logs a warning. I decide this is right and should not be refused:
  - The removal is a safety action that has already happened.
  - Refusing afterwards would report a failure for something that was done.
  - It matches `writePresence` (internal/server/audit_presence.go:50) and the best-effort `RecordOrLog`, which also only logs when the insert fails.
- **SHOULD-FIX R2-1: a Talk that is already gone fails open.** `talks.IsDMTalk` returns `(false, nil)` for an id with no `channels` row (internal/chat/talks/talks.go:145-146). If the call Talk is deleted between the kick and the lookup, the entry is written as `voice.disconnect` with a conversation's call Talk id. The Owner and Administrators then read it.
  - The Talk can be deleted in that window because the conversation is deleted, or because the empty-Talk countdown fires. The default countdown is 30 s, so this needs a concurrent deletion and the window is milliseconds.
  - `writePresence` closes the same gap with a second query: if the Talk is gone, it records nothing (audit_presence.go:56-60: "A Talk that is gone by now may have been a conversation's").
  - Fix: do the same in `auditTalkRemoval`. Treat a missing Talk as "do not record", for example by checking the row exists or using a lookup that reports "not found".
  - Evidence is from the code only; I could not time a deterministic run.
- **"Every moderation action is recorded".** doc/moderation.md does not say that. Line 10 is about permissions. "What is recorded" (line 494) promises "every successful mutating request below `/api/admin`". The per-Talk route is not below `/api/admin`, and its events-table row now says plainly that a removal from a call Talk is not recorded. doc/talks.md says the same in its call-Talk paragraph, and doc/upgrade.md says it for operators. I found no contradiction. Leaving no trace is acceptable here: the documented promise for call Talks (not telling the Owner who called whom) is the stronger one, and voice.join and voice.leave already work the same way.
- **The admin-wide kick from call Talks.** `POST /api/admin/users/{id}/kick` also removes the member from a call Talk when the caller is a member of that conversation with Disconnect members (non-members get 0 permissions there). The middleware always records it as `user.talk_kick`. That entry names no Talk; its details hold the params and a user snapshot. So it does not say "who called whom", only that the member was removed from voice at that time. That is acceptable, and the author's choice is consistent with the purpose of doc/talks.md.
  - NOTE R2-2: doc/talks.md now lists "was removed from it" among things that never reach the log. Add half a sentence saying the admin-wide kick is still recorded as `user.talk_kick` without the Talk, so the wording is literally true.
- **Old call-Talk rows.** These are hidden from auditors by `legacyTalkRemoval` and readable by the Owner and Administrators, as now documented. They cannot be told apart at read time, because the call Talk's row is usually deleted. Accepted.
- **Gates by exit code at 23e164337.**
  - gofmt: clean.
  - go vet (with and without `-tags=integration`) on internal/auth/memberadmin, internal/audit and internal/server: 0.
  - `make complexity-check`: 0.
  - `go test ./internal/repo/ ./internal/audit/ ./internal/auth/memberadmin/`: 0. This covers the package production-file cap and the size limits, after the new import of internal/chat/talks.
  - `node scripts/check-english-only.mjs`: 0.
  - `make openapi-check`: 0.
  - Commit type `fix`, no attribution line.
  - I did not re-run the full integration suite, since its single run was spent on d9760e384. The delta's own and neighbouring tests pass.

## Round 3 (H-L6-03): delta 985f56c66 on 23e164337

Verdict: **MERGE**. R2-1 and R2-2 are resolved. No new findings.

- **The check now runs before the removal.** `disconnectFromTalk` has one path. Inside the `Moderated` callback the order is:
  1. `RequireChannel` (the permission check),
  2. `auditableTalk` (decides whether to audit),
  3. `kickUserFromVoice` (the removal).

  The entry is written only when the action succeeded, someone was removed, and the Talk was judged auditable (`err == nil && kicked && audited`). The admin-wide kick does not use this path (the middleware records it). Because the check runs while the Talk still exists, a call Talk deleted after the removal can no longer be mistaken for a public one.
- **Missing Talks and failed lookups.** `auditableTalk` reads `dm_id` itself:
  - A missing row (`ErrNoRows`) means "not audited" without a warning.
  - Any other error means "not audited" with a warning.
  - In both cases it never returns an error, so a failed lookup cannot abort the removal.

  `TestOnlyAPublicTalkThatExistsIsAudited` covers five cases (public, call, gone, failed, cancelled) through a fake database connection.
- **A public Talk deleted between the check and the write is harmless.** The entry is written with the id of a deleted public Talk. It is Channel class, so auditors who can no longer view that Talk do not see it. The Owner and Administrators could see that Talk anyway. This matches how a deleted Talk's `voice.join` rows behave.
- **A stale answer from the lookup cannot matter.** The lookup runs on the connection pool, outside the permission-lock transaction, but `channels.dm_id` cannot change:
  - It is set only when the row is created (migration 0079 adds it with `REFERENCES channels(id) ON DELETE CASCADE` and a CHECK that ties it to temporary voice Talks).
  - No `UPDATE channels SET dm_id` exists in the code (grep).

  So the only change possible between the check and the write is "exists" becoming "gone", which is covered above. A plain SELECT does not wait on the transaction's locks. Querying the pool inside the callback is an existing pattern: `RequireChannel` and `kickUserFromVoice` already query outside the transaction.
- **R2-2.** doc/talks.md now says the admin-wide kick is still recorded as `user.talk_kick`, naming the member but no Talk, also when it ends a call. This is consistent with what "Nothing about it reaches the audit log" means there, and with doc/moderation.md.
- **Tests run at 985f56c66.** `TestDisconnectMembersFollowsTalkOverwrites`, `TestTalkRemovalFromAPrivateTalkReachesOnlyAuditorsWhoSeeIt`, `TestARemovalFromACallTalkIsNotAudited` and `TestOnlyAPublicTalkThatExistsIsAudited` all pass.
- **Gates by exit code, all 0.** gofmt (clean), go vet with and without `-tags=integration` on internal/auth/memberadmin and internal/server, `make complexity-check`, `go test ./internal/repo/ ./internal/audit/ ./internal/auth/memberadmin/`, `node scripts/check-english-only.mjs`, `make openapi-check`. The commit type is `fix`, with no attribution line.
