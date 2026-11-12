# Review: simulator harnesses for the media group protocol (branch msch128/hunt-lane2)

Reviewed: c94174e4b (three test-only commits on main 56f6a4101), 2026-11-12, by an independent reviewer
who did not write the branch. Read-only: nothing in the branch was changed. Everything below was run in a
fresh checkout with `npm ci`, at most 3 vitest workers.

## Verdict: MERGE AFTER FIXES

The branch changes no production code, leaves every existing seed's outcome as it was, and its smoke tests
are cheap. Three things block the merge. The typecheck gate for tests fails. The roster-lie action and the
wall-clock step action never run, so the roster-lie sweep and the clock-step part of the clock sweep test
nothing. One probe file writes to an absolute path on the author's machine. All three are small fixes. The
roster-lie and clock-step numbers have to be measured again after the fix.

## BLOCKER

### B1. `vue-tsc -p tsconfig.tests.json` fails with 4 errors (main: 0)

- `web/src/lib/media/groupSim.fixture.ts:237` and `:612`: `info: WireInfo | undefined` is assigned to
  `Timer.info?: WireInfo` under `exactOptionalPropertyTypes` (TS2379, TS2375). These two are type errors only.
- `web/src/lib/media/groupSimRun.fixture.ts:95` and `:98`: `Property 'id' does not exist on type 'string'`
  (TS2339). These two are real bugs, see B2.

`npm run typecheck`, and so the pull request pipeline, goes red.

### B2. Roster lies and wall-clock steps never happen

`groupSimRun.fixture.ts:86` builds `awake` as a list of ids (`.map(member => member.id)`). Lines 95 and 98
then call `sim.lie(sim.pick(awake).id, ...)` and `sim.stepWall(sim.pick(awake).id, ...)`, and `.id` of a
string is `undefined`. `GroupSim.lie` and `GroupSim.stepWall` look the member up with
`this.room.get(undefined)`, find nothing, and return before they write their trace line.

Measured: on honest seeds 4, 9, 12, 14, 20 and 33, `liarPlan` produced 0 `lie to` lines and the `steps`
clock variant produced 0 `wall clock` lines. Sleeps did fire in the same runs (up to 5 per seed). The
author's own saved liar trace contains no `lie to` line either.

Consequences:
- `SIM_POWER=liar` (groupPowers.sweep.test.ts) and the liar smoke test (groupPowers.test.ts:25-30) never
  lie to anyone. The only effects of a "liar" run are an extra action weight, which consumes one random
  draw and so shifts the schedule (in effect a different seed), and one extra true roster at calm
  (`correctRosters`). Failures reported for "roster lies" are therefore failures of ordinary,
  re-randomised seeds. The hunt's roster-lie suspicions should be reopened on that basis.
- `stepWall`, including its `monoLost` bookkeeping, has never run. The `steps` and `all` clock variants
  exercise offsets, drift and monotonic stops only.

Fix: `sim.pick(awake)` without `.id` in both places. Add a smoke assertion that a liar run contains at
least one `lie to` line and that a `steps` run contains at least one `wall clock` line, so this cannot fail
silently again.

### B3. `web/src/lib/media/groupPowers.probe.test.ts` writes to an absolute path on the author's machine

Line 15 hard-codes a home-directory path outside the repository. When `PROBE` is set, the test writes a
trace file there. It is a debugging probe, not a harness, and this repository is public. Delete the file.

## SHOULD-FIX

### S1. No workflow runs the deep modes, and four of the six cannot fail

`.github/workflows/sim-sweep.yml` sets only `SIM_SWEEP` and `SIM_SHARD`. Nothing sets `SIM_AVAIL`,
`SIM_CYCLE`, `SIM_CLOCKS`, `SIM_POWERS`, `SIM_EXPLORE` or `SIM_CRASH`. On top of that:
- `groupAvailability.sweep.test.ts` and `groupCycle.sweep.test.ts` contain no `expect`. They only log or
  append rows.
- The deep modes of `groupExplore.sweep.test.ts:30-53` and `groupCrash.sweep.test.ts:52-77` count `failed`,
  but never assert on it.
- `groupClocks.sweep.test.ts` and `groupPowers.sweep.test.ts` do assert against the failure budget. On main
  they are red by design, though: the author's own clock rows show failures outside the budget in the
  drift, steps, all and extreme variants, and the extreme variant exists to make the oracle fire. So they
  cannot simply be scheduled either.

What does run nightly is the availability budget and the epoch-life check, because both are wired into
`groupSim.sweep.test.ts`, which `sim-sweep.yml` runs. Everything else is a measurement tool that someone
has to run by hand. That is acceptable if it is labelled that way, but the harness headers and the hunt
report present these as nightly harnesses. Either:
- make explore and crash deep assert `failed === 0`, give clocks and powers a known-failure list per
  variant or power (with `extreme` as an expected failure), and add the jobs to `sim-sweep.yml` in a
  follow-up pull request; or
- say in each header that the mode is a manual measurement.

### S2. A crash at an await boundary is in fact a reload after the step that contains it

`groupCrash.fixture.ts:44-54` schedules the reload with `sim.after(null, 0, ...)`. `GroupSim.runUntil`
(`groupSim.fixture.ts:634-650`) settles the whole step (`mediaSettled`) before it runs the next timer. So
the tab finishes the step, adopts the commit and sends its wires (it is not yet `gone` when it sends), and
only then reloads. The header (lines 1-9: "after a commit was created and before it was adopted ... nothing
it sends reaches the relay") describes something the harness does not do.

Measured on the smoke script (2 members, `scripts(2)[0]`):
- all 155 boundaries of member 1 give only 5 distinct runs, and all 232 boundaries of member 2 give only
  5 (traces compared with key fingerprints masked);
- ordinals 1 to 40 of member 1 are one identical run, a reload at t=0 after the founding step;
- the smoke test (`groupCrash.sweep.test.ts:41`, ordinals 1 to 6) therefore tests one crash point per
  member and `seen` flag, not six.

The "31,736 runs" of the deep mode are a few hundred distinct situations. Fix: in `boundary()`, mark the
tab gone at once and return a promise that never settles to the crashed tab's continuation, so the rest of
the step really is lost. Otherwise, rename the harness to "reload after each step" and deduplicate.

Add a guard against vacuity as well. If the `vi.mock('ts-mls')` wrapping ever stops counting,
`base.boundaries.counts` is empty and the smoke test passes after zero crash runs. Assert
`counts.size === 2` and that at least one run had `crashed === true`.

### S3. The whole-room recurrence detector is never used

`Recurrence` and `deep()` (`groupCycle.fixture.ts:96-154`) are used only by their unit test.
`SIM_CYCLE` and `SIM_AVAIL` use the coarse `CycleDetector`. The hunt claim "no exact cycle in 1200 s on 4
honest seeds" cannot be reproduced from the checked-in code. Wire `Recurrence` into `groupCycle.sweep.test.ts`
or remove it. `deep()` also reads protected fields of `MediaGroup` through a cast (`alive`, `resets`,
`restarting`, `pending`, `heard`). The unit test uses fakes, so renaming one of those fields breaks only
the unrun sweep.

### S4. The explorer's smoke test claims more than it runs

`groupExplore.sweep.test.ts:17-19` is titled "a room of 2, every script", but it runs only `scripts(2)[0]`,
1 of 24 scripts, with 9 schedules. All 24 scripts at one deviation among 4 points is 216 runs, a few
seconds. Run them all, or rename the test.

### S5. The availability budget does not cover join latency, and nothing forces it down

`lostMs` counts only time without keys after a member first held keys (`groupAvailability.fixture.ts:42`,
`:72`). Suppose newcomers wait 200 s for their first keys. That passes invariant 1 (240 s) and passes the
budget. Count first-keys latency as well, as its own per-seed limit.

The budget itself is a real gate, but a coarse one. The limits are 30 % (honest) and 40 % (hostile) per
seed, against a measured worst of 26 % and 37 %. The limit is 65 % for budget seeds, against 61-62 %. The
pooled limit per shard is 22 % and 29 %, against a measured worst shard of 19 % and 26 %. To fail, the
worst ordinary seed has to lose about 4 more points, or a shard's pooled share has to rise about 3 points.
Shard 3/8 passes today (both relays, 53 s).

The removal fix measures honest 6.1 % pooled (from 12.7 %). After it lands, the 22 % limit is about three
times the measurement, and nothing like the `healed` check of the seed list makes anyone lower it. Print
the pooled share in the nightly output, and fail when it is less than half the limit, so the budget
follows the code.

### S6. A reference to a document that is not in the repository

`groupPowers.fixture.ts:1` cites "inventory I7, powers P5 and P8/P9", which is internal hunt material.
Replace it with the SECURITY.md sentences it means.

### S7. No smoke test runs a member clock at all

Every run with a non-zero `ClockSkew` sits behind `SIM_CLOCKS`. The `Date.now` and `clock.now` overrides
(`groupSim.fixture.ts:247-252`) run in CI only with zero skew. Add one honest seed per variant, without
`extreme`, to the smoke tests, together with the B2 assertion.

## NOTE

### N1. The welcome-clears-renewals change in the oracle matches the code, and changed no outcome

`groupSimChecks.fixture.ts:158-160` clears the oracle's renewal times on every welcome the member took.
The code does the same. A welcome is taken only by a member with no group, with a group of one, or from
a rival, and a rival welcome calls `discard()` (`group.ts:161`), which clears `updates`
(`groupBase.ts:256`). The other two cases have no other member's renewal remembered. So the oracle became
as lenient as the code, not more lenient.

The case it now accepts: X's renewal taken in group A, a rival welcome into group B, and X's renewal in B
within half a rotation period. It is legal by the code, but not quite by the wording of SECURITY.md ("at
most twice per rotation period per member"). Its cost is bounded by the rival back-off: at most one extra
renewal per rival welcome, one per minute doubling. Consider "per member of my group" in SECURITY.md. There
is no mutation test for the `[4] second renewal` oracle, so this check is not protected by a mutant.

### N2. The nonce test: the arithmetic is right, one comment is wrong, and the frame rates are literals

The numbers:
- 340 frames per second per member × 100 members × 3210 s gives 1.0914e8 frames, or 2^26.70 (3210 s is
  30 min + 2 min jitter + 20 min absence + 3 × 30 s);
- that is a margin of 39.4 to the 2^32 invocation limit that NIST SP 800-38D (section 8.3) sets for
  random IVs under one key;
- the margin of 16 is gone at 245 members.

The comment at `groupNonce.test.ts:35` ("a chance of a repeated nonce near 2^-47") is wrong: n²/2^97 is
2^-43.6.

The timing comes from `bounds.ts`. Lengthening the key life beyond about 3947 s (for example `ROTATE_MS`
above about 42 minutes) fails the `< 2^27` assertion long before the margin is gone. The frame rates are
literals, though, and their rationale does not match the code:
- the camera is assumed to send 60 fps on one stream, but it sends up to 3 layers at 30/30/15 fps
  (`talk/lib/cameraLayers.ts`, `MAX_SCREEN_LAYERS` = 3, camera capped at 30 fps): 75 in all;
- the screen is assumed to send 3 × 60 fps, but its lower layers are capped at 30 (`LOWER_LAYER_FPS`):
  60 + 30 + 30 = 120.

The total, 295 frames per second, stays under the 340 assumed, so the bound holds. Raising the layer count
or the camera rate would not fail the test, though. Derive the rate from those constants.

One point in favour of the bound: every honest member's leaf renewal also advances the epoch. The key life
is therefore bounded even when the committer never rotates.

### N3. The pinned constants

`groupConstants.test.ts` pins the values stated in SECURITY.md as literals and covers the back-off
doubling and its reset. The test does what it says.

### N4. Smaller points

- The skew applies to `Date.now` only, not to `new Date()`.
- Wall clocks do not drift.
- `drift` grows from `SIM_START` rather than from the member's join. That is harmless.

## Verified as claimed

- **No production change.** Every changed file is a `.test.ts` or `.fixture.ts` under `web/src/lib/media`.
  The clock seam (`clock.now`, `web/src/lib/media/clock.ts`; `web/src/test-setup.ts:13`) is on main
  already. No production file was touched to allow the per-member clocks.
- **Existing seeds are unchanged.** The probe ran 140 rows on main and on the branch: seeds 1..50 plus
  every budget seed, 66 honest and 74 hostile, so all 17 honest and all 26 hostile budget seeds are
  included. The rows are identical in member count, event count, agreement time and violation text (key
  fingerprints masked). The failing set is exactly the budget on both trees.
- **The mutation tests still kill their mutants.** `groupSim.mutation.test.ts` passes on the branch.
- **Smoke cost.** `vitest run src/lib/media`, 3 workers: main 29.6 s, branch 32.1 s (+2.5 s, 8 more
  test files, 40 more passing tests). The slowest new file is `groupPowers.test.ts` at 5 s.
- **Cheap claims checked.**
  - Explorer, deep default (2 members, depth 1, 40 points): 1448 runs, 0 failures, 40 s.
  - Crash points, shard 0/24 of 2 members (all boundaries): 774 runs, 0 failures, 8 s. See S2 for what
    those runs are.
  - Server powers, `replay-commit`, shard 0/8 of seeds 1..200, both relays: no seed outside the budget
    fails, 39 s.
  - Nightly sweep shard 3/8 with the new availability budget: passes.
- **Repo gates.** ESLint on all touched files is clean: max-lines (800 for tests and fixtures), 80 lines
  per function, cognitive complexity 15. `scripts/check-english-only.mjs` passes. vue-tsc for tests fails
  (B1). The largest file is `groupSim.fixture.ts` at 657 lines.

---

# Round 2: branch msch128/hunt-lane2-b at 80a176920 (rebased on main f58bc3fc6, 3.0.9)

Reviewed 2026-11-12 by the same reviewer. Read-only, in a fresh checkout with `npm ci`, nice -n 10,
at most 3 vitest workers. As the coordinator asked, the exact contents of the lists were not judged; they
are being measured again on the merged tree with the clock fix. What was judged: the mechanisms.

## Verdict: OK AFTER FIXES

One blocker: half of the new cycle job fails every night by construction. Everything from round 1 is
fixed. The mutation experiments below show the self-checks catch a perturbation that is skipped
completely, but not every half-broken one.

## Round 1 items

| item | status |
|---|---|
| B1 vue-tsc | fixed: `vue-tsc -p tsconfig.tests.json` and `-p tsconfig.app.json` clean, ESLint clean on all touched files, English check passes, actionlint clean |
| B2 lies and steps never ran | fixed: both run, and a sweep fails when they do not (M1, M3 below) |
| B3 probe with a local path | fixed (file deleted), but a new probe appeared, see R2-S1 |
| S1 deep modes unrun / cannot fail | fixed: sim-sweep.yml has jobs for powers (4), clocks (4), cycle (2), explore (4), crash (8); every deep mode now asserts |
| S2 crash after the step | fixed: the crash happens at the boundary (verified by trace, below); the self-check is weak (R2-S3) |
| S3 recurrence unused | fixed: `Recurrence` is used in groupCycle.sweep.test.ts; but see R2-B1 |
| S4 explorer smoke | fixed: all 24 scripts, and it asserts that drops and overtakes happened |
| S5 availability | fixed: first-keys latency per seed (`joinMs`); per-shard table with a two-sided window of 2 points, so it must be lowered when the protocol gets better |
| S6 private reference | fixed |
| S7 no smoke with a skewed clock | fixed: groupClocks.test.ts runs offset, drift, steps, all on honest seeds |

No production file changed: the diff against f58bc3fc6 holds only `.test.ts`/`.fixture.ts` files under
web/src/lib/media and .github/workflows/sim-sweep.yml.

## BLOCKER

### R2-B1. The cycle job's shard 0 of 2 runs no seed and fails every night

Without `SIM_CYCLE_SEEDS`, the default list in groupCycle.sweep.test.ts is the failure budget, which is
empty on 3.0.9, plus `8 * i + 3` for i = 0..49. All of those seeds are odd. Shard 0/2 keeps
`seed % 2 === 0`, so it gets none. The guard `expect(ran).toBeGreaterThan(0)` then fails: measured red,
"runs: expected 0 to be greater than 0", both relays. Shard 1/2 runs all 50.

The guard working is good; the fix is to shard by position in the list, not by seed value, or to pick
seeds that cover both shards.

## SHOULD-FIX

### R2-S1. A new debugging probe: groupClocks.probe.test.ts

It writes a trace to `process.env.OUT ?? '/tmp/probe.txt'` when `PROBE` is set. It is the same kind of
file as round 1's B3 (no personal path this time). Delete it. Also squash the commit titled "wip of the
corrected harness" before merging.

### R2-S2. The self-checks catch a perturbation that is skipped entirely, not one that is noted but has no effect

Mutations, one at a time, each against the smoke test and a small deep run:

| mutation | smoke | deep |
|---|---|---|
| M1 `lie` returns at once | fails | fails ("runs in which the power happened: 0") |
| M2 `lie` writes its trace line but never delivers the roster | passes | passes |
| M3 `stepWall` returns at once | fails | fails ("runs with a wall clock step: 0") |
| M4 `stepWall` writes its trace line but does not move the clock | passes | fails, but only because a seed outside the list happened to fail |
| M5 replay picks no handshake | fails (deliveries > 50) | passes: the deep check matches `replay \d+ old handshakes`, so "replay 0 old handshakes" counts |
| M6 crash only after the step (the round 1 behaviour) | passes | not run; the same check |
| M7 crash never happens | fails ("the crash at 01#1 did not happen") | |

Fixes:
- match `replay [1-9]\d* old` in groupPowers.sweep.test.ts;
- count the deliveries of a lying roster (in `deliverRoster`), not the `lie to` note;
- count steps by their effect (`skew.wallStep !== 0` at the end of the run) or note the step after it was
  applied.

### R2-S3. The crash is real now, but `distinct > 10` cannot tell it from the old behaviour

Measured on script 0 of 2 members with the crash at the boundary:
- m1 at boundary 1 dies before it founds (no "keys 0" before the crash);
- m1 at boundary 60 dies before it adopts the commit that adds m2, where before it did.

With the old behaviour put back (M6), the same 600 runs (150 boundaries per user, both `seen` flags) give
14 distinct traces, exactly as many as the corrected code, so the smoke assertion `distinct > 10` passes
both. Replace it with a direct check: a crash at m1's first boundary leaves no "m1#1 keys" line before the
`crash` line.

The hunter's argument about the count is right, as measured: boundaries inside one protocol step give the
same run from outside. 600 runs give 14 distinct traces, so a distinct count "close to the number of
boundaries" cannot be expected. The count measures interrupted steps.

### R2-S4. The deep crash mode depends on what ran before it in the same process

`SIM_CRASH=1 SIM_SHARD=0/24 ... -t members` (the deep test alone) fails every time with "the crash at 01#155
did not happen", and the same happens for scripts 1 and 13. The nightly invocation runs the smoke test
first in the same file and passes (shard 0/8: 130 s, green). A crash probe that starts with a crash run in
a fresh module hangs (timeout), while the same probe after one uncrashed run works.

The cause, as I read it: one-time module-level initialisation (ts-mls and WebCrypto calls behind a shared
cache) is counted as boundaries in a cold base run but does not recur. A FOREVER promise at such a point
can also poison a cache that all simulated tabs share (in reality each tab is its own process).

Fix: warm up with one uncrashed run before counting, and never crash inside a shared cache fill, or skip
ordinals beyond the last boundary that recurs.

## NOTE

- **Shrink-only.**
  - `KNOWN_POWERS` and `KNOWN_CLOCKS` have a `healed` check: a listed seed that passes turns the sweep red.
  - `shardLost` is two-sided.
  - The per-seed limits (`seed`, `knownSeed`, `joinMs`) and `CHURN_LIMIT` / `CHURN_SEEN` can only be kept
    from growing by review, as with the seed list before.
- **pacer.step.test.ts** pins the defect twice, as `it.fails` and as a positive `toBeLessThan(10)`. Both
  turn red when the pacer fix lands, as intended. One would do.
- **Sensitive to the H-L2-04 and H-L2-05 fixes.** All runs are deterministic, so any change of protocol
  behaviour moves them:
  - the `shardLost` table: both relays, two-sided, so even an improvement turns it red;
  - `KNOWN_CLOCKS`: the steps, steps-back and all lists (the pacer fix should heal them), and
    extreme/hostile;
  - `KNOWN_POWERS` liar/hostile;
  - `joinMs`;
  - `STEP_SEEDS` in groupClocks.test.ts;
  - `CHURN_SEEN` (measured on the old budget seeds);
  - the cycle job's "must see churn" check stays unexamined while the failure budget is empty.
- **Cost.** Nightly job time, measured locally per shard: powers 600 s, clocks 347 s, explore 294 s, crash
  130 s, sweep 69 s. With npm ci per job this is in line with the stated 155 job minutes. Every timeout
  (45 min) leaves room.
- **Smoke suite.** `vitest run src/lib/media` (3 workers): 46 files passed, 441 tests, 54 s under a load of
  about 16 from other agents. The new files cost at most 6 s each when run alone.

## Deep shards run (3.0.9 + this branch)

| shard | result |
|---|---|
| sweep 3/8 with the availability table | green |
| powers 0/4 (all 8 powers, both relays, seeds 1..200) | green, 600 s |
| clocks 0/4 (8 variants, both relays, seeds 1..120) | green, 347 s |
| cycle 0/2 | red, no seed (R2-B1) |
| explore 0/4 (2 members, depth 2, 30 points) | green, 294 s |
| crash 0/8 (2 members, every boundary, nightly order) | green, 130 s |

## Per property: can the stated depth be reported as examined?

- Server powers, replay of each kind: yes, at 200 seeds x 2 relays. The deep self-check is weak, but the
  smoke test proves the replay happens (R2-S2).
- Server powers, roster lies: yes for "a lie is told", not for "a lie is delivered": not shown, by M2.
  Report it as examined once R2-S2 is fixed.
- Clocks, offset and drift: yes.
- Clocks, wall steps: yes, the steps happen (M3); that they move the clock is not checked (M4).
- Small rooms, explorer (2 members, depth 2, 30 points): yes.
- Crash points (2 members, every boundary): yes in the nightly order, as interrupted steps (R2-S3, R2-S4).
- Availability and first-keys latency: yes.
- Recurrence: not examined. Shard 0 runs nothing (R2-B1), and the churn check cannot fire on 3.0.9.
- Nonce bound and constant pins: unchanged since round 1, examined.

---

# Round 3: the harness commits on msch128/fix-h-l2-05-l9-02 at 61e2e2017

Reviewed 2026-11-12. Scope: the harness commits 5db07970d, d5b03bff6, ea0abd8d6, 4f0ffa5f0, 37da88d10,
1983d6561, 18934e683 and 61e2e2017. The protocol fixes on the same branch (H-L2-04, H-L2-05) are reviewed
elsewhere. Read-only, in a fresh checkout, nice -n 10, load below 7 throughout.

## Verdict: OK TO MERGE

All round 2 items are answered. Two smaller points are left; neither blocks the merge (R3-S1, R3-S2).

## Gates

- vue-tsc is clean for tsconfig.tests.json and tsconfig.app.json.
- ESLint is clean on every touched .ts file; the English check and actionlint pass.
- `vitest run src/lib/media` with 3 workers: 48 files, 452 tests passed, 43 s.
- Intermediate commits: nothing depends on them in a squash merge. Only a non-squash merge or a bisect
  would hit the two commits that do not type-check on their own.

## Round 2 items

- **R2-B1 (cycle shards): fixed.** The cycle, clock and power sweeps now shard by position in the list.
  Cycle shard 0/2 passes in 22 s and shard 1/2 in 26 s. Both now run seeds, because the guard `ran > 0`
  passes in each.
- **R2-S1 (probe file): fixed.** groupClocks.probe.test.ts is deleted.
- **R2-S2 (self-checks): fixed, with one gap.** Mutations, each against the smoke test and a small deep
  run (seeds 1..8). Without a mutation, all deep controls are green.

| mutation | smoke | deep |
|---|---|---|
| M1 lie no-op | red | red |
| M2 lie noted, roster not delivered | red | red |
| M2b lie set in the simulator, the call never told | red (the invariants fire) | red |
| M3 step no-op | red | red |
| M4 step noted, clock not moved | red | red |
| M5 replay picks nothing | red | red |
| M8 replay noted, never handed to the member | **green** | **green** |
| M6 crash after the step | red (cutOff at the first boundary) | red |
| M7 crash never | red | red |
| M9 no warm-up | | red at full depth ("crash at 01#155 did not happen"); green with SIM_CRASH_MAX=40 |

- **R2-S3 (crash check): fixed.** `cutOff` catches the round 1 behaviour, in smoke and in deep.
- **R2-S4 (crash order): fixed.** The warm-up works: without it, shard 0/24 fails again at full depth.

## Pooled availability (new job)

- **End to end.** I ran all 8 sweep shards with SIM_POOL_OUT and then groupAvailability.pool.test.ts on
  the 16 files. Result: green. Pooled honest 6.101 % (budget 6.10 %), hostile 12.825 % (budget 12.82 %).
  With one file removed, the pool test fails: "hostile: the shards present are [0,1,2,4,5,6,7]".
- **Order of writes.** The sums are written before the shard's own assertions, and the upload step has
  `if: always()` and `if-no-files-found: error`. So a failing shard still delivers its sums, and a
  missing shard fails the pool job.
- **The window.**
  - The measured per-seed spread is about 9.4 points. That gives about 1.33 points of standard error per
    shard of 50 seeds and about 0.47 points pooled over 400.
  - A window of ±1 point is therefore about 2 standard deviations of the reshuffle a timing change causes.
  - The runs are deterministic, so unchanged code never flaps.
  - A real regression has to move the pool by more than 1 point (honest 6.1 to 7.1 %, a sixth more).
  - That is reasonable, and lowering after an improvement is enforced.

## Oracle allowance for the receiver's clock rate (ea0abd8d6)

`ownClock` divides a limit by (1 + max(0, drift)). It allows exactly what the code's own rule permits
(groupVet.ts prematureRemoval, on the receiver's monotonic clock). The extra window is the drift fraction
of the limit: 0.5 % in the drift plan, 3 % in extreme. A real early removal larger than that is still
caught.

Repeated with the author's mutation, the removal limit cut by 10 % in groupVet.ts, on seeds 1..40 and
both relays:

| clock plan | runs failing [4] "took the removal of mN, who is in the room and was heard ~26 s ago" |
|---|---|
| drift | 2 of 80 |
| none | 1 of 80 |

So the allowance hides nothing measurable. Note how low the oracle's sensitivity to a 10 % early removal
is (1 to 3 % of runs). It is enough over the nightly's 800 sweep runs and 240 drift runs. The SECURITY.md
sentence of 4f0ffa5f0 (19.5 minutes, by the receiver's own clock) matches groupVet.ts.

## SHOULD-FIX (not blocking)

### R3-S1. `REPLAY_DELIVERED` counts the note, not the delivery

The note is written just before `to.call.receive` (groupSim.fixture.ts:556). Mutation M8 dropped the
receive and kept the note; it passed smoke and deep. Note after the receive, or count a member's reaction,
for example `deliver`/`keys` lines after the replay window. The smoke test already requires more than 50
replay lines, so only this half-broken case gets through.

### R3-S2. Re-running one sweep shard will probably fail its upload

`actions/upload-artifact` v7 refuses an artifact name that already exists in the run unless
`overwrite: true` is set. A "re-run failed jobs" of one shard most likely hits `pool-N` from the first
attempt. This was not tried on GitHub. The content is deterministic, so `overwrite: true` is safe; add it.

## Lists sensitive to the merged tree

As stated by the coordinator, the author re-measures them on the merged tree. These move with any change
of the group's timing:
- the per-shard table (now only a warning);
- the pooled budget (hard, ±1 point);
- `KNOWN_CLOCKS` and `KNOWN_POWERS`;
- `joinMs`;
- `CHURN_SEEN`.

## Per property: can the stated depth be reported as examined?

| property | examined? |
|---|---|
| server powers, replay of each kind (200 seeds x 2 relays) | yes. The perturbation is shown to happen, but the delivery itself is not yet proved (R3-S1); the smoke test's 50 deliveries cover it |
| server powers, roster lies (200 x 2) | yes: a lying roster the member holds is counted (M1, M2, M2b red) |
| clocks, offset and drift (120 x 2) | yes |
| clocks, wall steps forward and back (120 x 2) | yes: the moved clock is read back (M3, M4 red) |
| small rooms, explorer (2 members, depth 2, 30 points) | yes |
| crash points (2 members, every boundary, as interrupted steps) | yes (M6, M7, M9 red) |
| availability: pooled per relay, per seed, first-keys latency, epoch life | yes, end to end (8 shards pooled, missing shard detected) |
| recurrence: exact cycle and coarse churn limit on 50 seeds x 2 | yes for the checks that run. The "must see churn" self-check is not examined, as the header says (the failure budget is empty) |
| removal and committer oracles under drift | yes: a 10 % early removal is still caught |
| nonce bound and constant pins | yes, unchanged |
