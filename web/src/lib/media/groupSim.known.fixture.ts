// Every seed among 1..400 that ends in a room still regrouping 240 s after the
// relay stopped interfering, per relay: the failure budget of the nightly
// sweep (groupSim.sweep.test.ts). The lists may only shrink: a seed that
// heals moves to groupSim.test.ts and leaves these lists in the same change.
//
// v3.0.7 had 26 hostile and 17 honest seeds (groupSim.settle.fixture.ts). The
// committer's wait for newcomers (groupEntry.ts) healed all but hostile seed
// 109, which had passed before: it ended in two groups, one of nine members
// and one of two, that merged slowly, because the rival-welcome allowance (one
// welcome per sender per minute, doubling to ten) was used up by earlier
// merges; it settled after 415 s. It was not the waiting rule: the wait only
// moved the schedule into a state in which the allowance was spent. With the
// wait the length of the patience of the committer skip it passes, and the
// lists are empty. A seed that fails again goes back here, with its reason.
export const KNOWN_HOSTILE: number[] = []
export const KNOWN_HONEST: number[] = []
