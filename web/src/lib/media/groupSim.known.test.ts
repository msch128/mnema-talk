// What the simulator finds that the protocol does not pass today. These tests
// record the failures; each one fails invariant 1 (the room does not come to
// hold one epoch and key in time) and none fails 2, 3 or 4.
//
// What is left, in short: two groups that each have several members never
// merge. The group led by the room's lowest id lets in only whoever offers a
// key package; a member that holds a working group never offers; the lowest id
// never starts over. (A group of one beside a group is repaired: it says again
// that it is here on its maintenance tick.)
//
// The tests are expected failures (it.fails): they run, and the day the
// protocol heals a case its test turns red and must move to groupSim.test.ts.
import { webcrypto } from 'node:crypto'
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'
import { report, runSeed } from './groupSimRun.fixture'

const previous = globalThis.crypto
beforeAll(() => { vi.stubGlobal('crypto', webcrypto) })
afterAll(() => { vi.stubGlobal('crypto', previous) })

describe('known: seeds that end in a split room', () => {
  // On the honest relay: only delay, and members that join, leave, reload, sleep or reconnect.
  it.fails.each([
    [37, 'two groups of several members'],
    [2, 'two groups of several members'],
    [81, 'one side still lists the other'],
    [30, 'still regrouping when the time is up'],
  ])('honest relay, seed %i: %s', async (seed) => {
    const result = await runSeed(seed, { relay: 'honest' })
    expect(result.violations, report(result)).toEqual([])
  }, 60_000)

  // On the hostile relay, which also reorders, duplicates and drops per recipient.
  it.fails.each([
    [48, 'two groups of several members'],
    [11, 'two groups of several members'],
    [213, 'one side still lists the other'],
    [59, 'still regrouping when the time is up'],
  ])('hostile relay, seed %i: %s', async (seed) => {
    const result = await runSeed(seed, { relay: 'hostile' })
    expect(result.violations, report(result)).toEqual([])
  }, 60_000)
})

// Every failing seed among 1..400, for whoever repairs the protocol. Skipped:
// each takes a full run, and the eight above stand for them.
describe.skip('known: all failing seeds among 1..400', () => {
  const HOSTILE = [11, 30, 36, 41, 48, 59, 61, 62, 69, 73, 80, 83, 89, 95, 104, 107, 125, 126, 142, 148, 150, 156, 166, 175, 185, 195, 203, 213, 227, 238, 242, 244, 245, 254, 263, 277, 278, 292, 310, 315, 322, 323, 331, 335, 352, 354, 362, 365, 371, 373, 378, 380, 383, 399]
  const HONEST = [2, 5, 30, 37, 40, 64, 81, 98, 109, 113, 119, 120, 133, 148, 159, 162, 165, 195, 199, 210, 226, 228, 246, 254, 258, 260, 276, 282, 288, 299, 300, 303, 306, 354, 364, 373, 377]

  it.each(HOSTILE)('hostile relay, seed %i', async (seed) => {
    const result = await runSeed(seed, { relay: 'hostile' })
    expect(result.violations, report(result)).toEqual([])
  }, 60_000)

  it.each(HONEST)('honest relay, seed %i', async (seed) => {
    const result = await runSeed(seed, { relay: 'honest' })
    expect(result.violations, report(result)).toEqual([])
  }, 60_000)
})
