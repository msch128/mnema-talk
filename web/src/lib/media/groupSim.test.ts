// The media group protocol under a relay that interferes: a fixed set of seeds
// of the simulator in groupSim.fixture.ts. Every seed here passes; a seed that
// starts to fail is a regression in the protocol (or in the call around it).
// Seeds the protocol does not pass today are in groupSim.known.test.ts.
import { webcrypto } from 'node:crypto'
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'
import { report, runSeed, type SimPlan, type SimResult } from './groupSimRun.fixture'

const previous = globalThis.crypto
beforeAll(() => { vi.stubGlobal('crypto', webcrypto) })
afterAll(() => { vi.stubGlobal('crypto', previous) })

// Two seeds per room size from 2 to 12 for each relay, drawn from seeds 1..400.
const HOSTILE = [14, 17, 26, 30, 43, 82, 104, 140, 153, 158, 162, 166, 206, 216, 221, 224, 253, 290, 321, 350, 372, 377]
const HONEST = [9, 33, 38, 46, 50, 51, 68, 79, 88, 117, 122, 140, 156, 159, 199, 242, 244, 279, 322, 332, 335, 350]

async function expectHolds(seed: number, plan: SimPlan): Promise<SimResult> {
  const result = await runSeed(seed, plan)
  expect(result.violations, report(result)).toEqual([])
  return result
}

/** The trace without what real key material makes different from run to run. */
function shape(result: SimResult): string[] {
  return result.trace.map(line => line.replace(/(\d+):\d{5} \d{5} \d{5} \d{5}/g, '$1'))
}

describe('the media group while the relay reorders, delays, duplicates and drops handshakes', () => {
  it.each(HOSTILE)('agrees on one epoch and key and keeps the rules, seed %i', async (seed) => {
    const result = await expectHolds(seed, { relay: 'hostile' })
    expect(result.agreedAfterMs).not.toBeNull()
  }, 60_000)
})

describe('the media group on an honest relay that only delays, while members come and go', () => {
  it.each(HONEST)('agrees on one epoch and key and keeps the rules, seed %i', async (seed) => {
    const result = await expectHolds(seed, { relay: 'honest' })
    expect(result.agreedAfterMs).not.toBeNull()
  }, 60_000)
})

describe('the simulator', () => {
  it('runs a seed the same way twice', async () => {
    const first = await runSeed(26)
    const second = await runSeed(26)
    expect(shape(second)).toEqual(shape(first))
    expect(first.events).toBeGreaterThan(500)
  }, 60_000)

  it('covers rooms of 2 to 12 members and every kind of interference', async () => {
    const seen = new Set<string>()
    const sizes = new Set<number>()
    for (const seed of [14, 153, 224]) {
      const result = await runSeed(seed)
      sizes.add(result.members)
      for (const line of result.trace) {
        const word = /^\s*[\d.]+ (\w+)/.exec(line)?.[1] ?? ''
        seen.add(line.includes('(copy)') ? 'copy' : word)
      }
    }
    for (const word of ['join', 'leave', 'reload', 'sleep', 'reconnect', 'drop', 'copy', 'deliver']) expect([...seen]).toContain(word)
    expect(Math.min(...sizes)).toBeGreaterThanOrEqual(2)
    expect(Math.max(...sizes)).toBeLessThanOrEqual(12)
  }, 60_000)
})
