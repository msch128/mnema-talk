# Review: branch msch128/followup-protocol (simulator sweep, settle table, renewal test)

Reviewed commit 5270382db on top of origin/main c7fafa242 (v3.0.7). Read-only review in a throwaway worktree, removed afterwards.

## Verdict: MERGE (after considering the SHOULD-FIX items, none blocks)

No production code changes, the new test bites, the budget logic is correct in both directions, sharding covers every seed exactly once, the settle figures reproduce, and the workflow follows the repo's rules. The one real weakness is that a malformed SIM_SHARD makes the sweep pass vacuously (SHOULD-FIX 1).

## BLOCKER

None.

## SHOULD-FIX

1. **A malformed shard setting passes vacuously.** `web/src/lib/media/groupSim.sweep.test.ts:19-20`: `SIM_SHARD=x` gives `index`/`count` of NaN, `mine()` is false for every seed, no seed runs, and both tests pass in about 0 s (reproduced: `SIM_SWEEP=1 SIM_SHARD=x` prints 2 passed). The same holds for `count=0` or `index >= count`. The workflow sets the value from a literal matrix, so today it cannot happen, but a typo in a later edit would turn the sweep green and silent. Fix: throw if `!Number.isInteger(index) || !Number.isInteger(count) || count < 1 || index < 0 || index >= count`, and optionally assert that at least one seed ran.
2. **A failing seed is reported without its trace.** The sweep only lists seed numbers. `report(result)` (used by the known tests) is not attached, so the nightly alert needs a manual rerun per seed. Cheap fix: put `report(result)` of the first unexpected failure into the assertion message. Also, an exception thrown by `runSeed` aborts the whole relay loop of that shard and hides the other seeds; a try/catch that records the seed as failing would keep the result complete.

## NOTE

1. **Who notices a failure.** A red scheduled run sends GitHub's default failure e-mail to the person who last edited the workflow or its cron line (and shows in the Actions tab). No issue or other alert is created. This is the same convention as `trust-expiry.yml` ("a failed scheduled run is the alert"), so it is consistent, but it depends on that person's notification settings. Eight shards fail independently, so one regression can send up to eight mails.
2. **No `concurrency` group and no fork guard** in `.github/workflows/sim-sweep.yml`. A manual dispatch overlapping the nightly run is harmless (read-only, no shared state). Other scheduled workflows have no fork guard either.
3. **Timeout is generous.** `timeout-minutes: 90` per shard; a shard is 50 seeds per relay, roughly a minute of CPU locally, so 15 to 20 minutes would still leave room and fail a hang sooner.
4. **Order of the two assertions.** Per relay, a "not in the budget" failure hides the "passes now" list in the same run (the first expect throws). Both are reported once the first is fixed; acceptable.
5. **`SIM_SETTLE_MS` other than 3600000 compares against the 3600 s table.** With a different limit the table test fails for seeds whose result changes (e.g. 7200000 still passes for honest 258 because it stays "never"). The header comment says it repeats the measurement, so this is intended; just do not use it as a regression check.
6. **The settle table is documentation, not a gate.** Its only always-on check is that the seeds equal the budget lists, which is what it should be. The 240 s tally in `groupSim.settle.fixture.ts` ("within 240 s: 0") is right (smallest value is 241).

## What was checked and found correct

- **No behaviour change.** `git diff origin/main...origin/msch128/followup-protocol --name-only` filtered for non-test files: only `.github/workflows/sim-sweep.yml`, `AGENTS.md` (one line in the tree listing) and the two `*.fixture.ts` files. Nothing under `group.ts`, `groupMerge.ts`, `groupSync.ts`.
- **Moved lists equal the old ones.** `KNOWN_HOSTILE` (26 seeds) and `KNOWN_HONEST` (17 seeds) in `groupSim.known.fixture.ts` are identical, in order, to `HOSTILE`/`HONEST` in `groupSim.known.test.ts` on main; the old file now imports them. The settle test asserts the table lists exactly these seeds, in order, per relay.
- **New unit test** (`groupMerge.test.ts` "never gives a new lowest id a package that an earlier lowest id has already seen"): passes on the branch (22/22). With the line in `groupMerge.ts` reverted to `const offered = last?.own === this.own` it fails alone (21 pass, 1 fail, "expected package not to deeply equal the first"). Reverted afterwards.
- **Budget logic, both directions**, using one-seed shards (`SIM_SHARD=30/400`, `1/400`): as committed, 2 pass. Seed 30 removed from the hostile list: fails with "hostile relay: seeds that fail and are not in the budget: [30]". Seed 1 (passing) added to the honest list: fails with "honest relay: seeds in the budget that pass now; move them to groupSim.test.ts: [1]". Fixture restored afterwards.
- **Sharding.** `seed % count === index` over 1..400 with 8 shards covers all 400 seeds exactly once (checked by script); the budget check uses the same predicate, so each budget seed is judged in exactly one shard. Both relays run in every shard.
- **Vacuity, except SHOULD-FIX 1.** Without `SIM_SWEEP` the sweep suite is skipped (1 file skipped). The workflow sets `SIM_SWEEP: "1"` and `SIM_SHARD: <i>/8` at step level, the vitest path matches the file, there is no `it.skip`. Vitest's `include` is `src/**/*.test.{js,ts}`, so the file is collected by the PR pipeline too but skipped.
- **PR pipeline cost.** The always-on part is the one settle-table test (a list comparison) plus one skipped suite; the `describe.skip` block in the known test is unchanged. Negligible.
- **Settle table spot checks** (SIM_SETTLE=1, filtered; the test asserts equality with the table): honest 64 (241 s, fast) passes in 3 s; honest 119 (3062 s, slow) passes in 23 s; honest 258 ("never") passes in 8 s; hostile 107 ("never") passes in 43 s. In addition honest 258 given 7200 s of virtual time still does not settle. The tallies in the fixture header (0 / 16 (6+10) / 26 (10+16) / 35 (15+20) / never 8) match a recount of the 43 rows, and the eight "never" seeds listed are exactly the nulls.
- **Workflow.** Actions are pinned to the same full SHAs as `ci.yml` (checkout v7.0.1, setup-node v7.1.0), node 26 as in CI; `permissions: contents: read`; `persist-credentials: false`; no secrets; no untrusted input (the only interpolation is the numeric matrix value, in a step name and an env value, not in the script); cron `43 2 * * *` is valid; `fail-fast: false` so all shards report; `npm ci` in `web/`.
- **Repo gates** (in the worktree): ESLint on the six touched TS files clean; `vue-tsc --noEmit -p tsconfig.tests.json` clean; `node scripts/check-agents-paths.mjs` ok (149 paths); `node scripts/check-english-only.mjs` ok; `web/scripts/lint.ts` 0 errors (23 existing warnings, none in touched files); no non-ASCII characters in the touched files; all touched files are far below 800 lines (largest `groupMerge.test.ts` 533) and the new functions are short.

## What I ran

`git fetch`; worktree of the branch with `npm ci` in `web/`; vitest with at most 2 workers under `nice -n 10`: `groupMerge.test.ts` (normal and with the rule reverted), sweep and settle test files with and without `SIM_SWEEP`/`SIM_SETTLE`, one-seed shards for both budget directions, one malformed shard, five settle seeds. ESLint, vue-tsc (tests), agents-path check, english-only check.

## What I did not check

- The full 400-seed sweep on both relays (neither unsharded nor one of the eight shards), so I did not confirm that every one of the 43 budget seeds still fails and no other seed does; I only ran seeds 1 and 30 through the sweep path and trust the author's list, which equals the old list.
- The actual GitHub-hosted run (runtime on ubuntu-latest, e-mail behaviour, `npm ci` there), and the other 38 settle rows.
- The full `npm run check` / coverage gates and the Go side.
