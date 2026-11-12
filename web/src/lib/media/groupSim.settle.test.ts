// What became of the seeds that were "still regrouping when the time is up" at
// v3.0.7 when they get an hour (table: groupSim.settle.fixture.ts).
//
// They were not slow, they churned (seed 258 on the honest relay, and the same
// counts in every seed that never settled). The room's committer removed a
// member that had been silent for the absence limit by ITS clock, and the
// others refused that commit as premature by theirs: they had joined (taken a
// welcome, or started over) less than the limit ago, and a joiner counts every
// member of the tree as alive from its join. In seed 258 the committer's last
// heartbeat of the silent member was at 1189.5 s, it removed at 1221.0 s, and
// the others, who had joined at 1195.6 s, refused at 25.4 s of silence against
// their 28 s. The committer was then one epoch ahead of everyone, its next
// commit was a "commit ahead", all the others started over, which reset their
// baselines, and the next removal half a minute later was refused again: 134
// refused removals in 1200 s, epoch 34 to 217, each member without keys for
// 370 to 420 s of the 1200. Now the committer waits until the newest member
// has been in for the limit (groupEntry.ts) and the same seed settles after
// 95 s with 11 start-overs.
//
// SIM_SETTLE=1 SIM_SETTLE_MS=3600000 repeats the measurement.
import { webcrypto } from 'node:crypto'
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'
import { KNOWN_HONEST, KNOWN_HOSTILE } from './groupSim.known.fixture'
import { SETTLE } from './groupSim.settle.fixture'
import { runSeed } from './groupSimRun.fixture'

const previous = globalThis.crypto
beforeAll(() => { vi.stubGlobal('crypto', webcrypto) })
afterAll(() => { vi.stubGlobal('crypto', previous) })

describe('the settle table', () => {
  it('has a row for every seed of the failure budget', () => {
    const listed = (relay: string) => SETTLE.filter(row => row.relay === relay).map(row => row.seed)
    for (const seed of KNOWN_HONEST) expect(listed('honest')).toContain(seed)
    for (const seed of KNOWN_HOSTILE) expect(listed('hostile')).toContain(seed)
  })
})

describe.skipIf(!process.env.SIM_SETTLE)('the settle time, measured again', () => {
  it.each(SETTLE.map(row => [row.relay, row.seed, row.after] as const))('%s relay, seed %i', async (relay, seed, expected) => {
    const result = await runSeed(seed, { relay, agreeWithinMs: Number(process.env.SIM_SETTLE_MS ?? 3_600_000) })
    expect(result.agreedAfterMs === null ? null : Math.round(result.agreedAfterMs / 1000)).toBe(expected)
  }, 900_000)
})
