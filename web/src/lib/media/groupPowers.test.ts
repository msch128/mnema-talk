// The server's other powers on a few seeds of the simulator (groupPowers.fixture.ts; the whole range runs in
// groupPowers.sweep.test.ts): replaying old handshakes that must cost nothing, and lying about the roster.
import { webcrypto } from 'node:crypto'
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'
import { HARMLESS, liarPlan, replayPlan } from './groupPowers.fixture'
import { report, runSeed } from './groupSimRun.fixture'

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
  it.each([4, 9, 12])('leaves a room that agrees once the lies stop and one true roster follows, honest relay, seed %i', async (seed) => {
    const result = await runSeed(seed, liarPlan({ relay: 'honest' }))
    expect(result.violations, report(result)).toEqual([])
  }, 60_000)
})
