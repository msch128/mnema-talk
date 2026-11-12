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

Not kept from the hardening run (lost with a temporary directory before they were copied): the
review of #768 (permission checks refuse without a service), the first two
rounds for #771 and the second round for #779.
