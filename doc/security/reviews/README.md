# Independent review reports

Reports of the independent, read-only reviews that a change under the paths in
`.github/CODEOWNERS` gets before it is merged (rule 9 in `AGENTS.md`). A
reviewer gets the code and the task, not the author's conclusion, changes
nothing, and writes down what it checked, what it ran and what it did not
check. The reports are kept as written; a finding that was fixed later stays
in the report.

The reviewers so far were agents, not people. A report here is no external
audit.

## 2026-11: hardening run on 3.0.0 (releases 3.0.1 to 3.0.7)

| Report | Change | Subject |
|---|---|---|
| [771-third](2026-11/771-third.md) | #771, merged with #780 | metadata removal for JPEG, PNG and WebP, third round |
| [775](2026-11/775.md) | #775, merged with #780 | repair of the media group protocol |
| [777-keys](2026-11/777-keys.md) | #777 | key rotation (`JWT_SECRET_PREVIOUS`, `DATA_KEY`, `rotate-keys`) |
| [777-ws](2026-11/777-ws.md) | #777 | panic isolation in the hub and the SFU |
| [779](2026-11/779.md) | #779 | group merge, first round (rejected) |
| [779c](2026-11/779c.md) | #779 | group merge, third round |
| [779d](2026-11/779d.md) | #779 | group merge, fourth round |
| [779-final](2026-11/779-final.md) | #779 | the three lines that changed after the fourth round, reviewed after the merge |
| [780](2026-11/780.md) | #780 | the merge train as a whole |
| [780-events](2026-11/780-events.md) | #780 | typed events |
| [780-outbox](2026-11/780-outbox.md) | #780 | webhook queue written in the transaction, health under a storage outage |
| [780-tt](2026-11/780-tt.md) | #780 | Trusted Types and the CI changes |

## 2026-11: follow-up to the hardening run (3.0.8)

| Report | Change | Subject |
|---|---|---|
| [789](2026-11/789.md) | #789 | timer panics, fail-closed permission enforcement, live storage health; two rounds: the should-fix findings S1 to S4 of the first were fixed in the same pull request and checked in the second |
| [790-sweep](2026-11/790-sweep.md) | the nightly simulator sweep | the settle table of the 43 failing seeds, the sweep of seeds 1..400 with a failure budget, a test for the key package renewal rule; its two should-fix findings were fixed in the same pull request |
| [798](2026-11/798.md) | #798 | the data export after a desktop download: one more hour after the first complete download, the length sent, a single download claim that holds under parallel requests; two rounds: the should-fix findings of the first were fixed in the same pull request and checked in the second |
| [bot-platform](2026-11/bot-platform.md) | the bot platform | manifest install, message components and modals, interactions, both SDKs and the release packaging; seven rounds, every finding fixed in the same branch and checked by reverting its fix |
| [bot-platform-web](2026-11/bot-platform-web.md) | the bot platform | the web UI: the add-bot flow, components, selects, forms and their states, at six viewports in two browsers; two rounds |
| [webhook-call-chat](2026-11/webhook-call-chat.md) | #803 | an outgoing webhook of a conversation's call Talk heard its call chat (releases up to 3.0.13; fixed in 3.1.0): the routes, the queue insert and the delivery each refuse a private channel |
| [816](2026-11/816.md) | #816 | the flaky ws, sfu and macOS stage tests fixed at their cause: each stated cause checked against the code, two reproduced on main, and each test shown to fail again when the product behaviour it guards is broken |

## 2026-11: defect hunt on 3.0.8

Each finding of the hunt went to two agents that had not written it (one
reproduced it from a clean checkout, one tried to defeat it), and each fix to
a reviewer that had written neither. These are the fix reviews; several took
two or three rounds, and a round that ended in "do not merge" is part of the
report.

| Report | Fix for |
|---|---|
| [h-l1-01-04](2026-11-hunt/h-l1-01-04.md) | four gates and tests that did not check what their stated reason said (complexity gate, a false skip, the compatibility baseline, the release checks) |
| [h-l4-01](2026-11-hunt/h-l4-01.md) | leaving a group conversation while seated in its Talk |
| [h-l3-s1-01](2026-11-hunt/h-l3-s1-01.md) | edits and poll votes after a block in a conversation of two |
| [h-l3-12](2026-11-hunt/h-l3-12.md) | the chat of the conversation's Talk after a block |
| [h-l3-01-04](2026-11-hunt/h-l3-01-04.md) | link rules: the server judges every host the client could open |
| [h-l3-05-06](2026-11-hunt/h-l3-05-06.md) | a muted channel and resolved mentions; snippets and pushes hide what the renderer hides |
| [h-l3-09](2026-11-hunt/h-l3-09.md) | wiki titles compared by one Unicode-aware key |
| [h-l3-10](2026-11-hunt/h-l3-10.md) | a blocked member's words folded on every surface |
| [removal-fix](2026-11-hunt/removal-fix.md) | the removal of silent members in the media group protocol (the committer waits for newcomers; a renewal that won a conflict counts toward the limit), three rounds, with a design note: a mitigation, not a cure |
| [h-l3-07-h-l2-01-h-l3-11](2026-11-hunt/h-l3-07-h-l2-01-h-l3-11.md) | document sentences that differed from the code; the composer's message text |

One change in this set was made by the coordinator and reviewed by nobody
else: after "Show", focus goes to the message's row in the place where it was
pressed (`web/src/shared/lib/revealFocus.ts`).

## 2026-11: defect hunt, second part (lanes 5 to 11, on 3.0.9)

The same procedure. Several findings of this part came from the fix reviews
themselves, and a review that ended in "merge after fixes" stays in the report
with its later rounds appended.

| Report | Fix for |
|---|---|
| [h-l8-08](2026-11-hunt/h-l8-08.md) | the SFU vets a client's answer against its own offer (media sections, one stream per section) before the connection reads it; two rounds |
| [h-l8-07](2026-11-hunt/h-l8-07.md) | caps on RTCP streams nothing reads on a media connection; four rounds |
| [h-l8-07-test-change](2026-11-hunt/h-l8-07-test-change.md) | a test of the stream caps that the coordinator corrected just before the merge (a rare stray buffered stream), reviewed after the release: right as it is |
| [h-l8-09](2026-11-hunt/h-l8-09.md) | memory kept per subscription switch: the connection is renewed at a sender cap, a resume budget per publisher and source; three rounds |
| [h-l8-03](2026-11-hunt/h-l8-03.md), [h-l8-04-06](2026-11-hunt/h-l8-04-06.md) | the bandwidth estimator forgets a stopped sender; a hub map entry left at zero; a counter for dropped presence audit entries |
| [h-l8-02-h-c-01-03](2026-11-hunt/h-l8-02-h-c-01-03.md) | the TURN relay's allocation budget per member; mention kinds next to `@all` (the field `direct_mentions`); an event comment's delete dialog; two rounds |
| [h-l9-01-03](2026-11-hunt/h-l9-01-03.md) | the SFU's stored times on a steady clock; sequence numbers and picture ids across a layer switch; two rounds |
| [h-l2-04](2026-11-hunt/h-l2-04.md) | the media group protocol: an add the member never saw relayed; two rounds |
| [h-l2-05-h-l9-02](2026-11-hunt/h-l2-05-h-l9-02.md) | the handshake pacer and the back-offs on the monotonic clock; with a measured judgement of the availability under a hostile relay and the trace of five simulator seeds (one new finding, not fixed: an equal-epoch fork) |
| [lane2-harness](2026-11-hunt/lane2-harness.md) | the protocol sweeps themselves (replays, roster lies, clocks, crash points): three rounds; the first found that two perturbations never ran |
| [h-l5-02-03-05](2026-11-hunt/h-l5-02-03-05.md), [h-l5-01-04](2026-11-hunt/h-l5-01-04.md) | the web client's lists after a reconnect and a delete; the follow before the first reply; unread counts corrected after a delete or edit; read-state answers ordered against live events (migration 0104) |
| [h-l6-01](2026-11-hunt/h-l6-01.md), [h-l6-01-round2-h-l6-03](2026-11-hunt/h-l6-01-round2-h-l6-03.md) | audit entries of a private forum and of a removal from a private Talk reach only auditors who can see them |
| [h-l6-02](2026-11-hunt/h-l6-02.md) | a hidden message, category or channel answers like a missing one; three rounds |
| [h-l6-04](2026-11-hunt/h-l6-04.md), [h-l6-05-06-07](2026-11-hunt/h-l6-05-06-07.md) | a conversation's call Talk: membership decides, and it is no community channel anywhere (events, the admin channel routes, the media list, reports, auto-delete) |
| [h-l7-01-03](2026-11-hunt/h-l7-01-03.md), [h-l7-06-07](2026-11-hunt/h-l7-06-07.md), [h-l7-09](2026-11-hunt/h-l7-09.md) | limits and rules under concurrent requests: reset links, the first Owner, role templates, the group conversation cap, the Owner's reply and a pinned reply against deletes and the auto-delete sweep |
| [h-l7-04-05](2026-11-hunt/h-l7-04-05.md), [h-l7-08](2026-11-hunt/h-l7-08.md) | audit entries inside the change's transaction; answers read inside it; no background job after the shutdown drain began |
| [h-l10-01-04](2026-11-hunt/h-l10-01-04.md), [h-l10-05](2026-11-hunt/h-l10-05.md) | account erasure and export: what was left out; nothing new may name an erased account (migration 0103), with a dump and restore comparison |
| [h-l11-01-03](2026-11-hunt/h-l11-01-03.md) | the security floor of the desktop bundle: the trailer is read the same way by the pull request check and the release; Go reads signed documents as the desktop clients do; three rounds |
| [h-l10-06](2026-11-hunt/h-l10-06.md) | an account deletion aborted by the database as a deadlock with the member's own write is retried (found by the integration review, fixed after 3.0.10) |
| [follow-ups](2026-11-hunt/follow-ups.md) | event comments and event texts go through automod; reactions compared in one emoji form |
| [release-gate](2026-11-hunt/release-gate.md) | a red, stale or missing night of the deep sweeps blocks the release, with one deliberate override; more paths in CODEOWNERS; the known open defects named in SECURITY.md; two rounds |
| [bundle-integration](2026-11-hunt/bundle-integration.md) | the collection branch as a whole: every merge with a hand resolution, the coordinator's own commits, interactions between fixes |

Not kept from the hardening run (lost with a temporary directory before they were copied): the
review of #768 (permission checks refuse without a service), the first two
rounds for #771 and the second round for #779.
