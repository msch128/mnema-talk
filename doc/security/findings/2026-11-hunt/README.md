# Open findings of the 2026-11 defect hunt

A finding file is the record a hunter wrote for one reproduced defect: the
claim, the property it breaks (with the document or code that states it), a
repro command, the smallest case found, who can reach it, the effect, what was
not shown and a direction for the fix. Each finding then went to two agents
that had not written it (one reproduced it from a clean checkout, one tried
to defeat it), which could change its severity.

Only the findings that are still open are here, the ones
[SECURITY.md](../../../../SECURITY.md#known-open-defects) lists under "Known
open defects". A fixed finding is described by the review of its fix instead,
in [doc/security/reviews](../../reviews/README.md). When a finding here is
fixed, its file and its SECURITY.md entry go with the fix.

The files are kept as the hunt wrote them, with an "Outcome" line added on
top. References to files outside the repository were rewritten to where the
thing now lives in the repository, or dropped. Commit ids and branch names
are the hunt's; line numbers are from the release the finding was made on.

Severity scale of the hunt:

- S1: confidentiality, integrity or authorization is broken for someone
  without special rights.
- S2: data loss or corruption, or loss of service without an attacker.
- S3: wrong behaviour a user or operator can see; a policy that can be
  bypassed by an ordinary member.
- S4: robustness; needs special rights or unusual conditions.
- N: no defect today, but one change away from one. Gets a guard test.

| File | Severity | Subject |
|---|---|---|
| [h-l2-06](h-l2-06.md) | S4 | two groups of a Talk with the same members and the same epoch number cannot see each other |
| [h-l10-06](h-l10-06.md) | S4 | an account deletion aborted as a deadlock by the member's own write from another device |
| [h-l8-01](h-l8-01.md) | N (claimed S4) | a flood of fresh keys evicts a login limiter entry that has counted failures |
| [h-l8-05](h-l8-05.md) | S4 | SFU memory kept per member who joined and left, for the life of a viewer's connection |
| [h-l7-10](h-l7-10.md) | N (claimed S4) | an edit to empty text accepted after the last attachment went while it waited; no route reaches it |
