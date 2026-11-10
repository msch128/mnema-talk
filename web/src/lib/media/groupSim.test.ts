// The media group protocol under a relay that interferes: a fixed set of seeds
// of the simulator in groupSim.fixture.ts. Every seed here passes; a seed that
// starts to fail is a regression in the protocol (or in the call around it).
// Seeds the protocol does not pass today are in groupSim.known.test.ts.
import { webcrypto } from 'node:crypto'
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'
import { RENEWAL_RACE, renewalsByEpoch, report, runScript, runSeed, someoneStartedOver, type SimPlan, type SimResult } from './groupSimRun.fixture'

const previous = globalThis.crypto
beforeAll(() => { vi.stubGlobal('crypto', webcrypto) })
afterAll(() => { vi.stubGlobal('crypto', previous) })

// Two seeds per room size from 2 to 12 for each relay, drawn from seeds 1..400.
const HOSTILE = [14, 17, 26, 30, 36, 70, 104, 140, 153, 158, 162, 166, 206, 216, 221, 224, 253, 290, 321, 350, 372, 377]
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

// Seeds and a script that ended in a split room until a group of one learned to
// say again that it is here, and the lowest newcomer to wait for a welcome.
describe('rooms that used to split', () => {
  it('two members join a Talk of one at almost the same moment and end in one group', async () => {
    // Member 1 joins 30 ms after member 3, gets 3's offer and the announcement of member 2, who was
    // alone: it used to found a group of its own at once, while 2 let both of them into its group.
    const result = await runScript({
      latency: { 1: 40, 2: 40, 3: 40 },
      steps: [[0, 'join', 2], [10_000, 'join', 3], [10_030, 'join', 1]],
      quietAt: 20_000,
    })
    expect(result.violations, report(result)).toEqual([])
    expect(someoneStartedOver(result), report(result, 200)).toBe(false)
  }, 60_000)

  it('two members that each ended alone in a group of one find each other', async () => {
    // Only joins and leaves on a link that stalls now and then. Both groups shrank to one on a timer,
    // so neither stepped aside nor spoke again, and the two stayed apart.
    await expectHolds(73, { relay: 'honest', members: 5, actions: ['join', 'leave'] })
  }, 60_000)

  it.each([24, 343, 275])('honest relay, seed %i', async (seed) => {
    await expectHolds(seed, { relay: 'honest' })
  }, 60_000)

  it.each([286, 389, 255])('hostile relay, seed %i', async (seed) => {
    await expectHolds(seed, { relay: 'hostile' })
  }, 60_000)
})

describe('two members that renew their leaf for the same epoch', () => {
  it('end on the lower id\'s renewal, and nobody loses its group', async () => {
    const result = await runScript(RENEWAL_RACE)
    expect(result.violations, report(result)).toEqual([])
    // The race did happen: two members sent a renewal for one epoch.
    expect([...renewalsByEpoch(result).values()].some(senders => senders.size === 2)).toBe(true)
    expect(someoneStartedOver(result), report(result, 200)).toBe(false)
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
