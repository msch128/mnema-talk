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
    [357, 'two groups of several members'],
    [263, 'one side still lists the other'],
    [36, 'still regrouping when the time is up'],
  ])('honest relay, seed %i: %s', async (seed) => {
    const result = await runSeed(seed, { relay: 'honest' })
    expect(result.violations, report(result)).toEqual([])
  }, 60_000)

  // On the hostile relay, which also reorders, duplicates and drops per recipient.
  it.fails.each([
    [211, 'two groups of several members'],
    [31, 'two groups of several members'],
    [20, 'one side still lists the other'],
    [16, 'still regrouping when the time is up'],
  ])('hostile relay, seed %i: %s', async (seed) => {
    const result = await runSeed(seed, { relay: 'hostile' })
    expect(result.violations, report(result)).toEqual([])
  }, 60_000)
})

// Every failing seed among 1..400, for whoever repairs the protocol. Skipped:
// each takes a full run, and the eight above stand for them.
describe.skip('known: all failing seeds among 1..400', () => {
  const HOSTILE = [16, 18, 20, 31, 43, 48, 59, 64, 78, 80, 82, 84, 89, 109, 112, 116, 120, 124, 143, 148, 152, 175, 179, 182, 185, 186, 211, 212, 227, 235, 238, 250, 256, 260, 262, 268, 270, 273, 294, 301, 303, 322, 328, 345, 351, 352, 354, 357, 362, 367, 368, 383, 392, 395]
  const HONEST = [36, 37, 47, 58, 69, 73, 113, 119, 137, 142, 149, 178, 215, 228, 238, 245, 246, 254, 256, 263, 276, 291, 302, 303, 306, 357, 360, 369, 384, 395]

  it.each(HOSTILE)('hostile relay, seed %i', async (seed) => {
    const result = await runSeed(seed, { relay: 'hostile' })
    expect(result.violations, report(result)).toEqual([])
  }, 60_000)

  it.each(HONEST)('honest relay, seed %i', async (seed) => {
    const result = await runSeed(seed, { relay: 'honest' })
    expect(result.violations, report(result)).toEqual([])
  }, 60_000)
})
