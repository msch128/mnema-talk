# Review: msch128/audit-followup (base origin/main e6c9d9394)

I am an independent, read-only reviewer: I wrote none of these commits. Nothing in the repository was changed and nothing was pushed. I worked in a temporary worktree at the branch tip cdee1edac, removed afterwards. All GitHub API calls were reads (`gh api`).

| Commit | Verdict |
|---|---|
| 9e7407cf8 ci: the nightly gate in release.yml | **MERGE AFTER FIXES**: G-1, G-2, G-3 |
| bb12e2c42 chore: CODEOWNERS paths | **MERGE AFTER FIXES**: C-1 |
| cdee1edac docs: "Known open defects" and the finding files | **MERGE AFTER FIXES**: D-1, D-2 |

None of the three has a BLOCKER. The gate fails closed in every case I tried. Nothing I found lets a red night ship unnoticed, except by an act that needs write access: deleting the red run (G-2), or editing the gate's own script, which no code owner guards (C-1).

---

## 9e7407cf8: the nightly gate

### (1) Bypass without the override

**Triggers** in release.yml: `workflow_run` (CI `completed` on `main`) and `workflow_dispatch` (inputs `mirror_version` and `nightly_override`). Nothing else.

**Jobs:**
- `release-please` is the only job that can create a release or tag. Its `if` (release.yml:76-80) requires either a green CI push run, or a dispatch on `refs/heads/main` with a non-empty override.
- `image`, `web-bundle` and `release-notes` need `release_created == 'true'` from that job.
- `dockerhub-mirror` only copies an existing, cosign-verified GHCR image.

**Order inside `release-please`:**
1. The tip check.
2. The sparse checkout of the tested commit.
3. **"Check the nightly sweeps"**.
4. The first release-please step, which creates the release when a release PR was already merged.
5. The merge of a new release PR.
6. The publish step.

Steps 3 and 4 share one condition (`steps.tip.outputs.current == 'true'`), so release-please never runs without the gate before it.

**Paths I checked:**
- **The release-please PR merged by hand:** the push to main runs CI, which triggers release.yml. The gate runs before the release-please step that would see the merged PR and tag it. A red night leaves the merged PR pending, and the first green run releases it. Not a bypass.
- **A re-run of an old Release run:** it re-runs the whole job. The tip check passes only if main has not moved. The gate then reads the API live, at the current time. Not a bypass.
- **workflow_run chains:** only CI triggers release.yml, and the job also requires `workflow_run.event == 'push'`. A fork pull request from a branch named `main` stays excluded, as before.
- **A dispatch from another branch:** refused by `github.ref == 'refs/heads/main'`. As before this change, a branch with a modified release.yml can run its own copy. That copy would sign as `release.yml@refs/heads/<branch>`, which `internal/imageverify` refuses. This is not new.

**Ways past the gate that need write access** (G-2):
- `gh run delete` of the red scheduled run. The previous green run then decides for up to 48 hours. The API list no longer contains the red run, and the gate cannot tell it is missing.
- Re-running a failed scheduled run until it is green. The gate's own error message suggests this "when it failed for a reason outside the code". On a sweep with a new seed every night (store-trace, plain-sweep) that re-runs the same seed, so it only helps against true flakes. It is still a way that no record shows.
- Editing `scripts/nightly-gate.py` itself. No code owner guards it (C-1).
- Growing a known-failure list: `web/src/stores/storeTrace.known.ts`, or `canaryKnownLeaks` in `internal/server`. These "grow only with a finding" by convention, and neither path has a code owner.

doc/development.md says "There is no other way past the gate". That sentence needs these named as forbidden.

### (2) Blocking forever or by accident; API failures

- **An API failure blocks.** Every `gh api` call runs under `set -euo pipefail`. A 4xx or 5xx exits non-zero, the step fails with gh's own message (for example "HTTP 502"), and nothing is released. If an answer is unreadable or missing a key, the script exits 2 with "cannot read the answers for X". It never passes.
- **A renamed or deleted workflow file** gives a 404, which blocks.
- **Disabled workflows** block: `disabled_manually` and `disabled_inactivity`.
- **Pagination:** the query asks for the 20 newest scheduled runs on main, newest first. Only the newest completed run matters, so pagination cannot hide one.
- **The 48-hour window against 02:43 to 03:47 UTC nights:**
  - A release at any hour with every night run: the last completed run is 0 to 25 hours old, which passes.
  - A run in progress is ignored, so a release at 03:00 during the night's run is judged by yesterday's run, which passes.
  - **One skipped night plus a release during the next night's run blocks**, because the last completed run is then just over 48 hours old. Probe: last green 2026-11-12T02:43, release 2026-11-14T03:00, the run in progress gives rc 1. This is brief and fails safe, and the message names it.
  - Two missed nights block until a night completes. That is intended.
- **First night:** sim-sweep.yml was registered at 2026-11-12T02:43Z, and visibility-sweep.yml and store-trace.yml at 19:44Z (API, read only). None has a scheduled run yet. Their first nights are 2026-11-13 at 02:43, 03:41 and 03:47 UTC. If sim-sweep has not completed a scheduled run by 2026-11-14T02:43Z, it blocks.
  - **At the first red night**, every release stops until a fix has landed and the next scheduled run of that sweep is green, which means up to a day after the fix. The alternative is the override. Manual runs of the sweep do not count. This is by design and documented. It also means the fix of a red night is itself held until the next night.
- **Forks:** schedules do not run in forks, and in a fork that enables Actions the gate blocks after 48 hours. That is harmless.
- **Messages:** a run 48 h 1 min old is reported as "48 h old (limit 48 h)", because the hours are rounded. This is cosmetic.

### (3) The override

- **Who can dispatch:** anyone with write access to the repository. Today that is one collaborator, the owner, as admin (`gh api …/collaborators`). No workflow in the repository has `actions: write`, and the default workflow permissions are `read`, so no `GITHUB_TOKEN` of another workflow can start a dispatch. **Local agents use the owner's gh credentials, so they can dispatch, and GITHUB_ACTOR then names the owner.** "Agents never use the override" (AGENTS.md rule 9) is a rule of conduct; nothing technical enforces it. That is fine, but it should be stated plainly. An environment with required reviewers would not help, because there is a single account.
- **Signing identity is unchanged** (read from the code; not run). `internal/imageverify/policy.go:44` pins `^https://github\.com/msch128/mnema-talk/\.github/workflows/release\.yml@refs/heads/main$` with the GitHub OIDC issuer, and the provenance source is the repository. A dispatch on main gives a certificate SAN `release.yml@refs/heads/main`, the same as the `workflow_run` trigger. The policy checks no trigger or event extension. The mirror's `cosign verify` uses the same prefix.
- **Only tested commits are released:** a dispatch takes main's tip, requires a successful CI `push` run on main for exactly that SHA (release.yml:109-117), then mirrors every required check (`release-checks.py`) as before.
- **The reason is recorded, but not durably** (G-3). The reason is whitespace-normalised, so a newline cannot start a workflow command. It is printed as a `::warning::` annotation and in the job summary, together with GITHUB_ACTOR. Log and summary retention is 90 days (repository setting, read via the API). After that, the record that development.md calls "the run in the Actions history is the record" is gone.
  - A reason of only spaces passes the job `if` but is no override: the gate blocks when red, and releases normally when green.
- **mirror_version is now optional:**
  - A backfill dispatch (mirror_version only) works as before. The release-please job is skipped, the mirror runs, and `VERSION = inputs.mirror_version`.
  - Both inputs together fail in the gate step. The mirror is skipped because `nightly_override != ''`.
  - A dispatch with neither input runs nothing and ends green. That is harmless, but silent (NOTE).
  - After an override release, the mirror runs with `github.event_name == 'workflow_dispatch'`. So it takes the backfill branches: the digest is resolved from the GHCR tag instead of the image job's output, and "newest" is decided with `gh release view`. The result is the same image (cosign-verified, the tag just pushed), but it would be cleaner to branch on `release_created` (NOTE).

### (4) The script, with my own JSON and the real API

All probes ran in throwaway directories:

| Input | Exit |
|---|---|
| all green | 0 |
| empty run lists, workflows older than 48 h | 1 |
| empty run lists, young workflows | 0 |
| completed run with `conclusion: null` | 1 |
| completed run without a `conclusion` key | 1 |
| newer red `workflow_dispatch` run over an older green scheduled run | 0 (manual runs do not count) |
| newer red scheduled run on another branch | 0 (only main counts) |
| two workflows red (failure and timed_out) | 1, both named |
| workflow answer file missing | 2 |
| runs answer file missing | 2 |
| runs answer is an API error object | 2 |
| runs answer is an empty file | 2 |
| workflow answer is an API error object | 1 ("is None") |
| in progress only, young workflow | 0 |
| in progress newest, older completed red | 1 |
| queued newest, older green 24 h | 0 |
| green 47 h 59 min old | 0 |
| green 48 h 01 min old | 1 |
| conclusion `skipped` | 1 |
| conclusion `neutral` | 1 |
| rerun that is green, created 3 days ago | 1 (created_at decides) |
| state `disabled_inactivity` | 1 |
| run created in the future | 0 |
| a workflow named twice | 0 |
| override with a red night | 0 |
| override of only spaces with a red night | 1 |
| override with `\|` and a newline | 0, flattened to one line |
| an unparseable `--now` | traceback, exit 1 |

**Real API, read only, at 2026-11-12T23:26Z:** all three workflows are active with 0 scheduled runs. The script gives "no completed scheduled run yet; it was added 21 h / 4 h / 4 h ago" and **passes** (rc 0). With `--now 2026-11-14T03:00Z` on the same answers, sim-sweep blocks, as described in (2).

**Tests:** `scripts/tests/test_nightly_gate.py` has 12 tests, all OK. Four mutants (age check off, success check off, active check off, every run counted as completed) each fail 1 to 8 tests.

### (5) Which scheduled workflows belong in the gate (G-1)

| Workflow | Schedule | Kind | Belongs in the gate |
|---|---|---|---|
| sim-sweep.yml | daily 02:43 | deep protocol, trust chain, race and SFU sweeps | yes (in) |
| visibility-sweep.yml | daily 03:47 | permission model and canary world | yes (in) |
| store-trace.yml | daily 03:41 | store traces | yes (in) |
| **plain-sweep.yml** | daily 03:17 | 200,000 messages against the plain forms of pushes and snippets, no known-failure list, a privacy property | **yes: missing** |
| release-check.yml | daily 07:23 | checks what the *last* release published | no: a red run means the release pipeline drifted, and blocking the next release would block its repair |
| image-scan.yml | weekly | new vulnerabilities in the image already shipped | no: blocking would block the fix |
| codeql.yml | weekly | CodeQL also runs on every pull request (required) | no |
| trust-expiry.yml | weekly | an operational expiry alert | no |

plain-sweep.yml is a deep test of exactly the kind the gate is for, and it was registered before visibility-sweep and store-trace. Add it to the list in release.yml:153 and in the docs. Also add a test that reads `.github/workflows/*.yml` and requires every workflow with a daily schedule to be either in the gate list or on an explicit exclusion list with a reason. Then a new nightly cannot be forgotten.

### (6) Linters, tests, permissions

- `actionlint` on release.yml and on the whole repository: exit 0.
- `shellcheck -s bash` on the two new or changed run blocks (extracted): exit 0.
- `make test-scripts`: exit 0, all 11 suites OK, including test_nightly_gate.py.
- Permissions: the job adds `actions: read` only, which listing runs and workflows needs. The workflow-level default stays `{}`.
- The checkout of the tested commit has `persist-credentials: false` and a sparse checkout of the two scripts. It now also runs when there is no release PR; that is harmless.
- `OVERRIDE` and `MIRROR` reach the step only through env, never interpolated into `run:`.

### (7) Docs

doc/development.md "Releases" and AGENTS.md rule 9 match the code, with three exceptions:
- "There is no other way past the gate": see G-2.
- "the run in the Actions history is the record": true for 90 days (G-3).
- "(or re-run the blocked Release run once a night is green)": this works only while main's tip is the commit that run tested. Otherwise the tip check skips it and the next push releases. Add "if main has not moved" (NOTE).

The release.yml header comment is accurate.

### Findings

- **G-1 SHOULD-FIX:** `plain-sweep.yml` is not in the gate (release.yml:153, development.md, AGENTS.md rule 9). Add it, plus a guard test over the workflow schedules (see (5)).
- **G-2 SHOULD-FIX:** deleting the red scheduled run (`gh run delete`) or re-running it until green passes the gate, and nothing shows it. Minimum: name both, and editing the gate script or a known-failure list, as forbidden in AGENTS.md rule 9 and development.md, instead of "there is no other way". Better: also detect a deleted run, for example by fetching the workflow's runs of every event and refusing a gap in `run_number` above the last completed scheduled run.
- **G-3 SHOULD-FIX:** the override reason and actor live only in a run summary and annotation, which are kept 90 days. Write them into a durable place: the release notes of that release (the `release-notes` job has the release), or a commit status or a comment on the release PR. Then correct the development.md sentence.
- **NOTE:**
  - A dispatch with neither input ends green and does nothing.
  - The mirror uses the backfill branches after an override release.
  - A "48 h old (limit 48 h)" message for just over 48 h.
  - One skipped night blocks a release made while the next night's run is still running.
  - Agents can technically use the override with the owner's token.

---

## bb12e2c42: CODEOWNERS

- **The comment is true.** main's ruleset has `require_code_owner_review: false` (read via the API), so "This file has no effect by itself … once main's ruleset has 'Require review from Code Owners' turned on" holds. The new sentence about the hunt's heaviest defects matches the added paths: the SFU; automod and the client's markdown links; the audit log; mdplain; messages, dm and poll write gates. `internal/perm/` is already listed.
- **The added paths against the fix lists in doc/security/reviews/README.md and the S1 to S3 findings:**
  - Correctly added: `internal/sfu/` (H-L8-07/08/09, H-L9), `internal/automod/` + `web/src/shared/lib/markdown/` (H-L3-01..04), `internal/audit/` (H-L6-01/03), `internal/mdplain/` (H-L3-05/06), `internal/chat/messages/` (H-L3-12, H-L3-S1-01, H-L5-05), `internal/chat/dm/` (H-L4-01, H-L7-06), `internal/poll/` (H-L3-S1-01).
  - S3 or worse fixes outside any owned path:
    - `internal/chat/talks/` (H-L3-12, H-L6-04: who may see a conversation's Talk).
    - `internal/chat/channels/` (H-L6-02: a hidden channel answers like a missing one; six of that review's file references).
    - `internal/ws/voice_kick.go` (the fail-closed permission enforcement of 3.0.8, named by the H-L4-01 S1 review). Only `perms.go`, `event_scope.go` and `mediaframe/` of `internal/ws` are owned.
    - `internal/turnrelay/` (H-L8-02, the relay allocation budget; "the media server" in the comment suggests it belongs with the SFU).
  - `internal/chat/readstate` (H-L5-01) is a correctness fix, not a security boundary, so I do not recommend it.
- **C-1 SHOULD-FIX: the release and update trust path is not owned beyond `.github/workflows/`.** These files decide what release.yml does but sit outside that directory:
  - `scripts/nightly-gate.py` (the new gate: changing it is the bypass G-2 forbids)
  - `scripts/release-checks.py` (which required checks are mirrored onto the release commit)
  - `scripts/security-floor.sh` (the bundle floor)
  - `internal/webbundle/` (desktop bundle signing and verification; "release and update verification" in the comment covers imageverify and updaterd but not this)
  - `desktop-trust/`, `web/desktop-compat.json`, `web/desktop-signing.pub`
  - `.github/CODEOWNERS` itself. Without it, removing an entry needs no review.

  Add these. Adding talks, channels, `ws/voice_kick.go` and turnrelay is a recommendation, not a condition.

---

## cdee1edac: "Known open defects" and the finding files

**Links:** I checked all 45 relative links in SECURITY.md, the five finding files, the findings README, the two edited review files and development.md with a script. Every target exists, and the anchors `#known-open-defects` and `#security-model-summary` resolve. `make docs-check` exit 0, English check exit 0.

**Nothing private:** no local absolute paths, no hostnames, no personal data. The only paths are generic `/tmp/o5.jsonl` outputs in a repro command. Branch names and commit ids are the hunt's, as the README says. Every test the entries cite exists: `internal/httpx/limiter_eviction_steering_test.go`, `internal/sfu/cycle_leak_test.go`, and `internal/server/subquery_lock_wait_integration_test.go:184` (`TestAnEditThatWaitedForTheRemovalOfTheLastAttachmentIsAcceptedEmpty`). "Since 3.0.10" is right: v3.0.10 is tagged at 9d139b1fc, and version.txt is 3.0.10.

**Each sentence against its finding and the code:**
- **H-L2-06:** the cause, the reach and "5 to 8 at its compressed timings" match the finding (304 s and 486 s). **"The fix needs a change of the wire format" is too strong.** The finding names a second direction without a wire change ("compare by leaf instead of id when deciding whether a welcome is a rival one"). The same claim is in the file's Outcome line. Write "the proposed fix changes the wire format (a protocol decision)" (D-2).
- **H-L10-06:** matches. The deletion is aborted, the request answers 500, nothing is half deleted, and a second attempt works.
- **H-L8-01** (D-1):
  - **"A locked entry is never evicted" is false.** `evictSampled` (internal/httpx/bounded.go:31-46) always deletes one entry of a sample of 32. `makeRoomLocked` (loginfailure.go:184-200) only *prefers* unlocked ones, so a sample of 32 locked entries evicts the stalest locked one. The finding's own wording is narrower: a locked victim survived the test's flood at the full cap.
  - **"gives fewer guesses than waiting out the lock" drops the finding's condition.** The finding says this holds on a 4-core host, with break-even at about 40 cores. Say "on a host with fewer than about 40 cores", or name the measurement.
  - "Each evicted count costs … tens of thousands of failed sign-ins" (67,600), "sign-ins answer 503" and "no fix planned" match.
- **H-L8-05:** matches. 3 to 4 KB is Pion's share, the memory is released when the viewer leaves, and it is bounded by the renewal at the sender cap since 3.0.10 (SECURITY.md "Media connection limits": 32,768 senders, about 8 MB).
- **The H-L8-07 residual:** matches the "Media connection limits" text ("a report that arrives in the moment a sender is made or stopped can leave that one stream with its capped buffer, which the buffered limit counts").
- **H-L7-10:** matches the finding and the outcome (N, latent, a test pins it).
- **The intro "None lets anyone read or change what they may not":** holds for the six entries. Nothing in H-L8-01 lets a guesser bypass the password check; it only weakens one throttle.
- **The findings README (NOTE):**
  - "Each finding then went to two agents" is not true of all five. H-L2-06 says "reproducer and refuter: open", H-L8-01 and H-L8-05 say "pending phase 2", and H-L10-06 was found in a review. Write "was meant to go to" or name the exceptions.
  - The table's "N (claimed S4)" for H-L7-10 matches its Outcome line.
- **D-1 SHOULD-FIX:** correct the two H-L8-01 sentences in SECURITY.md.
- **D-2 SHOULD-FIX (wording):** H-L2-06's "the fix needs a change of the wire format", in SECURITY.md and in the Outcome line.

## What I ran

- `gh api`, read only:
  - the workflows and their scheduled runs (sim-sweep, visibility-sweep, store-trace, plain-sweep)
  - the collaborators
  - main's branch rules
  - the Actions retention setting and the default workflow permissions
- The gate on those answers. My 28 JSON probes. Four mutants against the unit tests.
- `actionlint` (release.yml and all workflows). `shellcheck -s bash` on the extracted run blocks.
- `make test-scripts`, `make docs-check`, `node scripts/check-english-only.mjs`, and a link and anchor check of the changed docs.

## Not checked

- A real Release run: none was triggered, and no dispatch or override was used.
- The keyless certificate of a dispatch-triggered run. I read the policy; no image was signed.
- Whether job summaries expire exactly with the logs. I assumed the 90-day log retention applies to them.
- The H-L2-06 simulator repro, the H-L10-06 deadlock and the H-L8-05 measurements were not re-run. I checked the sentences against the finding files and the code.

---

## Round 2: 579ce4efd, 9320f69a8, 747e06f0a, 681977910 (on cdee1edac)

| Commit or commits | Verdict |
|---|---|
| ci (9e7407cf8 + 579ce4efd): the gate | **MERGE** |
| chore (bb12e2c42 + 9320f69a8): CODEOWNERS | **MERGE** |
| docs (cdee1edac + 747e06f0a + 681977910) | **MERGE AFTER FIXES**: R2-D1 (a condition on how it lands) and R2-D2 (one word) |

### The gate (G-1, G-2, G-3)

- **G-1 fixed.**
  - `GATED` in scripts/nightly-gate.py now holds sim-sweep, visibility-sweep, store-trace and plain-sweep.
  - `NOT_GATED` holds release-check, image-scan, codeql and trust-expiry, each with a reason.
  - release.yml loops over `$(python3 scripts/nightly-gate.py --list)`. If `--list` fails, the loop is empty and the gate is still called with no workflow arguments, so it judges `GATED`, finds no answer files and exits 2. It fails closed.
  - I added a daily `zz-new-sweep.yml` (quoted cron) to the worktree and then a weekly one with single quotes. In both cases `test_every_scheduled_workflow_is_gated_or_excluded_with_a_reason` fails and names the file.
  - **NOTE:** a cron written unquoted (`- cron: 5 4 * * *`) or in flow style (`schedule: [{cron: "5 4 * * *"}]`) is valid YAML, but the regex at test_nightly_gate.py:204 does not see it. Both forms passed the test. Every workflow in the repository quotes its crons today. Matching any `cron:` key, or a `schedule:` key under `on:`, would close the gap.
- **G-2 fixed, with one documented limit.**
  - **Run numbers across all events, on the real API (read only):** run numbers are per workflow and count every event. I listed the newest 100 runs of each of these workflows and found no gap:

    | Workflow | Events in the newest 100 runs | Run numbers |
    |---|---|---|
    | ci.yml | 18 push, 82 pull_request | 1811 to 1910 |
    | codeql.yml | 18 push, 81 pull_request, 1 schedule | 1867 to 1966 |
    | release.yml | 96 workflow_run, 4 workflow_dispatch | 189 to 288 |

    So a manual run or a pull-request run between two nights does not look like a deletion. The four gated sweeps only trigger on `schedule` and `workflow_dispatch`.
  - **Probes** (with run numbers and attempts):

    | Case | Exit |
    |---|---|
    | a manual dispatch between nights | 0 |
    | a red dispatch run newest over a green night | 0 |
    | red night 9 deleted, dispatch 10 exists | 1, "run numbers 9 are missing … (a deleted run)" |
    | red night 9 deleted, next night in progress | 1 |
    | red night 9 deleted, nothing newer | 0 (the documented limit) |
    | a gap *below* the deciding run | 0 (correct: older history does not count) |
    | `run_number` missing | 2 |
    | `run_attempt` missing | 0, read as attempt 1 |

  - **More than 100 runs between two nights:** the deciding scheduled run falls off the single page and the gate blocks. It fails closed, but the message reads "has no completed scheduled run on main although it was added … ago" (NOTE: misleading, and rare).
  - **The documented limit:** a deleted red run that is the newest run shows only once a later run exists. That means up to one day. development.md and the script docstring say so, and the rule forbids the deletion. **NOTE:** a limit of about 26 hours instead of 48 would shrink that window to minutes, at the cost of blocking after one missed night. That is a trade-off, not a defect.
  - **Re-runs:**
    - The attempts endpoint on a real re-run, CI run 43293325602 (run_attempt 2, success): `/attempts/1` answers `conclusion: failure`. `/attempts/9` answers HTTP 404, gh exits non-zero, the step removes the file, and the gate blocks with "first attempt could not be read" (probe).
    - Probes: attempt 1 success passes; attempt 1 failure blocks; attempt 1 missing or a 404 object blocks; attempt 1 as garbage exits 2.
- **G-3 fixed.**
  - The gate writes `--note-file` only for a non-blank override. The job output `override_note` is set only when that file is non-empty. The new step "Record the nightly override" runs only when `override_note != ''`, inside `release-notes`, which needs `release_created == 'true'`. So it runs only for an override that cut a release.
  - The note goes through env (`NOTE`) and is never interpolated into the script.
  - **Injection probes** through `release_note()`:
    - CR, LF and U+2028 are folded into one line (Python `split()`), so neither `GITHUB_OUTPUT` nor the body gains a line.
    - `-->`, `<!-- nightly-override -->` and `<script>` lose their angle brackets, so they can neither close nor forge the marker.
    - `@someone` becomes `(at)someone`, and backticks are stripped.
    - `${{ secrets.X }}` stays text: it only ever reaches an env value.
    - The login is reduced to `[A-Za-z0-9-]` and put in code marks.
  - **NOTE:** markdown links and images in the reason still render in the release notes (`[click](https://…)`, `![t](https://…)`). The reason comes from someone with write access, so this is low risk. Putting the reason in a code span, which is safe now that backticks are stripped, would make it inert.
  - **NOTE:** dispatch inputs can be up to 65,535 characters. A very long reason added to the notes could exceed GitHub's limit on a release body, and then `gh release edit` fails. The release itself is already complete when this step runs: the tag, the GitHub Release, the image, the bundle, and the notes from the step before. `image` and `web-bundle` do not depend on `release-notes`. So the failure leaves only the override line missing, on a red job. Nothing is half published. Truncating the reason (to a few hundred characters) would avoid the red job.
  - The marker prevents doubling on a re-run of the job.
- **Docs** (development.md "Releases" and "Forbidden ways past the gate", AGENTS.md rule 9) match the code. That includes "seen once a later run of that workflow exists", "if `main` has not moved since", the four known-failure lists by name, and "Nothing technical stops them … it is a rule".

**Run, by exit code:**
- `actionlint` (all workflows): 0.
- `shellcheck -s bash` on the three extracted run blocks (tip check, gate, record step): 0.
- `make test-scripts`: 0. All 11 suites OK, including test_nightly_gate.py with 21 tests.
- I deleted the temporary test workflow before removing the worktree.

### CODEOWNERS (C-1)

All 12 new paths exist. Each directory pattern ends in `/` and is a directory, and each file pattern has no trailing slash and is a file. In CODEOWNERS a leading `/` anchors a pattern at the repository root, `/dir/` covers everything below it, and `/path/file` matches exactly that file. The new paths:
- `internal/chat/talks/`, `internal/chat/channels/`, `internal/ws/voice_kick.go`, `internal/turnrelay/`
- `internal/webbundle/`, `desktop-trust/`, `web/desktop-compat.json`, `web/desktop-signing.pub`
- `scripts/nightly-gate.py`, `scripts/release-checks.py`, `scripts/security-floor.sh`
- `.github/CODEOWNERS`

The rewritten comment names exactly these additions, and its "no effect by itself" part is still true (the ruleset has `require_code_owner_review: false`).

NOTE: `scripts/tests/test_nightly_gate.py` and the known-failure lists outside owned paths (`web/src/stores/storeTrace.known.ts`, `canaryKnownLeaks` in `internal/server`) stay unowned. The written rule covers the lists.

### Docs (D-1, D-2, the findings README)

- **D-1 fixed.** The new H-L8-01 sentences match the code and the reports:
  - "Eviction deletes the stalest entry of a random sample of 32 and prefers unlocked ones, so a locked entry goes only when all 32 sampled entries are locked" matches `evictSampled` (internal/httpx/bounded.go:31-46) and `makeRoomLocked` (loginfailure.go:184-200).
  - "in the hunt's flood at the full cap a locked entry survived" matches the finding.
  - "about 67,600 failed sign-ins", "10 to 15 times fewer guesses on a 4-core host" and "break-even at about 40 cores" match the phase 2 refutation (lines 45, 71 and 72).
- **D-2 fixed.** H-L2-06 now names both directions (the epoch tag in the heartbeat, a wire change; judging a rival welcome by the leaf, no wire change), in SECURITY.md and in the Outcome line.
- **The findings README "Checked by" column is true against the hunt's reports:**
  - H-L2-06: found by the H-L2-05/H-L9-02 fix reviewer; no repro or refute report.
  - H-L10-06: found by the integration review.
  - H-L8-01: both a repro and a refute report (repro-H-L8-01-02, refute-H-L8-01-02).
  - H-L8-05: refute report only (refute-H-L8-04-06, with heap profiles).
  - H-L7-10: rated N by the coordinator, and N confirmed by phase 2 in fixreview-H-L7-09.
  - The intro now says "meant every finding to go to two agents … only H-L8-01 had both", which is true.
- **R2-D1 SHOULD-FIX (a condition on how it lands): H-L10-06 is documented as fixed, but the fix is not on this branch.**
  - On this branch, SECURITY.md drops the H-L10-06 entry, and three files call it fixed and link `doc/security/reviews/2026-11-hunt/h-l10-06.md`: the finding file's Outcome line, the findings README table row, and bundle-integration.md. That report does not exist on this branch, or on `origin/msch128/fix-h-l10-06`.
  - The fix itself (`eraseAttempts` in internal/auth/account/account_delete.go, the 40P01 check in internal/db/errors.go) is only on `origin/msch128/fix-h-l10-06` (f34d6b5d7), which is not an ancestor of this branch.
  - My link check of the changed docs and all review files reports three dead links, all to that report. The other six "missing" hits are pre-existing markdown examples (`url`, `<url>`, `//host`) in h-l3 reports. `make docs-check` does not see them, because it skips doc/security subdirectories.
  - Land this docs set only together with or after fix-h-l10-06, with the fix review (the hunt's fixreview-H-L10-06.md) committed at `doc/security/reviews/2026-11-hunt/h-l10-06.md` and a row in doc/security/reviews/README.md. Merged alone, main would say "fixed" for a defect it still has, and carry three dead links.
- **R2-D2 SHOULD-FIX (one word): "retried up to three times" is wrong.** `eraseAttempts = 3` with `if attempt == eraseAttempts` returning `ErrServerBusy` means the erasure *runs* up to three times, which is two retries. Write "run up to three times" (or "retried up to twice"). The phrase appears in the README row, the finding file's Outcome line and bundle-integration.md.

Not checked: a real Release run or override dispatch (none triggered); a real deleted run (none exists in the repository's history, so the gap rule is checked against the API's numbering and by probes); GitHub's exact limit on a release body. The temporary worktree was removed. Nothing was changed in the repository or pushed.
