// What the simulator found that the protocol does not pass today. Nothing here
// is fixed: these tests record the failures. Each one fails invariant 1 (the
// room does not come to hold one epoch and key) and none fails 2, 3 or 4.
//
// The split, in short: two members that each hold a group never merge. The
// group led by the room's lowest id lets in only whoever offers a key package;
// a member that holds a working group never offers; the lowest id never starts
// over. A group of one announces itself once per room and then stays silent.
//
// The tests are expected failures (it.fails): they run, and the day the
// protocol heals a case its test turns red and must move to groupSim.test.ts.
import { webcrypto } from 'node:crypto'
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'
import { report, runScript, runSeed } from './groupSimRun.fixture'

const previous = globalThis.crypto
beforeAll(() => { vi.stubGlobal('crypto', webcrypto) })
afterAll(() => { vi.stubGlobal('crypto', previous) })

describe('known: two members join a Talk of one at almost the same moment', () => {
  // The smallest case, on an honest relay: nothing is dropped, duplicated or
  // reordered, nobody leaves, every link is 40 ms.
  //  1. Member 2 is alone in the Talk and holds a group of one.
  //  2. Member 3 joins and offers its key package. Member 2 hears the room
  //     grow, announces its own key package (a group of one does) and adds 3.
  //  3. Member 1 joins 30 ms after 3, before member 2 has heard of 3. It gets
  //     3's key package and then 2's announcement: everyone else here offered
  //     and nobody is lower, so it founds a group of its own with 2 and 3 in it.
  //  4. Member 2 adds 1 to its group as well. Its welcome reaches 1 a moment
  //     after 1 founded; 1 now holds a group of three and ignores it. 1's
  //     welcome reaches 2 and 3, who hold a group with 1 in it and ignore that.
  //  5. From here on 1 seals with one key and 2 and 3 with another. Each side
  //     waits for the other, removes it for silence after the absence limit,
  //     and the room stays {1} and {2,3}: 1 announced itself to this room
  //     already and never does again.
  it.fails('ends with one group for the three of them', async () => {
    const result = await runScript({
      latency: { 1: 40, 2: 40, 3: 40 },
      steps: [[0, 'join', 2], [10_000, 'join', 3], [10_030, 'join', 1]],
      quietAt: 20_000,
    })
    expect(result.violations, report(result)).toEqual([])
  }, 60_000)

  it('does not split when the third member joins a little later', async () => {
    const result = await runScript({
      latency: { 1: 40, 2: 40, 3: 40 },
      steps: [[0, 'join', 2], [10_000, 'join', 3], [10_300, 'join', 1]],
      quietAt: 20_000,
    })
    expect(result.violations, report(result)).toEqual([])
  }, 60_000)
})

describe('known: seeds that end in a split room', () => {
  // On the honest relay: only delay, and members that join, leave, reload, sleep or reconnect.
  it.fails.each([
    [24, '{1} beside {3,4}'],
    [343, '{1} beside {2,3,4}'],
    [275, '{4,8} beside {1,5}'],
    [263, '{1,7} beside {3,6}'],
  ])('honest relay, seed %i: %s', async (seed) => {
    const result = await runSeed(seed, { relay: 'honest' })
    expect(result.violations, report(result)).toEqual([])
  }, 60_000)

  // On the hostile relay, which also reorders, duplicates and drops per recipient.
  it.fails.each([[286], [389], [211], [255]])('hostile relay, seed %i', async (seed) => {
    const result = await runSeed(seed, { relay: 'hostile' })
    expect(result.violations, report(result)).toEqual([])
  }, 60_000)
})

// Every failing seed among 1..400, for whoever repairs the protocol. Skipped:
// each takes a full run, and the eight above stand for them.
describe.skip('known: all failing seeds among 1..400', () => {
  const HOSTILE = [5, 16, 18, 20, 31, 48, 59, 64, 73, 78, 80, 84, 89, 102, 109, 116, 120, 124, 133, 145, 148, 152, 160, 180, 182, 185, 186, 199, 211, 212, 219, 227, 238, 245, 255, 256, 260, 262, 270, 273, 286, 294, 303, 322, 327, 328, 351, 352, 354, 357, 362, 367, 368, 383, 389, 390, 395]
  const HONEST = [24, 36, 37, 69, 73, 113, 131, 142, 149, 170, 178, 186, 193, 210, 215, 228, 238, 245, 254, 258, 263, 270, 275, 276, 287, 291, 302, 303, 306, 343, 360, 363, 368, 369, 395]

  it.each(HOSTILE)('hostile relay, seed %i', async (seed) => {
    const result = await runSeed(seed, { relay: 'hostile' })
    expect(result.violations, report(result)).toEqual([])
  }, 60_000)

  it.each(HONEST)('honest relay, seed %i', async (seed) => {
    const result = await runSeed(seed, { relay: 'honest' })
    expect(result.violations, report(result)).toEqual([])
  }, 60_000)
})
