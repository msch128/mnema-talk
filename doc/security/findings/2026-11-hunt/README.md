# Open findings of the 2026-11 defect hunt

A finding file is the record a hunter wrote for one reproduced defect: the
claim, the property it breaks (with the document or code that states it), a
repro command, the smallest case found, who can reach it, the effect, what was
not shown and a direction for the fix. The hunt meant every finding to go to
two agents that had not written it (one to reproduce it from a clean
checkout, one to try to defeat it), which could change its severity. Of the
files here, only H-L8-01 had both; the "Checked by" column says what is true
of each.

The findings here were open when they were published, and the open ones are
those [SECURITY.md](../../../../SECURITY.md#known-open-defects) lists under
"Known open defects". A fixed finding is described by the review of its fix,
in [doc/security/reviews](../../reviews/README.md); when a finding here is
fixed, its SECURITY.md entry goes with the fix, and its file stays (marked
fixed in its Outcome line) where a review links it.

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

| File | Severity | Subject | Checked by |
|---|---|---|---|
| [h-l2-06](h-l2-06.md) | S4 | two groups of a Talk with the same members and the same epoch number cannot see each other | found and reproduced in the simulator by the reviewer of the H-L2-05 and H-L9-02 fix; no separate reproducer or refuter |
| [h-l10-06](h-l10-06.md) | S4, fixed | an account deletion aborted as a deadlock by the member's own write from another device; fixed: the erasure transaction is retried up to three times when the database aborts it as a deadlock; after that the request answers 503 "the server is busy" and the member stays signed in (fix review [h-l10-06](../../reviews/2026-11-hunt/h-l10-06.md)) | found and reproduced through the real routes by the integration review of the collection branch; no separate refuter |
| [h-l8-01](h-l8-01.md) | N (claimed S4) | a flood of fresh keys evicts a login limiter entry that has counted failures | reproduced by one agent from a clean checkout; a second agent tried to defeat it and lowered it to N |
| [h-l8-05](h-l8-05.md) | S4 | SFU memory kept per member who joined and left, for the life of a viewer's connection | one agent that had not written it tried to defeat it, measured it with a heap profile and narrowed it; no separate reproducer |
| [h-l7-10](h-l7-10.md) | N (claimed S4) | an edit to empty text accepted after the last attachment went while it waited; no route reaches it | rated N by the coordinator; one agent (the reviewer of the H-L7-09 fix) confirmed N; no separate reproducer |
