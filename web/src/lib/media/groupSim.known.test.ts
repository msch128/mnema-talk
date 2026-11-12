// What the simulator finds that the protocol does not pass today. These tests
// record the failures; each one fails invariant 1 (the room does not come to
// hold one epoch and key in time) and none fails 2, 3 or 4.
//
// What is left, in short: rooms that are still regrouping when the time is
// up. Members that start over wait longer each time (10 s doubling to a
// minute), and a room that had to start over several times, large ones above
// all, is not done 240 s after the relay stopped interfering. (Two groups of
// several members become one now, and a leaf of a member that is elsewhere is
// swapped: see groupMerge.ts.)
//
// The tests are expected failures (it.fails): they run, and the day the
// protocol heals a case its test turns red and must move to groupSim.test.ts.
import { webcrypto } from 'node:crypto'
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'
import { KNOWN_HONEST, KNOWN_HOSTILE } from './groupSim.known.fixture'
import { report, runSeed } from './groupSimRun.fixture'

const previous = globalThis.crypto
beforeAll(() => { vi.stubGlobal('crypto', webcrypto) })
afterAll(() => { vi.stubGlobal('crypto', previous) })

describe('known: seeds that end in a split room', () => {
  // On the honest relay: only delay, and members that join, leave, reload, sleep or reconnect.
  it.fails.each([
    [30, 'still regrouping when the time is up'],
    [64, 'still regrouping when the time is up'],
    [116, 'still regrouping when the time is up'],
    [254, 'still regrouping when the time is up'],
  ])('honest relay, seed %i: %s', async (seed) => {
    const result = await runSeed(seed, { relay: 'honest' })
    expect(result.violations, report(result)).toEqual([])
  }, 60_000)

  // On the hostile relay, which also reorders, duplicates and drops per recipient.
  it.fails.each([
    [59, 'still regrouping when the time is up'],
    [30, 'still regrouping when the time is up'],
    [97, 'still regrouping when the time is up'],
    [294, 'still regrouping when the time is up'],
  ])('hostile relay, seed %i: %s', async (seed) => {
    const result = await runSeed(seed, { relay: 'hostile' })
    expect(result.violations, report(result)).toEqual([])
  }, 60_000)
})

// Every failing seed among 1..400, for whoever repairs the protocol. Skipped:
// each takes a full run, and the eight above stand for them. The list is in
// groupSim.known.fixture.ts; groupSim.sweep.test.ts runs all of it, nightly.
describe.skip('known: all failing seeds among 1..400', () => {
  it.each(KNOWN_HOSTILE)('hostile relay, seed %i', async (seed) => {
    const result = await runSeed(seed, { relay: 'hostile' })
    expect(result.violations, report(result)).toEqual([])
  }, 60_000)

  it.each(KNOWN_HONEST)('honest relay, seed %i', async (seed) => {
    const result = await runSeed(seed, { relay: 'honest' })
    expect(result.violations, report(result)).toEqual([])
  }, 60_000)
})
