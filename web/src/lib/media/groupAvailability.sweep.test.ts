// The share of member time without keys, per seed (groupAvailability.fixture.ts).
//   SIM_AVAIL=1 [SIM_SHARD=<i>/<n>] [SIM_AVAIL_OUT=<file>] npx vitest run src/lib/media/groupAvailability.sweep.test.ts
// With SIM_AVAIL_OUT every seed's numbers are appended to the file as JSON lines.
import { appendFileSync } from 'node:fs'
import { webcrypto } from 'node:crypto'
import { afterAll, beforeAll, describe, it, vi } from 'vitest'
import { availabilityOf, describeAvailability } from './groupAvailability.fixture'
import { counts, CycleDetector } from './groupCycle.fixture'
import { runSeed } from './groupSimRun.fixture'

const previous = globalThis.crypto
beforeAll(() => { vi.stubGlobal('crypto', webcrypto) })
afterAll(() => { vi.stubGlobal('crypto', previous) })

const [index, count] = (process.env.SIM_SHARD ?? '0/1').split('/').map(Number) as [number, number]
const FROM = Number(process.env.SIM_AVAIL_FROM ?? 1)
const TO = Number(process.env.SIM_AVAIL_TO ?? 400)
const ONLY = process.env.SIM_AVAIL_SEEDS?.split(',').map(Number)

describe.skipIf(!process.env.SIM_AVAIL)('availability per seed', () => {
  it.each(['honest', 'hostile'] as const)('%s relay', async (relay) => {
    for (let seed = FROM; seed <= TO; seed++) {
      if (seed % count !== index || (ONLY && !ONLY.includes(seed))) continue
      const detector = new CycleDetector()
      const coarse = new CycleDetector(counts)
      const result = await runSeed(seed, { relay, watch: (sim) => { detector.observe(sim); coarse.observe(sim) } })
      const a = availabilityOf(result)
      if (process.env.SIM_AVAIL_OUT) appendFileSync(process.env.SIM_AVAIL_OUT, `${JSON.stringify({ relay, seed, members: result.members, violations: result.violations.length, agreedAfterMs: result.agreedAfterMs, ...a, states: detector.states(), top: detector.top()?.visits ?? 0, topState: detector.top()?.state, coarseTop: coarse.top()?.visits ?? 0, coarseState: coarse.top()?.state, coarseStates: coarse.states() })}\n`)
      else console.log(relay, seed, describeAvailability(a))
    }
  }, 3_600_000)
})
