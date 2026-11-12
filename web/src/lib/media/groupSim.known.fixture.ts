// Every seed among 1..400 that ends in a room still regrouping 240 s after the
// relay stopped interfering, per relay: the failure budget of the nightly
// sweep (groupSim.sweep.test.ts). The lists may only shrink: a seed that
// heals moves to groupSim.test.ts and leaves these lists in the same change.
// (v3.0.7 had 26 hostile and 17 honest; see groupSim.settle.fixture.ts. The
// removal fix of groupEntry.ts healed all of them but exposed hostile seed
// 109, which was passing by luck of timing and settles after 415 s.)
export const KNOWN_HOSTILE: number[] = [109]
export const KNOWN_HONEST: number[] = []
