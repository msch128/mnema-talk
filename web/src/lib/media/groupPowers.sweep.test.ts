// The server's other powers on the simulator's seeds (groupPowers.fixture.ts):
//   SIM_POWERS=1 [SIM_POWER=replay|liar] [SIM_SHARD=<i>/<n>] [SIM_POWERS_SEEDS=1..400|a,b] [SIM_POWERS_OUT=<file>] npx vitest run src/lib/media/groupPowers.sweep.test.ts
// Every row says whether the seed broke an invariant, and whether it also fails without the power (the failure budget).
import { appendFileSync } from 'node:fs'
import { webcrypto } from 'node:crypto'
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'
import { availabilityOf } from './groupAvailability.fixture'
import { HARMLESS, liarPlan, replayPlan } from './groupPowers.fixture'
import { KNOWN_HONEST, KNOWN_HOSTILE } from './groupSim.known.fixture'
import { report, runSeed, type SimPlan, type SimResult } from './groupSimRun.fixture'

const previous = globalThis.crypto
beforeAll(() => { vi.stubGlobal('crypto', webcrypto) })
afterAll(() => { vi.stubGlobal('crypto', previous) })

function seeds(setting: string): number[] {
  const range = /^(\d+)\.\.(\d+)$/.exec(setting)
  if (range) return Array.from({ length: Number(range[2]) - Number(range[1]) + 1 }, (_, i) => Number(range[1]) + i)
  return setting.split(',').map(Number)
}

const POWERS: Record<string, (plan: SimPlan) => SimPlan> = {
  replay: plan => replayPlan(plan),
  'replay-harmless': plan => replayPlan(plan, HARMLESS),
  'replay-commit': plan => replayPlan(plan, ['commit']),
  'replay-welcome': plan => replayPlan(plan, ['welcome']),
  'replay-key-package': plan => replayPlan(plan, ['key-package']),
  'replay-heartbeat': plan => replayPlan(plan, ['heartbeat']),
  'replay-capability': plan => replayPlan(plan, ['capability']),
  liar: liarPlan,
}
const chosen = Object.entries(POWERS).filter(([name]) => !process.env.SIM_POWER || name === process.env.SIM_POWER)

describe.skipIf(!process.env.SIM_POWERS)('seeds with a server that does more than interfere', () => {
  it.each(chosen.flatMap(([name, plan]) => (['honest', 'hostile'] as const).map(relay => [name, relay, plan] as const)))('%s, %s relay', async (name, relay, plan) => {
    const [index, count] = (process.env.SIM_SHARD ?? '0/1').split('/').map(Number) as [number, number]
    const known = relay === 'honest' ? KNOWN_HONEST : KNOWN_HOSTILE
    const failures: string[] = []
    for (const seed of seeds(process.env.SIM_POWERS_SEEDS ?? '1..400')) {
      if (seed % count !== index) continue
      const threw: SimResult = { seed, members: 0, events: 0, endMs: 0, agreedAfterMs: null, violations: [], trace: [] }
      const result = await runSeed(seed, plan({ relay })).catch((err: unknown) => ({ ...threw, violations: [`threw: ${String(err)}`] }))
      const a = availabilityOf(result)
      const row = { power: name, relay, seed, failed: result.violations.length > 0, known: known.includes(seed), agreedAfterMs: result.agreedAfterMs, first: result.violations[0]?.slice(0, 240), lost: a.memberMs ? a.lostMs / a.memberMs : 0 }
      if (process.env.SIM_POWERS_OUT) appendFileSync(process.env.SIM_POWERS_OUT, `${JSON.stringify(row)}\n`)
      else if (row.failed && !row.known) failures.push(`${name} ${relay} seed ${seed}: ${report(result, 12)}`)
    }
    expect(failures, 'seeds outside the failure budget that fail').toEqual([])
  }, 7_200_000)
})
