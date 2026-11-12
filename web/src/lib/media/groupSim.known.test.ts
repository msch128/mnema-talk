// What the simulator finds that the protocol does not pass today: the seeds of
// groupSim.known.fixture.ts. Each one fails invariant 1 (the room does not come
// to hold one epoch and key in time) and none fails 2, 3 or 4.
//
// What is left, in short: a room that is still regrouping when the time is up.
// Hostile seed 109 ends in two groups, one of nine members and one of two, and
// settles after 415 s (groupSim.settle.fixture.ts). The 42 other seeds that
// used to be here (members that start over and are removed again and again,
// see groupEntry.ts) settle now.
//
// The tests are expected failures (it.fails): they run, and the day the
// protocol heals a case its test turns red and must move to groupSim.test.ts.
// groupSim.sweep.test.ts runs all seeds of 1..400 on both relays, nightly.
import { webcrypto } from 'node:crypto'
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'
import { KNOWN_HONEST, KNOWN_HOSTILE } from './groupSim.known.fixture'
import { report, runSeed } from './groupSimRun.fixture'

const previous = globalThis.crypto
beforeAll(() => { vi.stubGlobal('crypto', webcrypto) })
afterAll(() => { vi.stubGlobal('crypto', previous) })

describe('known: seeds that end in a split room', () => {
  it('has no seed on the honest relay (only delay, and members that join, leave, reload, sleep or reconnect)', () => {
    expect(KNOWN_HONEST).toEqual([])
  })

  // On the hostile relay, which also reorders, duplicates and drops per recipient.
  it.fails.each(KNOWN_HOSTILE)('hostile relay, seed %i: still regrouping when the time is up', async (seed) => {
    const result = await runSeed(seed, { relay: 'hostile' })
    expect(result.violations, report(result)).toEqual([])
  }, 60_000)
})
