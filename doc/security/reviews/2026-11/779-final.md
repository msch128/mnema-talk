VERDICT: NO BLOCKER. D1-D4 are each addressed correctly, and the delta introduces no defect I could reproduce or construct. One SHOULD-FIX: the D1(a) code path has no test that fails without it.

REVIEW of the unreviewed final state of #779 (merged as e2c3afa89, code state 465c2b93b) -- independent, read-only, 2026-11-12.
Nothing in the repository was edited, committed or pushed. All runs were in a scratch copy of web/ outside the repository.

## What was never reviewed

REVIEW-779d reviewed head 347d3f1bd (five commits on cd402e0bf). The merged state 465c2b93b is a rebase of that branch onto main plus one more edit.
347d3f1bd is not an ancestor of 465c2b93b, but both objects exist locally.

`git diff 347d3f1bd 465c2b93b -- web/src/lib/media SECURITY.md` is the whole delta:
- groupMerge.ts: 3 code changes, at lines 93, 97-98 and 151.
- groupMerge.test.ts: 1 new test (sign older than ten minutes).
- groupMerge.room.test.ts: 1 new test (the D1 scenario).
- SECURITY.md: the D2 and D4 text, plus path renames from the rebase.
- guard.ts: one import path from the refactor rebase.

Nothing else under web/src/lib/media changed. group.ts, groupVet.ts, groupSync.ts and groupEpoch.ts are untouched.
`git diff 465c2b93b e2c3afa89 -- web/src/lib/media SECURITY.md` is empty: the code on main is exactly 465c2b93b.

## What I ran

- vitest run src/lib/media/groupMerge: 2 files, 36 tests, all pass.
- vitest run src/lib/media: 33 files, 366 passed, 8 expected failures, 43 skipped.
  Count difference to 779d (35 files, 378 passed): the media test files are identical at both commits (35 in `lib/media`-prefixed paths). 779d's filter `src/lib/media` also matched web/src/lib/mediaStats.test.ts and mediaStatus.test.ts, which the layout refactor moved out of that prefix: 2 files, 12 tests. The expected-failure (8) and skipped (43) counts are equal.
- Seed sweep, seeds 1..400 on both relays, with the author's zzsweep harness copied unchanged: hostile 26 failing, honest 17 failing. Both sets equal the HOSTILE and HONEST lists in groupSim.known.test.ts exactly (none missing, none extra); no gate seed fails. Same result as 779d at 347d3f1bd, so the three code lines move none of the 800 runs. The author did not re-run this for the final commit.
- Mutation checks (revert one change in the scratch copy, run the merge tests):
  - remove the age check at line 93: the new age test fails ("expected ['mls_key_package'] to deeply equal []").
  - remove `this.heard.add(from)` at line 151: the new room test fails (1814 s against a bound of 335 s).
  - restore the old renewal rule at line 97: all 36 tests still pass (see SF1).

## D1-D4 against the final code

D1: fixed. Part (b) is the line-151 change (a verified heartbeat adds the id to `heard`). Part (a) is the line-97 change (`offered` looks at every entry of `asks`).
- Part (b) carries the scenario: without it the room test fails at 1814 s, which reproduces the slow path of 779d S6 (1781-1814 s). The test guards the fix.
- Part (a) is correct by reading: the condition is a strict superset of the old `last?.own === this.own`. It renews whenever the held package went to any lead, so a new lowest id never receives a package an earlier lowest id already saw.

D2: the text is accurate. SECURITY.md now says the key is held only if a heartbeat was verified while sharing a group, that the old leaf's key stays after a reload, that unseen heartbeats of the previous tab still count (only the server can supply them), that a verified heartbeat counts as heard, and that a sign older than ten minutes is none. Each clause matches beatOutside, beatNow and refreshRoom (beats are dropped for ids no longer hinted).

D3: fixed. Line 93 drops a sign older than ASK_MAX_MS (600 000 ms). The age is measured from the time the sign was received, not from the time the heartbeat was signed. The new test fails without the check, so it is meaningful. The check is not a security boundary: a server can deliver a held-back genuine heartbeat now and make a fresh sign (the same bounded supply 779d analysed: one record per id, monotonic time). Heartbeats come every 5 minutes, so a live lead keeps its sign inside the window even if one heartbeat is dropped.

D4: the text is accurate. "A package made by a member that holds no group (after a start-over or a removal, or when it offers again)" matches the rekey paths (recoverNow, reofferNow, resend, removal).

## Findings

### SHOULD-FIX

SF1. The D1(a) change at web/src/lib/media/groupMerge.ts:96-98 is untested.
- Evidence: with the old rule restored (`const offered = last?.own === this.own`), all 36 tests pass. The part is reachable only when the lead changes while an ask for the previous lead is still on record; the new room test passes through part (b) and never reaches it.
- The code is correct by reading, but a refactor could remove it silently.
- Suggested test: with twoGroups() from groupMerge.test.ts, let carol offer to the lowest id, remove that id from the roster, let the next id become lowest after a fresh sign, and assert that the second package differs from the first.

### NOTE

N1. `heard` is never cleared, and line 151 widens how an id enters it (heard is only added to: group.ts:82, groupMerge.ts:151).
- A hung lowest id that is heard but outside my tree stays the room's lowest id. lead() (groupMerge.ts:43-46) then points at it forever, no sign ever arrives, and no merge toward the next live lowest id is attempted.
- Before the delta a handshake that parses was needed; now a verified heartbeat also suffices. A server can supply one for a once-live id, but a server can already forge a handshake for any id, so it gains nothing. The honest path exists too: an id sends one heartbeat and hangs while the roster still lists it.
- This is the "hung lowest" family. group.hungTab.test.ts covers only the tree-member case. It needs a connected but frozen tab and a split at the same time; I did not reproduce it. It is not a regression of anything the author claimed.
- Possible fix: expire `heard` for ids outside the tree after a silence much longer than absentMs.

N2. An old commit-shaped sign now expires after 10 minutes (groupMerge.ts:93). A member that was in a group of one, or waiting out rivals.waitMs, when that commit arrived waits for the next sign, which can be a commit up to a 30-minute rotation later. SECURITY.md says "at most one rotation period". Bounded, no safety matter.

N3. groupMerge.ts:96-97: every offer to a new lead builds a new key package. Offers are bounded per lead by `asks` (the wait doubles from 60 to 600 s, entries are kept until the wait plus 10 minutes after the id leaves) and need a newer sign each time. I found no regrouping loop.

## Adversarial scenarios considered

- Malicious or hung lowest id: costs one offer per sign, then nothing. See N1 for the heard case.
- Hostile relay (reorder, duplicate, drop): sign, age and wait are all kept per id and are monotonic. A duplicate or an older heartbeat gives no sign (`beat.at <= known.at`, line 147). Delivering an unseen genuine heartbeat still makes a fresh sign, as in 779d, bounded by the wait. Per-recipient drop and reorder on top of the new line were not re-tested.
- Removed member returning: the asks entry survives leave and return, so the wait does not reset; line 68 is unchanged.
- Key package or Welcome used twice: the renewal means a package offered once goes to the next lead only as a new package, so a welcome built on the old one opens nothing. This widens the case in which an in-flight welcome from a previous lead is dropped; that is liveness only, and the lowest lead's own welcome follows.
- Endless regrouping: see N3 and the sweep result.
- Reload: the old leaf key stays, the new tab's heartbeats stay unread, only a commit or welcome counts. Documented in the new SECURITY.md text.

## Not checked

- Two tabs of one member: I did not trace mediaUserId to see whether tabs share an id.
- Mixed old/new rooms, and a truly frozen tab that wakes (the fixture can only cut a tab off or close it).
- beatElsewhere and startEpoch: unchanged lines, not re-verified.
- The rest of the PR (groupVet.ts, group.ts, the Go side): only the delta from the reviewed head was reviewed.
- Real browsers, WebRTC or hardware: nothing was run on them.

## Paths

- web/src/lib/media/groupMerge.ts (lines 93, 96-98, 151)
- web/src/lib/media/groupMerge.test.ts
- web/src/lib/media/groupMerge.room.test.ts
- SECURITY.md (lines 824-855)
