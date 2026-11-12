# Review: the committer waits for newcomers before it removes a silent member

Branch under review: `msch128/followup-churn`, the three commits on top of `msch128/followup-protocol`
(`c1aa9a18c` checker, `49f495562` fix, `834815a42` budget and settle table). Read-only review.

## Verdict: MERGE AFTER FIXES

The change does what it claims on the simulator and does not weaken any receiver rule. Measured: the full
sweep (seeds 1..400, both relays) passes against the new budget (only hostile 109 fails), and the refusals of
removals by receivers fall from 4291 to 1003 over the same 800 runs. But:

- it fixes the most common trigger (a receiver that joined by a welcome), not the cause. The cause is that the
  committer and each receiver judge silence by different baselines, and some baselines the committer cannot
  know (a receiver's sleep, a same-key leaf swap, a hung-committer hand-over). These still refuse removals;
- the wait is one maintenance interval too short for the hung-committer case (SHOULD-FIX 1);
- the new class has no behavioural test that fails when its key, its exclusions or its clearing are broken
  (SHOULD-FIX 3);
- SECURITY.md states a bound ("at most twice the limit") that holds per committer, not in real time.

None of these is a regression against 3.0.7, so I list no BLOCKER. I would merge after SHOULD-FIX 1, 3 and 4 and
after the wording in NOTE 1 is corrected. The design is a mitigation, not a cure (see the design note).

## BLOCKER

None found. I looked hard for a path that makes things worse than 3.0.7 and found none; the nearest is
SHOULD-FIX 1, which is the same refusal 3.0.7 already had.

## SHOULD-FIX

### 1. The wait is shorter than the patience of the committer skip, so a removal of a hung lowest id can still be refused

`groupEpoch.ts:254-259`: the committer waits `absentMs + max(10, maintenanceMs)` after the newest entry.
`groupBase.ts:201-204`: a receiver skips a silent lowest id as committer only after
`absentMs + 2 * max(10, maintenanceMs)` by its own baseline. A newcomer counts the lowest id as alive from its
join, so for it the lowest id is still the committer until join + limit + 2 intervals.

Sequence: the lowest id H hangs. A newcomer R joins at time T (taken from a welcome of a delayed or earlier
commit; R's baseline for H is T). The next-lowest member K becomes committer by its own record, holds until
T + limit + 1 interval, and commits the removal of H on the first tick after that. R is still in the window
(T + limit + 1 interval, T + limit + 2 intervals) in which it regards H as the committer, so `validator`
(`group.ts:349-352`) rejects K's commit as "not from the committer" (any commit with proposals). K is one epoch
ahead, its next commit is "ahead" for R, R starts over. The premature-removal check is not even reached.
Because K's first tick after the wait always falls inside that one-interval window, this is the normal outcome,
not a rare one. In the census (see "What I ran") the "not from the committer" rejections went from 175 to 189
(no improvement), while premature refusals fell by 77 percent.

Fix: make the wait `absentMs + 2 * max(10, maintenanceMs)` plus one tick, or at least the patience of
`committer()`. Share one constant between the two places, so they cannot drift apart again.

### 2. A receiver's own sleep resets its baseline and the committer cannot know; the simulator's compressed clocks hide it

`groupEpoch.ts:211-214`: on waking, a receiver sets every `alive` entry to now. `sleptRecently()`
(`groupBase.ts:325`) relaxes the vet rule only for `2 * maintenance + 60 s` after the wake, 2 minutes in
production (`MAINTENANCE_MS` 30 s). The simulator uses `absentMs` 30 s, `maintenanceMs` 2 s, so its relaxation
window (64 s) is longer than the whole limit and always covers the refusal; in production the window is 10 times
shorter than the limit. Sequence in production: member X goes silent at t0; receiver R sleeps from t0+5 min to
t0+8 min; at the wake `alive_R(X)` = t0+8; the committer (awake, no newcomer) removes X at t0+20; R computes
12 minutes of silence, refuses (needs more than 19.5), is one epoch behind and starts over at the next commit.
Entries cannot see this because the sleep is not in the shared history. I did not build a unit test for it
(not run); the reasoning is from the code, the cited lines, and the constants.

Fix options: while `sleptAt` is set, count that member as not judgeable until `absentMs` has passed since the
wake, but let the committer's removal count as accepted if the sender is the committer and R was asleep for more
than the sleep limit at any time inside the last `absentMs`; or lengthen `sleepLimit()` to the absence limit
(then a removal on the committer's word is accepted for a long time after any sleep, which is a weaker rule and
needs a decision). At minimum document it, and make the simulator's sleeps long enough (relative to the limit)
to reproduce it.

### 3. Entries has no behavioural test; five mutants of the fix survive the new unit tests

`group.removal.test.ts` has three tests of the committer's timing only. I mutated the code one change at a time
and ran `group.removal.test.ts` and `groupSim.mutation.test.ts` (the latter is about the lowest-id tie-break and
is unrelated to this change):

| Mutant | Result |
| --- | --- |
| fix reverted | killed (2 of 3 tests fail: 31 instead of 52, 31 instead of 61) |
| `2 * absentMs` changed to `1 * absentMs` | killed |
| `anyWithin` never true | killed |
| Entries ignores the signature key (a reload does not reset) | SURVIVES |
| the silent ids are not excluded from the newest-member test | SURVIVES |
| the committer's own id is not excluded | SURVIVES |
| departed ids are not forgotten (`delete` removed) | SURVIVES (3 tests pass) |

So the "key on signature key", "exclude silent and self" and "clear on leave" behaviours are untested. Add:
a reload of a member (new signature key) re-arms the wait; a silent newcomer does not hold for itself; a member
that left and came back with a new key re-arms the wait; and one end-to-end test where a receiver that joined
at the same time as the entry actually accepts the removal (the current tests only check when the committer
commits, not that the receiver accepts it, which is the point of the change).

### 4. Entries does not re-arm for a leaf swap that keeps the signature key, while the receiver's baseline does

`groupEntry.ts:30` resets only on a new id or a new signature key. `receiveReturningMember` (`group.ts:122-134`)
swaps a member's leaf with a package under the same signature key whenever the committer is the lowest id: the
member is in another group and offers itself, or it sent a second package. The member then takes a welcome and
is a newcomer by `markNewMembers` (all baselines now) while the committer's record still says "in since the
first add". The committer then removes at the old time and the swapped-in member refuses.

This is exactly what seed 369 does (honest relay): the committer got two more packages from m4 under the leaf's
key at 3.87 s and 3.93 s, after m4 was already in its tree (added at 3.47 s), and swapped the leaf at 6.555 s
(`commit@5[remove,add]`). I answer "why did the committer swap in the same leaf" with that: a package that
crossed the add on the wire is treated as a returning member (`group.ts:104-109`, `same` only blocks it when a
lower id exists). The checker commit is therefore consistent, but see NOTE 4.

Fix: key Entries on the leaf (hpke or the leaf node's serialised bytes, or the epoch where the leaf index was
last filled with a different leaf), not on the signature key. The leaf encryption key changes on a swap and on a
reload, and not on a self-update of another member only if it is the member's own leaf (self-update changes the
hpke key too, so compare the signature key and the leaf's `parentHash`/index, or record the entry in
`replaceMember`'s result: the committer knows which members it added in a commit it made).
Simplest: when the committer creates or processes a commit that adds an id, set the entry for that id then,
regardless of whether the id was already in the tree.

## NOTE

### 1. SECURITY.md: "at most twice the limit" is a per-committer bound, not a real-time one

The committer's override is `now - alive(X) > 2 * absentMs`, with `alive` taken by that committer
(`groupEpoch.ts:259`). `alive` is reset by the committer's own sleep (`groupEpoch.ts:213`), by its join (a
committer that joined by a welcome counts everyone alive from then), by X's leaf swap, and by any
sign of life. With a change of committer (the hung lowest id is skipped, or a lower id arrives), the new
committer starts a new 2x count of its own, so the real-time bound from the last sign of X can exceed 40
minutes. This was already true of the 1x rule in 3.0.7 (the 20 minute bound is also per committer). The text in
SECURITY.md ("at most twice the limit (40 minutes, ...)") should say "by the committer's record, in each
committer's term". It also says nothing about a malicious lowest id: such a committer can keep any member in
the group for as long as it likes by never committing, which the document already says for other powers; say
it here too, since the bound sentence reads as unconditional.

### 2. What a gone member can do with 40 minutes instead of 20

The wait applies only to members that are connected (in the room) and silent. A member that left the room is
removed at once by the roster path (`commitRemoves`, `groupEpoch.ts:365-374`), unchanged. A connected, silent
member keeps its epoch key and the previous epoch's key; while it is still connected to the SFU it receives the
forwarded frames and can decrypt everything sealed in epochs it holds. So the extra 20 minutes are 20 minutes of
readable media for a client that is connected and does not send heartbeats (a hung tab, a frozen mobile browser,
or a client that chose to stay silent). A client that wants to listen can already avoid removal by sending
heartbeats, so a hostile member gains nothing it did not already have; what is gained is only for a stolen or
hung session that the rightful user cannot answer for. SECURITY.md says "keeps its epoch keys" but not that
they still open frames while it is connected; add one clause. Also epochs rotate at 30 minutes, so the silent
member sees at most one rotation of media (a rotation is committed by the committer only if no removal commit
replaced it; check that a held removal does not also suppress rotation: `committerMaintenance` returns after a
removal commit, and with a hold it falls through to rotation, so rotation continues; correct).

### 3. A malicious or flapping member can hold the wait open, but only up to the bound

A member that reloads (new signature key, so a new entry) every less than limit + one interval keeps `hold` true,
so another silent member stays in until it has been silent for 2x. The bound holds in that path. The reloading
member gains nothing it could not already do (a silent member stays in anyway by sending heartbeats), and reload
swaps are rate-limited by `swaps` (a `Backoff` that reads `Date.now()`, `backoff.ts:14`, as do `rivals` and
`resets`; a backwards wall-clock step stretches their waits, a forward step shortens them; the new rule itself
reads only the monotonic `clock.now()`, so a wall-clock step cannot move the hold). The record is lost when the
committer reloads: a fresh Entries and fresh `alive` mean the committer waits the full limit plus one interval
from its own start (not longer than 3.0.7 plus one interval, since `alive` is also fresh).

### 4. The checker commit (invariant 4) keeps the older alive time after a same-leaf swap

The change (`groupSimChecks.fixture.ts:208-215`) stops resetting `seen.alive` when a commit removes and re-adds
an id and only deletes `seen.beats`. A real early removal that follows a same-leaf swap is still seen as early,
since the oracle keeps the older, smaller time, i.e. it can only declare more removals premature, never fewer.
To be blind, a removal would have to be early by the member's real time but late by the oracle's older time,
which cannot happen when the oracle's time is older. The oracle is therefore not blinded; it is sharper in the
direction of failing. The opposite risk is a false failure when the group did reset (the hpke key changed in the
swap): then the group's receivers have a newer alive time than the oracle and a legal removal looks late, which
is harmless. A case that would hide a real defect: none constructed. But the group resets `alive` only when
`hpkePublicKey` differs (`trackLeaves`), and a swap built from a package under the same signature key normally
has a new hpke key; so in the usual swap the group resets and the oracle does not, and the oracle now accepts a
removal the receivers would refuse (the receiver-side check is by its own alive, the oracle's is older, so the
oracle says "late enough" while the receiver says "too early"). That can hide a refusal (not an early removal)
in the oracle: the invariant it protects (nobody present is removed early) is checked against the receivers'
own refusals anyway. I see no way to construct an early removal accepted by receivers and hidden from the oracle.

### 5. The 10 seeds moved to the always-on test are reasonable

The 8 that never settled before (honest 125 and 258; hostile 107, 150, 194, 292, 323, 362) plus the two slowest of
those that settled (honest 119 at 3062 s, 162 at 2277 s). All 10 pass (full file run: 79 passed, 1 expected
failure, 44 skipped). The budget file lists only hostile 109, consistent with `groupSim.known.test.ts`
(one `it.fails`), with the settle table (a row exists for every seed of the budget) and with my full sweep.

### 6. Repo gates on touched files

ESLint on `groupEntry.ts`, `groupEpoch.ts`, `groupBase.ts`, `group.removal.test.ts` and all `groupSim*` files:
no findings. `vue-tsc --noEmit` for `tsconfig.app.json` and `tsconfig.tests.json`: clean. Line counts: `groupEpoch.ts`
398 (limit 400, now two lines of headroom), `groupBase.ts` 341, `groupEntry.ts` 40, the longest test file 131.
`absentProposals` is one dense line of about 190 characters (`groupEpoch.ts:259`); split it for readability, and
because `groupEpoch.ts` is at the limit, move the hold decision into `Entries` (for example `Entries.holds(...)`),
which also makes SHOULD-FIX 1 a one-place change.

## Hostile seed 109: a reshuffle of an existing weakness, not caused by the waiting rule

I ran it before (fix reverted in a scratch checkout) and after: agreed 62 s after the end of interference before,
415 s after; 52 and 49 "keys none" lines. The trace shows:

- Both runs split into two groups during the chaos phase and the two removed each other's members at the same
  moments (106.4 s and 107/108 s); the removals at those times are identical in the two runs, and in both
  the two groups merged by about 137 s.
- In the "after" run, at 136.5-137.1 s the lowest id m1 added the other group's members in two commits. Members
  3, 4, 5, 6 and 11 never log a `keys` line for them (they did not take the welcome: the rival-welcome allowance
  of one per minute per sender, doubling to ten minutes, `rivals.allow`, had been used by earlier merges), stayed
  in their own group, and m1 removed them at 169.0 s as silent (32 s after the add, which is the limit plus one
  interval: the waiting rule delayed this removal by about 2 s and did not cause it). The remaining merges then
  run at 169, 243, 291 and 487 s: the doubling back-off.
- Under the old timing the same merge landed in a state where the allowance was still free. That is luck of
  timing in a back-off that doubles per sender.

So: not a regression of the waiting rule, a pre-existing weakness (the rival allowance and its doubling make a
second and third merge of the same pair slow). I did not change code to prove it by removing the back-off; the
claim rests on the traces (kept outside the repository).

## Seed 70 from the independent harness (roster lies, honest relay): the oracle, not the fix

I overlaid the harness files of `msch128/hunt-lane2` on the churn branch (scratch checkout, not committed) and
ran `liar` on honest seed 70: fails with the fix ("took a second renewal from m2 after 10940 ms"), passes with
the fix reverted. Cause, from the trace and code:

- m7 took a renewal of m2 at 75.000 s in one group (`commit@9[]`), then at 75.14-75.15 s took three welcomes
  from m1 and moved into m1's group (`keys 11..13`). `joinFromWelcome` for a rival welcome calls `discard()`
  (`group.ts:161`), which clears `updates` (`groupBase.ts:259`), so the group correctly forgets the renewal
  of the group it left.
- The oracle clears its per-tab `renewals` only when it is told of an epoch with no verification
  (`groupSimChecks.fixture.ts:129-135`), and a rival welcome goes from one group to another in one step, never
  through "keys none". So the oracle keeps the 75.000 s renewal and charges the 85.94 s renewal in m1's group
  (a different group) as a second one inside the 18 s gap.
- Between 75 and 86 s there are no removals in the trace; m2's renewal at 85.94 s is an ordinary due renewal.
  The fix only moved the schedule, and the oracle's memory across a group replacement turned that into a
  failure. Fix on the oracle: clear `seen.renewals` when the member's group id or epoch lineage changes (a
  welcome), not only on "keys none". I did not verify the key-package replay result for 109.

## Design note

**The rule.** The committer (the lowest id of the room, with the hung-id skip) removes a member that has shown no
sign of life for more than the absence limit by the committer's record, but only once every other member (not
itself, not the silent ones) has been in the tree for the limit plus one maintenance interval. A member that is
silent for twice the limit is removed regardless. Receivers are unchanged: they refuse a removal of a member
whose last sign of life, by their own record, is within the limit minus one interval, unless they slept, are the
removed member, or the commit replaces the leaf.

**Information used, and where it differs.** The committer uses its own `alive` (last sign per member, from
heartbeats and commits it received, reset by its own sleep and by its own join) and its new `Entries` (when it
first saw each id, or each signature key, in an epoch of its tree). Each receiver uses its own `alive`, whose
baseline for every member it did not hear is the moment it joined (welcome), woke up, or saw that member's leaf
swapped. These are different quantities. The new rule makes them agree in one case, a receiver that joined while
the committer was in the group: the committer's entry for it is earlier than the receiver's join by the delivery
delay `d` of the welcome, and the margin is two intervals minus `d` (60 s in production). It does not make them
agree when a receiver's baseline moves for a reason the committer cannot see (sleep, a rival welcome, a leaf swap
under the same key, a late welcome with `d` over the margin), or when the question is who the committer is
(SHOULD-FIX 1).

**What an attacker gains or loses.**
- Hostile relay: it can still drop or delay anything, so it can still cause refusals (delay a welcome by more
  than the margin, or deliver one heartbeat of the silent member to a receiver only). Before, an honest delay of
  a few seconds was enough; now it needs more than a minute for the join-time case. It cannot cause a removal
  that receivers accept early. It can stretch a silent member's stay up to the bound by making newcomers appear
  (it cannot forge a join, only delay one).
- Malicious member: by reloading less than once per 20 minutes it keeps the hold open; a silent member then stays
  up to 40 minutes. It gains nothing a heartbeat would not give it. It cannot remove a present member, add a
  second leaf or return after removal (add and vet rules untouched).
- Malicious or hung lowest id: unchanged in power (keeps members in by never committing; can remove a
  member that looks silent to it). A hung one is skipped as before; the extra wait applies to its removal too.

**What "premature" protects against.** A removed member can join again with a new key package, so the refusal does
not protect confidentiality or membership; it protects availability and agreement: a committer (the lowest id, or
a malicious one) cannot cut a present member, and a group that forks on a removal is not silently taken by the
members who believe the removal. The cost of an early removal is a regroup for the removed member, plus the
split described above.

**Could a rule depend only on the shared history?** Yes, with a wire change. Silence would be judged by epochs the
whole group saw, not by clocks: a removal in two steps. Commit A records "X suspected" (a proposal or an extension
that every member sees in the tree history); any member that hears X in between would object (a commit that
clears it); commit B removes X only if at least N epochs or a number of committer-signed ticks have passed since A
by the epochs' own count, which every member, newcomers included, reads from the same history. Newcomers would
inherit the suspicion state from the tree, so there is no baseline that depends on the join time. Cost: a new
proposal or extension type (old clients reject any commit with a proposal other than add and remove, so it must be
gated on a capability bit and old committers remain), an extra epoch per removal (more commits, more welcomes),
and the epoch-count rule must still be bounded in real time (rotation is 30 minutes, so it needs its own ticks).
It would also remove the premature rule as a clock comparison. It is the cure; this change is a mitigation that
makes the common case agree and leaves the baselines differing.

## What I ran

- Full sweep on the branch: `SIM_SWEEP=1 vitest run groupSim.sweep.test.ts`, one worker, nice 10: 2 of 2 tests
  passed in 388 s (hostile fails only 109, honest none; the "budget passes now" assertion also holds).
- The pre-fix comparison was not done as a separate full sweep on `followup-protocol`; instead I reverted only
  `absentProposals` in a scratch checkout of the branch and measured the refusal census (below) and seed 109 on
  both. I did not run shards of the sweep on `followup-protocol`; the 17+26 failing seeds of its budget come from
  its own committed fixture.
- Always-on tests: `groupSim.test.ts`, `groupSim.mutation.test.ts`, `groupSim.known.test.ts`,
  `groupSim.settle.test.ts` together: 79 passed, 1 expected failure, 44 skipped (the settle and sweep suites are
  opt in). `group.removal.test.ts`, `groupSimChecks.test.ts`: 10 passed.
- Mutants: see SHOULD-FIX 3.
- Refusal census (instrumentation in a scratch checkout, removed): all 800 runs (seeds 1..400, both relays).
  Premature-removal refusals: 4291 before the fix, 1003 after; "commit with proposals from a non-committer"
  rejections: 175 before, 189 after. The remaining 1003 include the chaos phase, where drops make refusals
  legitimate; I did not separate them.
- Seed 109 hostile, before and after (agreed 62 s vs 415 s) and traces; seed 369 honest trace; lane2 seed 70
  (honest and hostile, with and without the fix).
- ESLint and `vue-tsc` on the touched files and projects.

## What I did not check

- A real (non-simulated) timing run, or a unit test of the receiver-sleep refusal (SHOULD-FIX 2) and of the
  hung-committer window (SHOULD-FIX 1): both are from the code and the constants. The census numbers support
  SHOULD-FIX 1 only indirectly.
- Separation of the 1003 remaining refusals by cause (chaos phase, sleep, swap, late welcome).
- A full sweep on `followup-protocol` (only the revert comparison above), and the lane2 power sweeps other than
  seed 70 (including the replay result for hostile 109).
- Mixed rooms with real 3.0.7 clients: reasoned from the code (committer-only change, no message change), not
  run.
- Go tests, e2e, the desktop client; the rest of SECURITY.md beyond the media section.
- The worktree used for this review is removed after the report is written; nothing was committed or pushed.

---

# Second round: `msch128/followup-churn-2`

Reviewed: the three top commits (`8bff70f68` plan option for the lifetimes, `530edd03d` rework of the wait and of
`Entries`, `82776a8a5` renewal fix), on top of main (`56f6a4101`).

## Verdict: MERGE

No BLOCKER and no SHOULD-FIX that has to precede the merge. Two small follow-ups (SHOULD-FIX 1 and 2 below) can
go in the same change or right after. The reworked removal fix closes my first-round items 1, 3, 4 and 5, and
the new renewal fix is correct. I also have to withdraw a verdict of the first round (next section).

## Correction of my first-round verdict on seed 70

I judged the failure of the roster-lies harness (honest seed 70) an oracle artefact and said a rival welcome
cleared the group's renewal memory. That was wrong. In that trace the welcomes at 75.14 s are the committer's
welcomes for other joiners, not a rival welcome for m7; m7 stayed in the same group, so `discard()` never ran. The
author is right: the defect is on the group side.

Reproduced on main: I added the new unit test of `82776a8a5` (`renewal.test.ts`, "counts a renewal that won
against a rival one") to a checkout of main plus the harness (`msch128/hunt-lane2`) and it FAILS there
(`expected 4 to be 3`: a second empty commit from Bob, whose renewal Alice went back to, is taken at once); with
the fix it passes (full `src/lib/media` run: 393 passed, 48 skipped, 0 failed). Seed 70 itself passes on main
(its schedule never produces the conflict there; the removal fix only moved it), so the defect is real on main and
reachable only through a schedule, not through that seed.

What it is: `mayUpdate` (the record `updates` that holds a member to one renewal per `(rotate - jitter) / 2`)
is set when a renewal is taken in `processCommit`/`adoptUpdate`. `switchConflictState` takes a winning renewal
(the lower id's) after a rival one was already taken for the same epoch, but did not record the winner. So after
a race of two renewals the lower id could send another at once and have it taken.

What a member or relay can do with it: a member that races its renewal against another member's gets one extra
renewal accepted per such race (two in a row instead of one per about 14 minutes). Each renewal is a commit and a new
epoch for every member (key schedule, welcome-free, previous key kept 30 s), so the cost is one extra epoch per
race, paid by all members; no confidentiality effect (a renewal only replaces the sender's own leaf key and cannot add
or remove anyone). A relay cannot make an honest member send a second renewal, so it gains nothing alone; it can
only create the race that makes an honest member's single renewal count differently. The amplification needs a
second member whose renewal arrives first each time, so it is a minor, bounded epoch-churn lever, not a rate-limit
bypass in bulk.

Is the fix correct: yes. It records the winner only when the winner is a renewal (a committer's commit is not an
update), at the moment it is taken, like every other renewal. "The loser stays counted": the loser's renewal was
recorded when it was first taken and stays; it can refuse an honest renewal of the loser only before about 14 minutes
have passed. The loser's own group goes back to its old leaf, and `trackLeaves` then sees its own hpke key differ
and draws a fresh `updateAfter` (about 30 minutes) and a fresh leaf age, so the loser's next renewal comes about 30
minutes later, well after the other members' window; it is not refused and no rotation is stalled (rotations are the
committer's commits, which do not use `updates`). The one visible cost is that the loser's renewal is delayed by one
period, which is also true without the record.

## Findings

### SHOULD-FIX 1. `added` is a field written by the validator and read at the next epoch start

`groupBase.ts` (`protected added`), set in `validator` (`group.ts`) and in `commit()` (`groupEpoch.ts`), read and reset
in `startEpoch`. If a commit passes the validator but is not adopted (`onlyRenewsLeaf` fails, the epoch is refused) or
a own commit fails, the ids stay in `added` and are stamped as new at the next epoch start of any kind, even a welcome.
The direction is safe (the committer waits longer, bounded by 2x), so it is not a defect, but it is an easy source of
a later bug. Pass the set to `adopt`/`startEpoch` as an argument, or reset it in the failure paths.

### SHOULD-FIX 2. "no clear on discard" survives: add a test

Of the mutants I ran against the new tests (below) one survives: `Entries.clear()` removed from `discard()`
(`groupBase.ts`). Without it, after a start-over the ids that were in the old group keep their old entries in the new
one, so a committer in the new group would wait for less time than its receivers do (the receivers' baselines are all
at their join). Add a test: a member discards its group, joins another one, and the committer's wait counts from the
new group's start.

### NOTE 1. The census does not fall, and the new wait is still right

My first-round item 1 was that the wait was one interval shorter than the hung-committer patience. Now `holds` waits
`patienceMs + maintenance` with `patienceMs` shared with `committer()` (`groupEntry.ts`), so for a newcomer R joined at
`T` (committer's entry `T' <= T`) the removal is made at or after `T' + patience + interval` while R takes the silent
lowest id for the committer only until `T + patience`: the margin is one interval minus the delivery delay of the
welcome, where it was minus one interval. The unit test "a member taking over from a lowest id that went silent ... and
the newcomer takes the commit" drives exactly this end to end; with the old wait (`absentMs + interval`) it FAILS, and
with the old patience it fails (mutants below). So the change is right and tested. The census (rejections as from a
non-committer 188 before and after) does not move because it counts every cause together: roster views that differ
between members, sleepers, members that came in by other paths. The author's reading (sleepers and roster views) fits
my own reading of the code; I did not separate the causes again.

### NOTE 2. Hostile seed 149 at production ratios: a pre-existing defect, a long split after a refused swap

Reproduced, and identical with the wait disabled and with the renewal fix reverted (I disabled the wait with an env
switch in a scratch copy, then also reverted the group fix; the violation text and time are the same: 141.579 s).

One-command repro (not the exact `it` of the author's file, which loops over seeds 1..150; this runs the seed alone in
a scratch test, about 1 s): a test that calls
`runSeed(149, { relay: 'hostile', timing: { rotateMs: 1_200_000, rotateJitterMs: 120_000, absentMs: 800_000, heartbeatMs: 200_000, maintenanceMs: 20_000 }, agreeWithinMs: 7_200_000 })`.
With the author's own command (`SIM_PRODUCTION=1 SIM_PRODUCTION_SEEDS=149 vitest run groupSim.settle.test.ts`, hostile
case) it should reproduce as well; I did not run that exact command.

What happens (trace): at 80.659 s the committer m1 receives a second key package of m4, which is already in its tree
since 76.579 s; only m1 gets it (the hostile relay delivers it to one recipient). m1 swaps m4's leaf (`remove,add`,
epoch 10 to 11). m6 and m7 take the commit; m3, m4 and m5 do not (most likely because they never saw that package
relayed, the add rule `wasRelayed`; I did not confirm the exact rule). m1 is one epoch ahead of three of seven members.
Nothing recovers them until the next commit or a heartbeat-based detection: with the 60 s hold of the simulator the
run fails at 141.6 s; with the hold set to 50 minutes the three members start over at 941 s (`keys none`), so the
split lasted about 14 minutes at production ratios (three of seven members without the group's key, so no media
between the two parts). It is not the removal fix and not the wait (it happens at 80 s, long before any silence).
Severity: only a relay that delivers selectively (a hostile one, which can drop all media handshakes anyway) can do it,
so the security impact is low; the availability impact, a long split after a single refused commit when the next commit
is half an hour away, is worth its own finding, and the simulator's fixed 60 s hold hides it at the default lifetimes.
Suggested follow-up: a member that rejects a commit it cannot judge should say so (or ask) rather than wait for the next
commit; or the committer should repeat the swap's commit for members that did not move (heartbeat for the old epoch).

### NOTE 3. Sleep: the experiment at production ratios supports a limited claim

My first-round item 2 (a receiver's sleep resets its baseline). The author did not change behaviour and measured with
production ratios: every seed of 1..150 on both relays settles within two hours of virtual time except hostile 149
(NOTE 2, which is not about sleep). The refusals are one per sleeper and silent member and start nobody over in a
loop. That is what the code suggests; the cost per event (a sleeper that refuses one removal, then starts over and loses
the keys for a re-join) remains and is now stated in SECURITY.md ("may still refuse one removal"). Honestly left
open, with the evidence. Note that the simulator's sleeps in that experiment are just over the sleep limit; the
refusal depends on the wake being within the absence limit of the removal, not on the length of the sleep, so the
experiment does cover it.

## First-round items

- 1 (wait vs patience): closed (NOTE 1).
- 2 (receiver sleep): left open honestly, documented and measured (NOTE 3).
- 3 (Entries tests): closed except the discard mutant (SHOULD-FIX 2). Six mutants I ran were killed: old wait,
  old patience, `Entries` ignoring a swap (not an add), self not excluded, silent ids not excluded, departed ids not
  forgotten. Survives: no `clear()` on discard.
- 4 (same-key swap): closed: the entry is stamped for every id a commit adds, including a swap; the new
  `groupEntry.test.ts` pins "even for the same leaf, and not one that only renewed its own".
- 5 (wording): closed. SECURITY.md now says the bound is each committer's own and for an honest one, that a
  silent connected member can decrypt every epoch it holds for up to 40 minutes, that a member that left is removed at
  once, and that a sleeper may refuse one removal.
- 6 (limits): closed. `groupEpoch.ts` 340 lines (was 398), `groupCommits.ts` 66, `groupEntry.ts` 72, `group.ts` 392,
  `groupBase.ts` 344, the longest new test 176.

## What I checked this round

- The move to `groupCommits.ts`: the block from `commitAdds` to the end of the class in the parent commit and in the new
  file diff to nothing (identical text, only the class and imports differ). `commit()` is the only change in behaviour
  (sets `added`).
- Full sweep, foreground-equivalent, `SIM_SWEEP=1 vitest run groupSim.sweep.test.ts --maxWorkers=3` on the branch:
  2 of 2 passed in 366 s, against an EMPTY budget (hostile 109 passes, no seed outside it fails). The budget fixture lists
  no seed and the known test asserts it.
- All of `src/lib/media`: 36 files passed, 1 skipped, 393 tests passed, 48 skipped (opt-in sweeps), including the
  tie-break mutation tests and the settle table.
- Mutants on `Entries`/wait (list above): the new tests kill all but the discard one.
- ESLint on `src/lib/media`: no findings. `vue-tsc --noEmit` for `tsconfig.app.json` and `tsconfig.tests.json`: clean.
- The renewal test on main plus harness (fails) and with the fix (passes); seed 70 on main (passes).
- Hostile 149 at production ratios: with the wait, without it, without the wait and without the renewal fix, and with
  a 50 minute hold (heals at 941 s).

## What I did not check this round

- The census of refusals on the new branch (taken from the author's numbers, 188 and 1012).
- Which rule exactly made m3, m4 and m5 refuse the swap in seed 149.
- A second full sweep at production ratios (150 seeds per relay, two hours of virtual time each): only seed 149 was
  rerun.
- Mixed rooms with real 3.0.7 clients.

---

# Third round: commit `9c36122ac` (collection branch, parent `423d61c36`)

## Verdict: MERGE. Both round-2 should-fix items are closed; one small test gap is left as a note.

## SHOULD-FIX 2 (no `clear()` on discard): closed

The new test (`group.removal.test.ts`, "a member that starts over into a group with the same members") discards Bob's
group, lets him into a new one 100 s later and requires that he counts the others as new from then. With
`this.entries.clear()` removed from `discard()` in my own run it fails (`expected false to be true`); with it
it passes. The mutant is killed.

## SHOULD-FIX 1 (stale `added`): closed, no path left that I can find

`added` is written in exactly three places: the start of `validator` (reset), the end of `validator` (set), and
`commit()` (set from the proposals). `validator` is only ever handed to `processPublicMessage` from `processCommit` and
`conflictNow`, and both now clear `added` in a `finally` that wraps everything, including the early returns
(`removedFromGroup`, `recoverNow`, a rejected leaf rule, a thrown error). `commit()` clears it in a `finally`
around the whole body. `startEpoch` still reads and resets it, inside the `try` in each path (`adopt` runs inside
them), so a commit that starts an epoch still passes its ids to `Entries`, and one that does not leaves none behind.
Group operations are serialised through the queue, so no other task can read the field between the set and the clear.

The split of `commit()`: `groupEpoch.ts` changes by nine added lines only (the diff of `groupEpoch.ts` against the
previous round is additions, no deletion); the old body of `commit()` is now `createCommitWires(previous,
extraProposals, renewal)` with the same text, and the only code left in `commit()` is the early return, the set, and the
`try`/`finally`. No behaviour changed.

## NOTE: no test pins the clearing of `added`

I removed the `finally` in `commit()`: all of `src/lib/media` still passes (394 passed). So the item is closed by
reading, not by a test. The effect of a stale value is only a longer wait for the committer, bounded by the 2x override,
so I do not ask for one; if one is wanted, make a commit fail after `this.added` is set (for example a failing
`createCommit`) and check that the next epoch start, by a welcome, does not stamp the ids.

## What I ran

- Mutant, no `Entries.clear()` in `discard()`: the new test fails; restored, passes.
- Mutant, no `finally` in `commit()`: not caught (above).
- All of `src/lib/media`: 36 files passed, 1 skipped; 394 passed, 48 skipped (opt-in sweeps).
- `SIM_SWEEP=1 SIM_SHARD=0/4 vitest run groupSim.sweep.test.ts --maxWorkers=3`: 2 of 2 passed in 98 s (that shard is
  seeds 4, 8, ... of 1..400 on each relay, 100 per relay, against the empty budget). I did not run the other three
  shards or the full sweep; the code change touches no protocol decision, only the lifetime of one field.
- ESLint on `src/lib/media`: no findings; `vue-tsc --noEmit` for the app and test projects: clean. `groupEpoch.ts` 349
  lines, `group.ts` 397 (limit 400, three lines of headroom), `group.removal.test.ts` 208.
