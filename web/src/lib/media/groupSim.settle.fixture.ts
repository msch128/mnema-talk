// How long the seeds that were in the failure budget at v3.0.7 take to settle
// when the simulator gives them far more than the default 240 s (3600 s of
// virtual time), before and after the committer waits for the newest member
// before it removes anyone (groupEntry.ts). `before` and `after` are the
// virtual seconds from the end of the relay's interference until the room
// first held one epoch and key (null: not within 3600 s; undefined: it passed
// the default run and was not measured; how long it then lasts was not
// checked). startOvers counts the "keys none" lines of the whole run, chaotic
// phase included. groupSim.settle.test.ts repeats the measurement.
//
// Before, of the 43 seeds: settled within 900 s: 26 (honest 10 of 17, hostile
// 16 of 26); within 3600 s: 35; never: 8 (honest 125 and 258; hostile 107,
// 150, 194, 292, 323, 362). After: 42 settle within 195 s; hostile seed 109,
// which passed the default run before, settles after 415 s. They had not been
// slow, they had churned (see groupSim.settle.test.ts).
export interface Settle {
  relay: 'honest' | 'hostile'
  seed: number
  members: number
  before: number | null | undefined
  after: number | null
  startOversBefore: number | undefined
  startOversAfter: number
}

export const SETTLE: readonly Settle[] = [
  { relay: 'honest', seed: 30, members: 11, before: 774, after: 38, startOversBefore: 214, startOversAfter: 19 },
  { relay: 'honest', seed: 64, members: 11, before: 241, after: 40, startOversBefore: 64, startOversAfter: 11 },
  { relay: 'honest', seed: 109, members: 12, before: 584, after: 21, startOversBefore: 169, startOversAfter: 17 },
  { relay: 'honest', seed: 116, members: 12, before: 281, after: 17, startOversBefore: 109, startOversAfter: 17 },
  { relay: 'honest', seed: 119, members: 11, before: 3062, after: 97, startOversBefore: 725, startOversAfter: 17 },
  { relay: 'honest', seed: 125, members: 11, before: null, after: 77, startOversBefore: 855, startOversAfter: 20 },
  { relay: 'honest', seed: 148, members: 7, before: 415, after: 5, startOversBefore: 75, startOversAfter: 17 },
  { relay: 'honest', seed: 162, members: 11, before: 2277, after: 42, startOversBefore: 514, startOversAfter: 14 },
  { relay: 'honest', seed: 167, members: 12, before: 1034, after: 56, startOversBefore: 270, startOversAfter: 21 },
  { relay: 'honest', seed: 206, members: 9, before: 1070, after: 34, startOversBefore: 196, startOversAfter: 19 },
  { relay: 'honest', seed: 246, members: 9, before: 1030, after: 60, startOversBefore: 188, startOversAfter: 23 },
  { relay: 'honest', seed: 254, members: 12, before: 244, after: 27, startOversBefore: 61, startOversAfter: 19 },
  { relay: 'honest', seed: 258, members: 10, before: null, after: 95, startOversBefore: 418, startOversAfter: 11 },
  { relay: 'honest', seed: 276, members: 9, before: 373, after: 63, startOversBefore: 90, startOversAfter: 12 },
  { relay: 'honest', seed: 303, members: 12, before: 247, after: 83, startOversBefore: 89, startOversAfter: 29 },
  { relay: 'honest', seed: 344, members: 10, before: 863, after: 57, startOversBefore: 229, startOversAfter: 14 },
  { relay: 'honest', seed: 377, members: 8, before: 544, after: 21, startOversBefore: 103, startOversAfter: 19 },
  { relay: 'hostile', seed: 30, members: 11, before: 1505, after: 118, startOversBefore: 406, startOversAfter: 39 },
  { relay: 'hostile', seed: 43, members: 12, before: 362, after: 28, startOversBefore: 95, startOversAfter: 31 },
  { relay: 'hostile', seed: 59, members: 6, before: 381, after: 14, startOversBefore: 57, startOversAfter: 22 },
  { relay: 'hostile', seed: 73, members: 10, before: 429, after: 135, startOversBefore: 95, startOversAfter: 35 },
  { relay: 'hostile', seed: 89, members: 12, before: 1083, after: 142, startOversBefore: 356, startOversAfter: 57 },
  { relay: 'hostile', seed: 97, members: 8, before: 253, after: 35, startOversBefore: 71, startOversAfter: 38 },
  { relay: 'hostile', seed: 104, members: 12, before: 484, after: 177, startOversBefore: 131, startOversAfter: 51 },
  { relay: 'hostile', seed: 107, members: 12, before: null, after: 193, startOversBefore: 1080, startOversAfter: 51 },
  { relay: 'hostile', seed: 109, members: 12, before: undefined, after: 415, startOversBefore: undefined, startOversAfter: 49 },
  { relay: 'hostile', seed: 133, members: 8, before: 488, after: 89, startOversBefore: 114, startOversAfter: 36 },
  { relay: 'hostile', seed: 148, members: 7, before: 438, after: 18, startOversBefore: 64, startOversAfter: 21 },
  { relay: 'hostile', seed: 150, members: 9, before: null, after: 83, startOversBefore: 719, startOversAfter: 41 },
  { relay: 'hostile', seed: 194, members: 10, before: null, after: 81, startOversBefore: 439, startOversAfter: 31 },
  { relay: 'hostile', seed: 199, members: 12, before: 607, after: 119, startOversBefore: 212, startOversAfter: 52 },
  { relay: 'hostile', seed: 206, members: 9, before: 610, after: 131, startOversBefore: 131, startOversAfter: 35 },
  { relay: 'hostile', seed: 238, members: 11, before: 835, after: 62, startOversBefore: 180, startOversAfter: 26 },
  { relay: 'hostile', seed: 244, members: 11, before: 1082, after: 22, startOversBefore: 242, startOversAfter: 26 },
  { relay: 'hostile', seed: 292, members: 9, before: null, after: 80, startOversBefore: 714, startOversAfter: 39 },
  { relay: 'hostile', seed: 294, members: 10, before: 261, after: 51, startOversBefore: 106, startOversAfter: 44 },
  { relay: 'hostile', seed: 300, members: 10, before: 400, after: 24, startOversBefore: 135, startOversAfter: 42 },
  { relay: 'hostile', seed: 301, members: 7, before: 323, after: 24, startOversBefore: 75, startOversAfter: 23 },
  { relay: 'hostile', seed: 310, members: 11, before: 2048, after: 161, startOversBefore: 563, startOversAfter: 57 },
  { relay: 'hostile', seed: 323, members: 11, before: null, after: 106, startOversBefore: 621, startOversAfter: 47 },
  { relay: 'hostile', seed: 332, members: 10, before: 512, after: 36, startOversBefore: 119, startOversAfter: 34 },
  { relay: 'hostile', seed: 362, members: 12, before: null, after: 44, startOversBefore: 441, startOversAfter: 29 },
  { relay: 'hostile', seed: 378, members: 10, before: 375, after: 143, startOversBefore: 106, startOversAfter: 32 },
  { relay: 'hostile', seed: 398, members: 11, before: 373, after: 195, startOversBefore: 152, startOversAfter: 57 },
]
