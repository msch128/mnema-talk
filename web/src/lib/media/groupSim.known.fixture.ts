// Every seed among 1..400 that ends in a room still regrouping 240 s after the
// relay stopped interfering, per relay: the failure budget of the nightly
// sweep (groupSim.sweep.test.ts). The lists may only shrink. A seed that
// heals moves to groupSim.test.ts and leaves these lists in the same change.
export const KNOWN_HOSTILE = [30, 43, 59, 73, 89, 97, 104, 107, 133, 148, 150, 194, 199, 206, 238, 244, 292, 294, 300, 301, 310, 323, 332, 362, 378, 398]
export const KNOWN_HONEST = [30, 64, 109, 116, 119, 125, 148, 162, 167, 206, 246, 254, 258, 276, 303, 344, 377]
