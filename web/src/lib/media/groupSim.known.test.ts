// What the simulator finds that the protocol does not pass today: the seeds of
// groupSim.known.fixture.ts, none at the moment. Each one fails invariant 1 (the
// room does not come to hold one epoch and key in time) and none fails 2, 3 or 4.
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
  it('keeps each list sorted and without a seed twice', () => {
    for (const list of [KNOWN_HONEST, KNOWN_HOSTILE]) expect(list).toEqual([...new Set(list)].sort((a, b) => a - b))
  })

  // On the honest relay: only delay, and members that join, leave, reload, sleep or reconnect.
  it.fails.each(KNOWN_HONEST)('honest relay, seed %i: still regrouping when the time is up', async (seed) => {
    const result = await runSeed(seed, { relay: 'honest' })
    expect(result.violations, report(result)).toEqual([])
  }, 60_000)

  // On the hostile relay, which also reorders, duplicates and drops per recipient.
  it.fails.each(KNOWN_HOSTILE)('hostile relay, seed %i: still regrouping when the time is up', async (seed) => {
    const result = await runSeed(seed, { relay: 'hostile' })
    expect(result.violations, report(result)).toEqual([])
  }, 60_000)
})
