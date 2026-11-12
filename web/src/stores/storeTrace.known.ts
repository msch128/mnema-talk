// What the store trace (storeTrace.test.ts) finds broken today: "<plan>
// <state>" -> the finding (lane 5 of the 2026-11 defect hunt). A key runs, must
// break, and turns the test red on the day it no longer does: smoke checks the
// keys marked smoke (the checked-in traces show them); a deep run of at least
// 20 traces, unsharded, checks the keys marked nightly (each was seen in at
// least 7 of 30 traces; the others in 1 to 6, too rare to require every
// night). The list may shrink; it grows only with a finding.
//
// H-L5-05: the first reply under my message is decided before the follow is known.
type Known = { finding: string; smoke: boolean; nightly: boolean }

const both = (finding: string): Known => ({ finding, smoke: true, nightly: true })

export const KNOWN_BROKEN: Record<string, Known> = {
  'clean reply-follow': both('H-L5-05')
}
