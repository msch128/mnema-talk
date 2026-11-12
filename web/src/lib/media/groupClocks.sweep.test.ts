// The seeds of the simulator with a clock per member (see ClockPlan in groupSimRun.fixture.ts):
//   SIM_CLOCKS=1 [SIM_CLOCKS_VARIANT=<name>] [SIM_SHARD=<i>/<n>] [SIM_CLOCKS_SEEDS=1..400|a,b] [SIM_CLOCKS_OUT=<file>] npx vitest run src/lib/media/groupClocks.sweep.test.ts
// Every row says whether the seed broke an invariant, and whether it also does with one clock for all members
// (a seed of the failure budget of groupSim.known.fixture.ts does: then the clocks changed nothing about it).
import { appendFileSync } from 'node:fs'
import { webcrypto } from 'node:crypto'
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'
import { availabilityOf } from './groupAvailability.fixture'
import { KNOWN_HONEST, KNOWN_HOSTILE } from './groupSim.known.fixture'
import { CLOCK_VARIANTS } from './groupClocks.fixture'
import { report, runSeed, type SimResult } from './groupSimRun.fixture'

const previous = globalThis.crypto
beforeAll(() => { vi.stubGlobal('crypto', webcrypto) })
afterAll(() => { vi.stubGlobal('crypto', previous) })

function seeds(setting: string): number[] {
  const range = /^(\d+)\.\.(\d+)$/.exec(setting)
  if (range) return Array.from({ length: Number(range[2]) - Number(range[1]) + 1 }, (_, i) => Number(range[1]) + i)
  return setting.split(',').map(Number)
}

const variants = Object.entries(CLOCK_VARIANTS).filter(([name]) => !process.env.SIM_CLOCKS_VARIANT || name === process.env.SIM_CLOCKS_VARIANT)

describe.skipIf(!process.env.SIM_CLOCKS)('seeds with a clock per member', () => {
  it.each(variants.flatMap(([name, clocks]) => (['honest', 'hostile'] as const).map(relay => [name, relay, clocks] as const)))('%s, %s relay', async (name, relay, clocks) => {
    const [index, count] = (process.env.SIM_SHARD ?? '0/1').split('/').map(Number) as [number, number]
    const known = relay === 'honest' ? KNOWN_HONEST : KNOWN_HOSTILE
    const failures: string[] = []
    for (const seed of seeds(process.env.SIM_CLOCKS_SEEDS ?? '1..400')) {
      if (seed % count !== index) continue
      const threw: SimResult = { seed, members: 0, events: 0, endMs: 0, agreedAfterMs: null, violations: [], trace: [] }
      const result = await runSeed(seed, { relay, clocks }).catch((err: unknown) => ({ ...threw, violations: [`threw: ${String(err)}`] }))
      const a = availabilityOf(result)
      const row = { variant: name, relay, seed, failed: result.violations.length > 0, known: known.includes(seed), agreedAfterMs: result.agreedAfterMs, first: result.violations[0]?.slice(0, 200), lost: a.memberMs ? a.lostMs / a.memberMs : 0 }
      if (process.env.SIM_CLOCKS_OUT) appendFileSync(process.env.SIM_CLOCKS_OUT, `${JSON.stringify(row)}\n`)
      else if (row.failed && !row.known) failures.push(`${name} ${relay} seed ${seed}: ${report(result, 12)}`)
    }
    expect(failures, 'seeds outside the failure budget that fail').toEqual([])
  }, 7_200_000)
})
