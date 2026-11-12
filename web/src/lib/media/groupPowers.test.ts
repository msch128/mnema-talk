// The server's other powers on a few seeds of the simulator (groupPowers.fixture.ts; the whole range runs in
// groupPowers.sweep.test.ts): replaying old handshakes that must cost nothing, and lying about the roster.
import { webcrypto } from 'node:crypto'
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'
import { HARMLESS, liarPlan, replayPlan } from './groupPowers.fixture'
import { occurrences, report, runSeed } from './groupSimRun.fixture'

const previous = globalThis.crypto
beforeAll(() => { vi.stubGlobal('crypto', webcrypto) })
afterAll(() => { vi.stubGlobal('crypto', previous) })

const SEEDS = [4, 9, 12, 14]

describe('a server that says old handshakes again', () => {
  it.each(SEEDS)('costs nobody their keys when they are heartbeats, capability messages and welcomes, honest relay, seed %i', async (seed) => {
    const result = await runSeed(seed, replayPlan({ relay: 'honest' }, HARMLESS))
    expect(result.violations, report(result)).toEqual([])
  }, 60_000)

  it.each(SEEDS)('and the room agrees once it stops, even when the replay was of everything, hostile relay, seed %i', async (seed) => {
    const result = await runSeed(seed, replayPlan({ relay: 'hostile' }))
    expect(result.violations.filter(line => line.includes('did not agree')), report(result)).toEqual([])
  }, 60_000)
})

describe('a server that lies about the roster while it interferes', () => {
  it('tells somebody a roster that is not the room, and leaves a room that agrees once the lies stop and one true roster follows, honest relay', async () => {
    let lies = 0
    for (const seed of [4, 9, 12]) {
      const result = await runSeed(seed, liarPlan({ relay: 'honest' }))
      lies += occurrences(result, /lie to /)
      expect(result.violations, report(result)).toEqual([])
    }
    expect(lies, 'lies told in these runs').toBeGreaterThan(0)
  }, 60_000)
})

describe('a server that replays', () => {
  it('really replays: the handshakes it kept are played again to every member', async () => {
    const result = await runSeed(4, replayPlan({ relay: 'honest' }, HARMLESS))
    expect(occurrences(result, /replay \d+ old handshakes/)).toBe(1)
    expect(occurrences(result, /replay \d+->m\d+#\d+ (heartbeat|capability|welcome)/)).toBeGreaterThan(50)
  }, 60_000)
})
