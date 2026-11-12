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

Not kept (lost with a temporary directory before they were copied): the
review of #768 (permission checks refuse without a service), the first two
rounds for #771 and the second round for #779.
