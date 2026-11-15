# Review: the full clipping sweep in its own gated workflow (branch msch128/clip-sweep-workflow, head 7f852a20c)

Reviewer: an agent that did not write the change. Read-only; own detached worktree at 7f852a20c, now removed.
At review time the branch had not been pushed and had no pull request. I reviewed the local branch tip; a later push needs this report checked against it.
The branch sits one commit behind main. It merges cleanly with main and with PR #816 (`git merge-tree`).
Commits reviewed:
- d552d865b: a forum post of the combo's own
- 74386d01f: the sweep in its own workflow, nightly and gated
- d89bf59b9: a gate test for a young sweep that has only pull request runs
- 00804665d: the post title within 100 characters
- 7f852a20c: wait for the welcome dialog; reuse the group DM on a reseed

CODEOWNERS paths touched: `.github/workflows/`, `scripts/nightly-gate.py`, `AGENTS.md`.

## Verdict

**MERGE AFTER FIXES.**

What works:
- The move is correct. The new job is the old one byte for byte apart from `needs`/`if`, the triggers and the concurrency group.
- The smoke subset still runs in ci.yml.
- A red night blocks as it does for the other sweeps.
- actionlint is clean.
- The docs and AGENTS.md rule 9 list the same five gated workflows as `GATED`.

What needs fixing:
- clip-sweep is the first gated workflow that also runs on pull requests. The gate reads only the newest 100 runs of every event, so pull request runs can push the deciding night off that page and block a release for no reason (finding 1).
- The forum post scene trades one source of noise for another and drops the long word it used to audit (finding 2).

## 1. Does the gate let a release through before the first night?

`judge` (nightly-gate.py) passes a gated workflow with no completed scheduled run on main only while `now - workflow.created_at <= 48 h`. Pull request runs do not count as nights: `scheduled_main_runs` keeps only `event == 'schedule'` and `head_branch == 'main'`.

- `python3 -m unittest scripts/tests/test_nightly_gate.py`: 22 tests, OK. This includes the new `test_a_new_sweep_with_pull_request_runs_only_passes_while_young`. Young with only pull request runs (one red) gives pass with "has no completed scheduled run yet"; old with the same runs gives block.
- **The clock starts at `created_at` of the workflow record, not at the merge.** For a workflow that runs on `pull_request`, GitHub creates that record when the file first runs from the branch, so the PR's open time likely counts. I could not measure this: the branch is not pushed, and `GET /actions/workflows/clip-sweep.yml` answers 404 today.
- The first night is the first 04:11 UTC after the merge, and the run takes up to 40 min. So the gate passes until then only if that night finishes within 48 h of the first push.
  - Example: pushed at 20:00 and merged before 04:11 the night after next, it passes.
  - If the merge slips past that, every release is blocked from 48 h after the push until the first night is green, at most about a day.
  - This errs on the safe side, so it is not a defect. Recommendation: merge within about a day of the first push, and right after the merge read `gh api repos/{repo}/actions/workflows/clip-sweep.yml --jq .created_at` to know when the window ends.

## 2. Does a red night still block?

The new workflow goes through the same `judge` as the other four. I ran scratch scenarios through the test module's `gate()` helper. They are not committed.

| clip-sweep.yml runs | Gate |
|---|---|
| red night, newer green pull request run | block: "the last scheduled run concluded failure" |
| red night, newer green manual run on main | block (workflow_dispatch is no night) |
| green night, newer red or cancelled pull request runs | pass |
| green night re-run (attempt 2), first attempt failed | block |
| green night followed by 100 pull request runs | **block**: "has no completed scheduled run on main although it was added 321 h ago" (finding 1) |

The gap check (`missing_numbers`) counts the run numbers of every event. Pull request runs cancelled by the new `cancel-in-progress` still carry their numbers, so they cause no false "deleted run".

## 3. ci.yml still runs the smoke subset

- The `clip-sweep` job is removed from ci.yml in full. actionlint shows no dangling `needs`.
- The e2e jobs still run `clipping.spec.ts`:
  - `e2e-chromium` runs every chromium spec (`--project=chromium ... --shard=i/4`) without `CLIP_SWEEP`, so the spec takes `smokeMatrix()` and only the `smoke` scenes (clipping.spec.ts:158, :169).
  - `ci-macos-calls.yml:56-58` still runs `clipping.spec.ts` for its Talk scenes.
- The old job was not a required check, and neither is the new one. The ruleset and `release-checks.py` need nothing changed.
- Narrower trigger: the PR-time full sweep used to run on every code change (`changes.code`). It now runs only for `web/**`, `e2e/**` and the workflow file. A server-only change that alters what the UI shows is caught by the next night, not on its PR. This is acceptable and matches the commit's intent.

## 4. Gates run

- `actionlint` 1.7.12 on the worktree: exit 0, no findings.
- `scripts/tests/test_nightly_gate.py`: 22 OK. This includes the check that every scheduled workflow is in `GATED` or `NOT_GATED`.
- e2e `tsc --noEmit`: pass.
- ESLint (`web/eslint.config.ts`) on `e2e`: pass.

## 5. Docs and AGENTS.md rule 9

The gated list reads `sim-sweep.yml`, `visibility-sweep.yml`, `store-trace.yml`, `plain-sweep.yml`, `clip-sweep.yml` in all of these, and they agree:
- AGENTS.md rule 9
- doc/development.md (Releases)
- the release.yml header comment
- `GATED`

- The AGENTS.md tree entry and the clip-sweep.yml header comment describe the same triggers as the `on:` block. clipping.spec.ts:22-24 was updated too.
- No doc still calls the sweep a ci.yml job (I searched for clip-sweep, clipping sweep and CLIP_SWEEP outside doc/security/reviews).
- Rule 9's "a new scheduled workflow goes into GATED or into NOT_GATED" is followed.

## Findings

### Finding 1 (MEDIUM, false release block; fix before merge) — .github/workflows/release.yml:166 with scripts/nightly-gate.py (`judge`, `missing_numbers`)

- **The problem.** release.yml fetches `actions/workflows/$wf/runs?per_page=100`, which is one page of runs of all events and branches. Until now every gated sweep ran only on schedule and dispatch, so the newest 100 runs always held the last night. clip-sweep.yml also runs on every pull request push that touches `web/**` or `e2e/**`. Once 100 such runs come after the last night, the gate sees no scheduled run at all and blocks with "no completed scheduled run on main" (table above, last row).
- **How likely.** ci.yml had 36 pull request runs in the last 24 h and 55 in the last 48 h, over all paths. A busy day of web work comes close to the limit.
- **Outsiders can trigger it.** On this public repository, fork pull requests touching `web/**` also create runs, including ones that wait for approval. So someone outside can push the night off the page. The gate fails closed, so this cannot let a release through. It can only stop releases, and rule 9 leaves no way round that other than the owner's override.
- **Fix.** Fetch every run the decision can depend on. The deciding night must be at most 48 h old to pass, and the gap check only needs runs newer than it, so all runs created in the last 49 h are enough:
  `gh api --paginate --slurp "repos/$GITHUB_REPOSITORY/actions/workflows/$wf/runs?per_page=100&created=>=$(date -u -d '-49 hours' +%Y-%m-%dT%H:%M:%SZ)" | jq '{workflow_runs: map(.workflow_runs[])}' > "$work/$wf.runs.json"`
  - Any equivalent that writes one `{workflow_runs: [...]}` file works.
  - With nothing in the window, a stale or missing night still blocks as before.
  - The cheaper alternative is to drop the `pull_request` trigger and keep nightly plus manual runs. That loses the PR-time full sweep.
- **Test.** Add a gate case that has the deciding night beyond position 100, or keep my scratch case as the regression.
- Changing release.yml's gate step is a gate edit, so it needs this kind of review. It does not let anything through that was blocked before.

### Finding 2 (LOW, test determinism and coverage) — e2e/tests/clipping/scenes.ts:256-268

- **More posts in the list.** Every combo now creates a post in the shared forum (`POST /api/channels/{forum}/posts`). The world is seeded once per server and removed only in `afterAll` (clipping.spec.ts:149-150, :161). So within a worker the forum fills up with "Post of <combo>..." posts from the combos before. The `forum` list scene (scenes.ts:255, a smoke scene) then audits a list whose content depends on which combos the shard ran first. That is the same kind of order dependence the commit set out to remove.
- **Lost long word.** The new post's body is `Body. <suffix>`. The seeded posts carry `LONG_WORD` (seed.ts:188), so the post view no longer audits an unbreakable word in a post body.
- **Fix.** Seed.ts already creates three posts. Put their ids into `World`, and open the first one by address (`/c/{forum}/t/{id}`), as the scene now does. Opening by address alone removes the click-during-redraw race, with no per-combo post and with the long word kept.

### Note 3 — e2e/tests/clipping.spec.ts:120

The welcome flow change is right: `Locator.isVisible` ignores its timeout and answers at once, so on main the scene could report "did not open" for a dialog that was still on its way. `waitFor({ state: 'visible', timeout })` waits for it.

### Note 4 — e2e/tests/clipping/seed.ts:198-203, :264

Reusing the run's group DM on a reseed matches the server's limit (`MaxNewDMsPerDay = 10`, internal/chat/dm/dm.go:34), and removeWorld hides it again (seed.ts:288). The un-hide is done as the admin, the same user who hid it.

Order matters on a reseed: `runMembers` is read before seeding, and the group id is written back only after `seedElsewhere` succeeds. If seeding fails part-way after the group was created (first seed), the id is lost, and the next attempt makes a new group. Bounded by the daily limit of ten. Acceptable.

### Note 5 — branch state

The branch is one commit behind main. #816 also edits `clipping.spec.ts` (it drops the `trackPeerConnections` import and call). The two touch different hunks and merge without conflict in either order. The branch adds no new use of `trackPeerConnections`, so it needs no follow-up after #816.

## Not checked

- No CI run exists for this branch, because it is not pushed. I did not run the clipping spec locally, since it joins Talks and call specs run in CI only. Finding 2 and notes 3 and 4 come from reading the code.
- The `created_at` behaviour for a workflow first seen on a pull request (section 1) could not be measured before a push.
- I did not check what a scheduled run costs in runner minutes.

## Outcome

Fix finding 1 (or drop the `pull_request` trigger) and finding 2, push, open the PR, and let its own clip-sweep run go green. Merge within about a day of the first push so the young-workflow window covers the first night. A re-review is needed only for the release.yml change of finding 1.

## After the review (added by the author)

- Finding 1: release.yml now pages through every run of the last 49 hours (`created=>=`, `--paginate --slurp`) instead of one page of 100. Gate tests: a night behind 150 pull request runs still decides (green passes, red blocks), and a guard test checks that release.yml pages with a window longer than 48 hours. Filtering for scheduled runs only was not used: the check for deleted runs needs every run number after the night, and pull request runs share that numbering.
- Finding 2: the world seeds one forum post with an unbreakable title, and the scene opens it by its address in every combo; no post per combo.
- Changes after the reviewed head that this report does not cover: the group conversation is posted to only when it is new (a member may send at most two unanswered messages), in this pull request; a Talk join waits for the realtime socket, because a join sent before the socket was up was dropped and the call never connected (its own pull request, #819, with its own review); a forum post title of one long word wraps inside its card, found by the sweep's new seeded post (its own pull request, #821). This branch is stacked on #819 and #821 and is rebased on main once they are merged.
