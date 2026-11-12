// How long the seeds in groupSim.known.fixture.ts take to settle when the
// simulator gives them far more than the default 240 s: measured at v3.0.7 with
// a settle time of 3600 s of virtual time (groupSim.settle.test.ts repeats it).
// settledAfterS is the virtual time from the end of the relay's interference
// until the room first held one epoch and key (null: not within 3600 s; the
// agreement was not checked for how long it lasts). startOvers counts the
// "keys none" lines of the whole run, chaotic phase included.
//
// Of the 43 seeds, settled within 240 s: 0 (that is the list); within 480 s: 16
// (honest 6 of 17, hostile 10 of 26); within 900 s: 26 (10, 16); within 3600 s:
// 35 (15, 20); never: 8 (honest 125 and 258; hostile 107, 150, 194, 292, 323,
// 362). They are not slow, they churn: see groupSim.settle.test.ts.
export interface Settle {
  relay: 'honest' | 'hostile'
  seed: number
  members: number
  settledAfterS: number | null
  startOvers: number
}

export const SETTLE: readonly Settle[] = [
  { relay: 'honest', seed: 30, members: 11, settledAfterS: 774, startOvers: 214 },
  { relay: 'honest', seed: 64, members: 11, settledAfterS: 241, startOvers: 64 },
  { relay: 'honest', seed: 109, members: 12, settledAfterS: 584, startOvers: 169 },
  { relay: 'honest', seed: 116, members: 12, settledAfterS: 281, startOvers: 109 },
  { relay: 'honest', seed: 119, members: 11, settledAfterS: 3062, startOvers: 725 },
  { relay: 'honest', seed: 125, members: 11, settledAfterS: null, startOvers: 855 },
  { relay: 'honest', seed: 148, members: 7, settledAfterS: 415, startOvers: 75 },
  { relay: 'honest', seed: 162, members: 11, settledAfterS: 2277, startOvers: 514 },
  { relay: 'honest', seed: 167, members: 12, settledAfterS: 1034, startOvers: 270 },
  { relay: 'honest', seed: 206, members: 9, settledAfterS: 1070, startOvers: 196 },
  { relay: 'honest', seed: 246, members: 9, settledAfterS: 1030, startOvers: 188 },
  { relay: 'honest', seed: 254, members: 12, settledAfterS: 244, startOvers: 61 },
  { relay: 'honest', seed: 258, members: 10, settledAfterS: null, startOvers: 418 },
  { relay: 'honest', seed: 276, members: 9, settledAfterS: 373, startOvers: 90 },
  { relay: 'honest', seed: 303, members: 12, settledAfterS: 247, startOvers: 89 },
  { relay: 'honest', seed: 344, members: 10, settledAfterS: 863, startOvers: 229 },
  { relay: 'honest', seed: 377, members: 8, settledAfterS: 544, startOvers: 103 },
  { relay: 'hostile', seed: 30, members: 11, settledAfterS: 1505, startOvers: 406 },
  { relay: 'hostile', seed: 43, members: 12, settledAfterS: 362, startOvers: 95 },
  { relay: 'hostile', seed: 59, members: 6, settledAfterS: 381, startOvers: 57 },
  { relay: 'hostile', seed: 73, members: 10, settledAfterS: 429, startOvers: 95 },
  { relay: 'hostile', seed: 89, members: 12, settledAfterS: 1083, startOvers: 356 },
  { relay: 'hostile', seed: 97, members: 8, settledAfterS: 253, startOvers: 71 },
  { relay: 'hostile', seed: 104, members: 12, settledAfterS: 484, startOvers: 131 },
  { relay: 'hostile', seed: 107, members: 12, settledAfterS: null, startOvers: 1080 },
  { relay: 'hostile', seed: 133, members: 8, settledAfterS: 488, startOvers: 114 },
  { relay: 'hostile', seed: 148, members: 7, settledAfterS: 438, startOvers: 64 },
  { relay: 'hostile', seed: 150, members: 9, settledAfterS: null, startOvers: 719 },
  { relay: 'hostile', seed: 194, members: 10, settledAfterS: null, startOvers: 439 },
  { relay: 'hostile', seed: 199, members: 12, settledAfterS: 607, startOvers: 212 },
  { relay: 'hostile', seed: 206, members: 9, settledAfterS: 610, startOvers: 131 },
  { relay: 'hostile', seed: 238, members: 11, settledAfterS: 835, startOvers: 180 },
  { relay: 'hostile', seed: 244, members: 11, settledAfterS: 1082, startOvers: 242 },
  { relay: 'hostile', seed: 292, members: 9, settledAfterS: null, startOvers: 714 },
  { relay: 'hostile', seed: 294, members: 10, settledAfterS: 261, startOvers: 106 },
  { relay: 'hostile', seed: 300, members: 10, settledAfterS: 400, startOvers: 135 },
  { relay: 'hostile', seed: 301, members: 7, settledAfterS: 323, startOvers: 75 },
  { relay: 'hostile', seed: 310, members: 11, settledAfterS: 2048, startOvers: 563 },
  { relay: 'hostile', seed: 323, members: 11, settledAfterS: null, startOvers: 621 },
  { relay: 'hostile', seed: 332, members: 10, settledAfterS: 512, startOvers: 119 },
  { relay: 'hostile', seed: 362, members: 12, settledAfterS: null, startOvers: 441 },
  { relay: 'hostile', seed: 378, members: 10, settledAfterS: 375, startOvers: 106 },
  { relay: 'hostile', seed: 398, members: 11, settledAfterS: 373, startOvers: 152 },
]
