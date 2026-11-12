// What becomes of the seeds that are "still regrouping when the time is up" if
// they get an hour (table: groupSim.settle.fixture.ts).
//
// What the traces show (seed 258 on the honest relay, and the same counts in
// every never-settling seed): they are not slow, they churn. The room's
// committer removes a member that has been silent for the absence limit by
// ITS clock, and the others refuse that commit as premature by theirs: they
// joined (took a welcome, or started over) less than the limit ago, and a
// joiner counts every member of the tree as alive from its join. The committer
// is then one epoch ahead of everyone, its next commit is a "commit ahead",
// and all the others start over, which resets their baselines, so the next
// removal, half a minute later, is refused again. In seed 258 that is 134
// refused removals in 1200 s, epoch 34 to 217, and each member without keys
// for 370 to 420 s of those 1200 (seed 125: 330 to 600 s). Members that hold
// no keys hear nobody; the ones that hold the current epoch hear each other.
//
// SIM_SETTLE=1 SIM_SETTLE_MS=3600000 repeats the measurement and prints it.
import { webcrypto } from 'node:crypto'
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'
import { KNOWN_HONEST, KNOWN_HOSTILE } from './groupSim.known.fixture'
import { SETTLE } from './groupSim.settle.fixture'
import { runSeed } from './groupSimRun.fixture'

const previous = globalThis.crypto
beforeAll(() => { vi.stubGlobal('crypto', webcrypto) })
afterAll(() => { vi.stubGlobal('crypto', previous) })

describe('the settle table', () => {
  it('lists exactly the seeds of the failure budget', () => {
    const listed = (relay: string) => SETTLE.filter(row => row.relay === relay).map(row => row.seed)
    expect(listed('honest')).toEqual(KNOWN_HONEST)
    expect(listed('hostile')).toEqual(KNOWN_HOSTILE)
  })
})

describe.skipIf(!process.env.SIM_SETTLE)('the settle time, measured again', () => {
  it.each(SETTLE.map(row => [row.relay, row.seed, row.settledAfterS] as const))('%s relay, seed %i', async (relay, seed, expected) => {
    const result = await runSeed(seed, { relay, agreeWithinMs: Number(process.env.SIM_SETTLE_MS ?? 3_600_000) })
    const seconds = result.agreedAfterMs === null ? null : Math.round(result.agreedAfterMs / 1000)
    console.log(`${relay} ${seed} members=${result.members} settledAfterS=${seconds} startOvers=${result.trace.filter(line => line.endsWith(' keys none')).length}`)
    expect(seconds).toBe(expected)
  }, 900_000)
})
