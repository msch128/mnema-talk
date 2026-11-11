// What the simulator finds that the protocol does not pass today. These tests
// record the failures; each one fails invariant 1 (the room does not come to
// hold one epoch and key in time) and none fails 2, 3 or 4.
//
// What is left, in short: rooms that are still regrouping when the time is
// up. Members that start over wait longer each time (10 s doubling to a
// minute), and a room that had to start over several times, large ones above
// all, is not done 240 s after the relay stopped interfering. A few end with a
// leaf of a member that is elsewhere, which goes with the absence limit. (Two
// groups of several members become one now: see groupMerge.ts.)
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
    [116, 'still regrouping when the time is up'],
    [254, 'still regrouping when the time is up'],
    [371, 'one side still lists the other'],
    [195, 'two groups of several members'],
  ])('honest relay, seed %i: %s', async (seed) => {
    const result = await runSeed(seed, { relay: 'honest' })
    expect(result.violations, report(result)).toEqual([])
  }, 60_000)

  // On the hostile relay, which also reorders, duplicates and drops per recipient.
  it.fails.each([
    [18, 'still regrouping when the time is up'],
    [42, 'still regrouping when the time is up'],
    [294, 'still regrouping when the time is up'],
    [31, 'one side still lists the other'],
  ])('hostile relay, seed %i: %s', async (seed) => {
    const result = await runSeed(seed, { relay: 'hostile' })
    expect(result.violations, report(result)).toEqual([])
  }, 60_000)
})

// Every failing seed among 1..400, for whoever repairs the protocol. Skipped:
// each takes a full run, and the eight above stand for them.
describe.skip('known: all failing seeds among 1..400', () => {
  const HOSTILE = [18, 20, 31, 42, 64, 71, 73, 80, 82, 84, 97, 107, 120, 143, 148, 160, 178, 185, 194, 206, 235, 238, 244, 256, 270, 274, 292, 294, 322, 328, 351, 352, 357, 370, 374, 378]
  const HONEST = [116, 125, 154, 195, 245, 246, 254, 266, 306, 344, 371, 374, 378]

  it.each(HOSTILE)('hostile relay, seed %i', async (seed) => {
    const result = await runSeed(seed, { relay: 'hostile' })
    expect(result.violations, report(result)).toEqual([])
  }, 60_000)

  it.each(HONEST)('honest relay, seed %i', async (seed) => {
    const result = await runSeed(seed, { relay: 'honest' })
    expect(result.violations, report(result)).toEqual([])
  }, 60_000)
})
