# Review: integration of the hunt bundle (collection branch, tip 3e337bcc8)

Reviewer: independent, read-only. Scope: what the coordinator did while merging
about 40 reviewed fix and harness branches onto origin/main f58bc3fc6 (release
3.0.9): the coordinator's own commits, every merge whose result differs from
the automatic merge, and interactions between fixes that no single fix review
could see. The branch for H-L5-01 and H-L5-04 is not in this tip and was not
reviewed. Own worktree at 3e337bcc8, removed at the end; nothing pushed, no
branch created.

## Verdict: OK to open the pull request

No blocker. One SHOULD-FIX (a documentation sentence, can go into the same
pull request or later), and NOTEs. One defect from before this branch is
written up below as a finding candidate. It is on 3.0.9 as well, and this
branch neither causes nor worsens it.

## Method

- `git log --first-parent origin/main..3e337bcc8`: 52 first-parent commits.
  10 are not merges: the eight the task names, the three answer-vetting commits
  (which are on the second parent of 90e3c71f6), and 82e51418f, the lane 5
  store trace harness at the bottom of the branch (only tests and one
  workflow).
- For every merge, the result was compared with `git merge-tree --write-tree
  ^1 ^2`. That redoes the automatic merge, so this finds every hand-resolved
  file, including a resolution that silently took one side. `git show --cc`
  alone misses that case. 12 merges differ from the automatic merge. In each
  of them, every line that either parent added since the merge base was
  checked against the result, and every line missing from the result was read
  and explained (list below).
- Production files changed by more than one merge, and functions changed by
  more than one merge, were listed from the hunk headers and read.
- Every lock statement the branch adds (blame against origin/main) was traced
  against the erasure's lock order. One cycle was tried on a real server.

## A. The coordinator's own commits

**a4258ed83, internal/automod/rule.go:268 (cleanDomain).** A link-rule domain
must now read back through hostOf as itself, or it is refused as "not a
domain". Probes, before and after the fix: `0.00..` was accepted as `0.00`
before (hostOf reads `0.00` as 0.0.0.0, so the entry could never match a link)
and is refused now. `1.00`, `0x7f.00`, `192.168.000`, `https://0.00./x`, `1.2.3`
and `10.0.0.1` are refused before and after. `example.com.`, `EXAMPLE.com..`,
the Unicode form of `xn--bcher-kva.example` and `a.1e` are accepted before
and after, with the same stored form. Reverting rule.go makes seed `0.00..` of
FuzzCleanDomain fail. The fix is correct and refuses only names no link can
match.
Callers: the only caller is `RuleInput.Normalize`, used only by
`parseRule` (internal/moderation/automod_rules.go:91) for create and update.
Stored rules are loaded by `automod.ListRules` and compiled
(internal/automod/compile.go:37) without being normalized again. So a rule
stored earlier with such an entry still loads, and it matches nothing, as
before. NOTE: updating such a rule with its full body (the admin dialog sends
the whole config) now answers 400 `"0.00" is not a domain` until the admin
removes that entry. This is the right outcome. No upgrade note is needed for
an entry that never worked.
NOTE: FuzzCleanDomain's property (`hostOf(d) == d`) is now exactly the check
cleanDomain performs, so the fuzz target can no longer fail. It still guards a
future removal of the check. The real oracle of this fix is FuzzLinkRules and
the two seeds.

**5d88a24bd (webhook settings test).** It now counts the id in `mentions`
instead of in the whole answer, because the mention-kinds fix also lists the
member under `direct_mentions`. `DirectMentions` is documented, and built, as
a subset of `Mentions` (internal/chat/core/message_types.go:58,
internal/chat/messages/messages.go:151). So every "0" assertion is exactly as
strict as before, and the "1" assertion measures what it always meant. It is
not weaker. The test passes.

**b61a553a7 (export fault cycle).** `buildExportInBackground` now returns an
error (`!= nil` replaces `!`). `Drain` now refuses every job after it starts
(the drain fix), so the per-cut wait is `DrainMail`, which waits on the same
WaitGroup (`h.rec.wg`, internal/auth/authn/account_mail.go:137) without
closing job acceptance. Each build joins that group through `TrackJob`. The
final step still uses `Drain(30s)`. The test is equivalent: it waits for the
same builds. Its only bound is now the test timeout instead of 30 s. It passes.

**871dc3ab9 (presence burst).** Stricter: it adds an assertion that the
`audit_presence` drop counter equals the missing rows. It passes.
The documentation sentence it changed is SHOULD-FIX 1 below.

**14ebe8be1 (ci).** It adds `MNEMA_DRAIN_HAMMER=1 go test -race -run
'^TestDrainHammer$' ./internal/auth/authn/`. `go test -list` finds the test,
and it ran green under -race (16 s).

**4ae4fbe03 (docs + test helper).** The orphan scan covers `uploads/`,
`avatars/`, `banners/`, `covers/` and `exports/`, and checks exports against
`data_exports.s3_key` (internal/media/orphans.go:26-32, :74). The new OpenAPI
and annotation sentences are true. `make openapi-check` is 0. The removed
`knownGap` helper has no user left.

**3903bc122 (benchmark).** 120 allocation requests per minute per member
(`newMemberLimiter`, internal/turnrelay/server.go:58, options.go:49) and 256 in
total (options.go:46). True.

**82b4ba901 (architecture).** passPacer keeps one writer per SSRC (plus an RTX
alias), and `UnbindLocalStream` removes both (internal/sfu/bwe.go:101,
:163-170). True.

**Answer vetting (e01ced711, 065d9b451, 0e5b74b47).**
`internal/sfu/answer_candidates.go`, `answer_ssrcs_test.go` and
`attach_viewer_test.go` are byte-identical between 0e5b74b47, the tip that
fixreview-H-L8-08 round 2 approved, and 3e337bcc8, and no commit touches them
afterwards. `ingress.go` and `ssrc_flood_test.go` were changed later by other
reviewed fixes (a15ea9e18, d77aa2893, 714225dd4). Every line the three commits
added to ingress.go is still in the tree.

**82e51418f (store trace harness, not in the task's list).** Tests, fixtures
and `.github/workflows/store-trace.yml` only. The workflow is pinned to full
SHAs, has `permissions: contents: read` and a 60-minute timeout, and puts no
untrusted input in run steps. NOTE: store-trace.yml is not in AGENTS.md's
workflow tree. Neither are ci-macos-calls, codeql, load-test, onboarding and
turn-probe on main, so this is not a regression.

## B. Merges with a hand resolution

Hand-resolved files (result differs from the automatic merge): 3e337bcc8
(sim-sweep.yml, AGENTS.md), 3fcdb7097 (sfu.go, room_tracks.go,
subscription_cycle_test.go), 50b495ffa (events.md, talks.md, upgrade.md,
handler_validate.go), fa544c89a (SECURITY.md, room_attach.go, room_tracks.go,
sfu.go, clock_step_test.go, source_replacement_test.go,
subscription_cycle_test.go), 02395c7a8 (SECURITY.md, upgrade.md,
erasure_in_flight_integration_test.go), 23dc6e381 (upgrade.md, webhooks.md),
f305da991 (communityevents/handler.go), a5d17e226 (upgrade.md), 46f97679a
(storeTrace.known.ts), 8cbc568c3 (two canary tests), 2f4342866 (moderation.md),
0c47488b8 (sim-sweep.yml, AGENTS.md). The combined diffs of 6577da7fb
(message_mutations.go) and 17abe73dd are clean automatic merges (read below).

**fa544c89a and 3fcdb7097 (resume budget on the steady clock).** In the
final tree the only writer of `TrackInfo.resumeAt` is `stopSourceTracksLocked`
(internal/sfu/sources.go:104-117). It stores `stopUntilLocked(...,
layerNow())`, which returns `steadyNanos(now.Add(wait))` (sources.go:166).
The only reader is `resumeStoppedTrack` (internal/sfu/room_attach.go:388-396),
which compares with `steadyNanos(layerNow())` and clears the value with a CAS.
The budget (`resumeBudget.at`, a `time.Time` from `layerNow()`) is only used
through `now.Sub(b.at)` (sources.go:135-137), which uses the monotonic
reading. So neither the deadline nor the refill can be moved by a wall clock
step. `steadyNanos` itself is `steadyStartUnix + t.Sub(steadyStart)`
(layers.go:67). Nothing in the package still reads `UnixNano` for these
values. The fa544c89a resolution (resumeAt in steadyNanos, stopUntil on
layerNow) was then fully replaced by the per-source budget of bbfec1723 in
3fcdb7097, which took the fix side. That side already returns steadyNanos,
because the fix branch was built on the steady-clock fix. The dropped
`TrackInfo.stopUntil`, `resumes` and `resumesAt` have no users left (the build
and vet are 0). Tests: TestClockStepBackResumesAStoppedCamera and
TestClockStepForwardKeepsTheResumeHoldoff step the wall clock while keeping
the monotonic reading. TestStopResumeBudget and
TestResumeBudgetOutlivesCapturesAndRejoins use the room's budget. The SFU
suite is green. SECURITY.md: both sentences are kept, in both merges. The
budget text says "belongs to the publisher and source, so a new capture or a
rejoin does not refill it", which is true: the entry of a publisher who left
is deleted only once the budget is full again (sources.go:146-150).

**f305da991 (createEvent).** The resolution keeps `commitAfter(r, pending,
err)` from the automod side and drops that side's post-commit
`h.auditRecord(r, "event.create", ev)`, because the first parent moved the
entry into `createOrStartSeries`. Paths in the final tree:
- single event: `Store.create(..., RecordTx(tx, eventEntry("event.create", ev)))`
  (handler_series.go:36-39) writes exactly one entry, inside the
  transaction. A failed entry rolls the event back, so `err != nil` and the
  automod hit is not committed.
- series: `CreateSeries`, then one `auditRecord` after it commits
  (handler_series.go:52). Exactly one entry. It is not inside a transaction,
  and doc/moderation.md:520-530 says so ("a new event (not a series)" is
  in-transaction, "series" is after the commit). The hit is committed when
  `CreateSeries` succeeded. A later `GenerateSeries` error is only logged, and
  the event exists then, so committing is right.
- an error before creation (validation, frequency, until) returns before
  `commitAfter` commits anything.
The in-transaction path does not call `auditable`. It needs no call: creation
refuses a conversation's call Talk as its location (`validateEventTalk`,
handler_validate.go:62-70), and `RecordTx` is nil-safe (audit.go:79).
Correct.

**Documents (02395c7a8, 50b495ffa, 23dc6e381, a5d17e226, 2f4342866).** Every
paragraph was compared with both parents:
- SECURITY.md (02395c7a8): the erasure sentence of one side and the export
  orphan wording of the other ("the orphan cleanup removes only an archive no
  export row names") are both kept. This matches 4ae4fbe03 and orphans.go.
- doc/upgrade.md: the sections "Nothing new may name an erased account",
  "Removals from a Talk in the audit log", "Calls in conversations no longer
  need View channels", "Conversations' calls leave the community's objects"
  and "A role mention next to @all breaks a mute" are each present once. No
  sentence of either parent is missing. The only lines dropped are conflict
  markers and blank-line placement.
- doc/events.md and doc/talks.md (50b495ffa): each resolved paragraph is the
  union of both parents' sentences (automod screening plus "not at all for an
  event of a conversation's call Talk"; "was removed from it" plus "no
  community channel ... `PATCH /api/talks/{id}`" plus the admin-wide kick
  sentence). Nothing contradicts.
- doc/webhooks.md (23dc6e381): "a web app from before 3.0.10" became "a web
  app that does not know `direct_mentions` yet". The version is not fixed yet,
  so this is the better wording.
- doc/moderation.md (2f4342866): the voice.join row comes from the first
  parent (with 871dc3ab9's wording), and the new voice.disconnect row from the
  second. Both are kept.

**8cbc568c3 and 46f97679a (emptied known-failure lists).**
- `canaryKnownLeaks` and `canaryKnownDeepOnly` (all H-L6-01; its fix
  d1e297554 is merged earlier in the branch) are empty. Ran
  `VIS_SWEEP=1 go test -tags=integration -run
  TestNoCanaryReachesAnObserverOutsideItsClass ./internal/server`: PASS in
  1171 s, the deep mode the nightly job runs. Smoke is green in the full
  suite. So no removed row still leaks, and a leak that came back would fail.
- `storeTrace.known.ts` drops the H-L5-02 and H-L5-03 rows (the fix's side)
  and `clean reply-follow` H-L5-05 (merged in 8b853104c before). The remaining
  rows are H-L5-01 and H-L5-04, both still open here. I recorded 20 new
  traces (`STORE_TRACE_SEEDS=20 STORE_TRACE_FIRST=5000 go test
  -tags=integration -run 'TestStoreTrace$' ./internal/server`, exit 0) and
  replayed them unsharded (`STORE_TRACE_DIR=... vitest run
  src/stores/storeTrace.test.ts -t deep`): passed. With 20 or more unsharded
  traces the test requires every nightly key to still break and no other key
  to break. So the list is right at this tip. (The lane 5 branch still to
  come will shrink it again.) The smoke replay is green in the web run below.
  NOTE: the replay prints happy-dom `AbortError` stacks at teardown, but the
  run is green.

**0c47488b8 and 3e337bcc8 (sim-sweep.yml).** The final jobs are sweep,
availability, links, powers, clocks, cycle, explore, crash, sfu, trust and
races. Each is present once. The text of each job block was compared with the
parent it comes from: identical, except that `sweep` is the newer side's
(with the pooled-availability upload, which `availability` needs), and that
`races` has 14ebe8be1's step. `actionlint .github/workflows/*.yml`: exit 0.
Commands: `go test -list` finds TestUnhandledSRTCPStreamsAreCapped,
TestSubscriptionSwitchesRetainNothing, TestTrustChainSequences and
TestDrainHammer. The races pattern matches 5, 1, 1, 3 and 21 tests in its
five packages. For the web jobs, `vitest list` printed nothing for most
env-gated files even with their variables set (a listing quirk; it did list
groupExplore and groupCrash). So I checked instead that each file exists and
that its `describe.skipIf(!process.env.X)` gate is the variable the job sets
(SIM_SWEEP, SIM_POOL_DIR, LINK_SWEEP, SIM_POWERS, SIM_CLOCKS, SIM_CYCLE,
SIM_EXPLORE, SIM_CRASH): all match. AGENTS.md: the sim-sweep line is the union
of both descriptions, and visibility-sweep.yml is added. Its wording is a
little long ("...; and ... ; and the SFU's resource bounds"), but nothing is
lost.

**50b495ffa code files.** handler_validate.go imports `auth`, `automod` and
`talks`, and all three are used. In channel_permissions.go, checkChannelMove
combines H-L6-02's removal of `targetCategoryVisible` with the call-Talk
fix's `snap.IsDMTalk(c.ID)` refusal. Both are kept, and the order (hidden
check first) is right.

**Other combined diffs.** In 6577da7fb (message_mutations.go
changeReaction), `core.ReactionForm` normalization comes before the
`applyReaction` refactor of the other fix, so every path of the toggle sees
the normalized emoji. In 02395c7a8 and 8cbc568c3 (test worlds), the renames
`canaryWorld` and `erasureWorld` are consistent, and the tests compile and
pass.

## C. Interactions between fixes

**Functions changed by more than one fix:** `audit.filter.restrictTo`
(H-L6-01, then H-L6-03 built on it), `channels.selectExpired` (auto-delete pin
fix, then the call Talk fix), `checkChannelMove`, `changeReaction`,
`createEvent` and `auditRecord`, `SetAnswer` and `removeStaleSenders` (H-L8-07
and H-L8-08), `resumeStoppedTrack` and `stopSourceTracksLocked`. Each was read
in its final form. No function lost one fix's behaviour to the other.

**Locks.** The new locks are: the reporter row `FOR NO KEY UPDATE` (reports.go:198), the
message `FOR UPDATE` on delete (message_mutations.go:173), `FOR KEY SHARE` on
the pin's root (pins.go:184), the group conversation `FOR UPDATE`
(dm_members.go:19), the webhook channel `FOR NO KEY UPDATE`
(webhook/store.go:233), the advisory locks (account tokens, owner seed, role
templates), and the 0103 triggers (`FOR KEY SHARE` on users). Only the erasure
takes users rows in a conflicting mode (`FOR UPDATE`, account_erase.go:85).
`FOR KEY SHARE` and `FOR NO KEY UPDATE` do not conflict, so the report path
cannot cycle with the 0103 triggers. The advisory locks are taken first in
their transactions and are each held by one code path only. The pin path takes
a key-share lock on the root, and the sweep skips locked rows (SKIP LOCKED),
so no wait arises there. No cycle is created by combining two of this branch's
fixes.

**One cycle that is already on 3.0.9** (finding candidate, not caused by this
branch): the erasure takes the member's users row first and then empties their
messages (internal/chat/account/account_erase.go:42). The member's own edit
from a second device locks the message first (`FOR NO KEY UPDATE`,
message_mutations.go:65-70) and then inserts into `message_edits`
(edit_history.go:34), which needs the users row (the foreign key check on
3.0.9, the 0103 guard on this branch). PostgreSQL resolves the cycle by
aborting the transaction whose deadlock check runs while the cycle exists.
When the edit's wait starts more than `deadlock_timeout` (1 s) before the
erasure reaches its messages step, that transaction is the erasure.
Reproduced on a real server, through the real routes, with the erasure held by
a pgx tracer right before its messages step:
- held for 300 ms: the edit answers 500 (`deadlock detected`), and the erasure
  answers 204.
- held for 2.5 s: **the erasure answers 500** (`erase "UPDATE messages SET
  content = '' ...": deadlock detected (SQLSTATE 40P01)`), and the edit goes
  through.
The same probe on f58bc3fc6 (3.0.9) gives the same two outcomes. Nothing
leaks: the erasure rolls back, and a retry succeeds. But the H-L10-05 review's
statement "the erasure always wins" (fixreview-H-L10-05.md:232-234) holds only
when the writer starts waiting less than 1 s before the erasure closes the
cycle. A large account, whose earlier erase steps take longer than that, can
see its deletion fail once. The probe is at
reviews/review-bundle-integration-deadlock-probe.go.txt (put it in
internal/server and run it with -tags=integration). (The probe is not in the repository; the finding it
led to is H-L10-06, [doc/security/findings/2026-11-hunt/h-l10-06.md](../../findings/2026-11-hunt/h-l10-06.md).) Suggested fix, in a later
hunt branch: retry the erasure transaction on 40P01, or lock the member's
messages (`SELECT ... FROM messages WHERE user_id = $1 FOR UPDATE`) right
after the users row. The second is still racy for messages the member is
about to edit, so a retry is simpler. Severity: availability, low (S4).
(Fixed later as H-L10-06: the erasure transaction is retried up to three times when the database aborts it as a deadlock; after that the request answers 503 "the server is busy" and the member stays signed in; fix review: [h-l10-06.md](h-l10-06.md).)

**H-L6-03 together with the erasure's audit scrub** (NOTE, by design of both).
`legacyTalkRemoval` (internal/audit/filter.go:116) classes an old per-Talk
`user.talk_kick` row by `details.channel_id`. The erasure's `scrubAudit`
(account_erase.go:39) sets `details = '{}'` on every entry about, by or
mentioning the erased member. After that scrub, such a row falls to the
`user.talk_kick` auditors row (classes.go:67). Every auditor then sees "member
removed at time T, by a moderator (name blank or given)". It no longer names
the Talk, so it says what an admin-wide kick says. No Talk leaks.

**Tests passing for the wrong reason:** none found beyond the
FuzzCleanDomain note. The webhook, export-cycle and burst adaptations measure
what they did before (section A). The canary and trace lists were checked in
deep mode.

## SHOULD-FIX

1. doc/moderation.md:556: "the server log writes each one at the default log
   level". The line is `slog.Warn("audit log: presence queue full, event
   dropped", "channel", channel)` (internal/server/audit_presence.go:43). Warn
   is shown at the default level, so that part is true. But the line names
   only the Talk: not the member, and not whether it was a join or a leave. So
   the log does not keep "each one" in the sense the sentence gives the
   reader. Fix either side: add `"user", user, "joined", joined` to the log
   line (one line of code; nothing new to protect, the server log already
   carries member ids), or say "the server log notes each drop with its Talk".

## What I ran (on 3e337bcc8, nice -n 10, all exit codes read)

- `go build ./...` 0; `go vet ./...` 0; `go vet -tags=integration ./...` 0.
- `make complexity-check` 0, `make docs-check` 0, `make openapi-check` 0,
  `make contracts-check` 0, `node scripts/check-english-only.mjs` 0,
  `actionlint .github/workflows/*.yml` 0.
- Full suite `go test -tags=integration -p 1 -count=1 ./...`: exit 0, 85
  packages ok, internal/repo included (internal/server 292 s, internal/sfu
  91 s). Spot check that the integration tests really ran (three tests with
  -v).
- Lane 7 deep sweep: `RACE_SWEEP=1 go test -tags=integration -p=1 -count=1
  -timeout 50m -run '<races pattern>'` on the five packages: exit 0.
  `MNEMA_DRAIN_HAMMER=1 go test -race -run '^TestDrainHammer$'
  ./internal/auth/authn/`: exit 0.
- `VIS_SWEEP=1` canary sweep: PASS (1171 s).
- Store trace deep: 20 recorded traces, replay passed.
- web: `npm ci`, then `vitest run src/lib/media src/stores --maxWorkers=3`:
  118 files passed, 6 skipped; 1221 tests passed, 5 expected failures, 88
  skipped. Exit 0.
- Probes: cleanDomain with 16 inputs, with and without a4258ed83 (removed
  afterwards). The erasure/edit deadlock on this tip and on f58bc3fc6 (removed
  from both trees; kept as a .txt under reviews/).

## Not checked

The nightly web sweeps themselves (sim, powers, clocks, cycle, explore, crash,
links: hours each). Only their wiring was checked. The SFU deep probes
(SFU_SSRC_FLOOD_DEEP, SFU_SUB_SLOPE) were not run. The trust chain sweep was
not run. The web suite outside src/lib/media and src/stores was not run (the
coordinator ran all of it on an earlier tip). The fixes themselves were not
reviewed again; each has its own review.
