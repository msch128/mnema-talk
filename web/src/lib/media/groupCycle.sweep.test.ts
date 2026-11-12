// Recurrence of the room's state before it agrees (groupCycle.fixture.ts), per seed:
//   SIM_CYCLE=1 [SIM_CYCLE_SEEDS=1..400 | a,b,c] [SIM_CYCLE_MS=900000] [SIM_CYCLE_OUT=<file>] npx vitest run src/lib/media/groupCycle.sweep.test.ts
// With SIM_CYCLE_OUT every seed's numbers are appended to the file as JSON lines.
import { appendFileSync } from 'node:fs'
import { webcrypto } from 'node:crypto'
import { afterAll, beforeAll, describe, it, vi } from 'vitest'
import { CycleDetector } from './groupCycle.fixture'
import { runSeed } from './groupSimRun.fixture'

const previous = globalThis.crypto
beforeAll(() => { vi.stubGlobal('crypto', webcrypto) })
afterAll(() => { vi.stubGlobal('crypto', previous) })

function seeds(setting: string): number[] {
  const range = /^(\d+)\.\.(\d+)$/.exec(setting)
  if (range) return Array.from({ length: Number(range[2]) - Number(range[1]) + 1 }, (_, i) => Number(range[1]) + i)
  return setting.split(',').map(Number)
}

describe.skipIf(!process.env.SIM_CYCLE)('recurring states per seed', () => {
  it.each(['honest', 'hostile'] as const)('%s relay', async (relay) => {
    const [index, count] = (process.env.SIM_SHARD ?? '0/1').split('/').map(Number) as [number, number]
    for (const seed of seeds(process.env.SIM_CYCLE_SEEDS ?? '1..400')) {
      if (seed % count !== index) continue
      const detector = new CycleDetector()
      const result = await runSeed(seed, { relay, agreeWithinMs: Number(process.env.SIM_CYCLE_MS ?? 900_000), watch: detector.observe })
      const top = detector.top()
      const row = { relay, seed, members: result.members, agreedAfterMs: result.agreedAfterMs, states: detector.states(), top: top?.visits ?? 0, topState: top?.state, firstAtS: top?.firstAtS, lastAtS: top?.lastAtS, firstTraceLine: top?.firstTraceLine }
      if (process.env.SIM_CYCLE_OUT) appendFileSync(process.env.SIM_CYCLE_OUT, `${JSON.stringify(row)}\n`)
      else console.log(JSON.stringify(row))
    }
  }, 7_200_000)
})
