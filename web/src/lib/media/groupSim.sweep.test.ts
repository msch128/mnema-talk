// The whole sweep of the simulator, seeds 1..400 on both relays, against the
// failure budget in groupSim.known.fixture.ts. Too slow for every push: it runs
// nightly (.github/workflows/sim-sweep.yml) and on demand, as
//   SIM_SWEEP=1 [SIM_SHARD=<index>/<count>] npx vitest run src/lib/media/groupSim.sweep.test.ts
// It fails when a seed outside the budget fails (a regression), and when a seed
// inside it passes (the budget must shrink: move the seed to groupSim.test.ts
// and delete it from the list).
import { webcrypto } from 'node:crypto'
import { appendFileSync, mkdirSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'
import { AvailabilityBudget, availabilityOf } from './groupAvailability.fixture'
import { KNOWN_HONEST, KNOWN_HOSTILE } from './groupSim.known.fixture'
import { report, runSeed } from './groupSimRun.fixture'

const SEEDS = 400
const previous = globalThis.crypto
beforeAll(() => { vi.stubGlobal('crypto', webcrypto) })
afterAll(() => { vi.stubGlobal('crypto', previous) })

// A shard setting that selects no seed would let the sweep pass without a run.
function shard(setting: string): readonly [number, number] {
  const match = /^(\d+)\/(\d+)$/.exec(setting)
  const [index, count] = match ? [Number(match[1]), Number(match[2])] : [Number.NaN, Number.NaN]
  if (!(count >= 1 && index >= 0 && index < count)) throw new Error(`SIM_SHARD must be <index>/<count> with index below count, got "${setting}"`)
  return [index, count]
}
const [index, count] = shard(process.env.SIM_SHARD ?? '0/1')
const mine = (seed: number) => seed % count === index

/**
 * The shard's sums go to SIM_POOL_OUT (a directory) for the job that pools all shards against the hard limit
 * (groupAvailability.pool.test.ts); its row of the per-shard table is only a warning, in the job summary when there is one.
 */
function record(budget: AvailabilityBudget) {
  const sums = budget.sums()
  if (process.env.SIM_POOL_OUT) {
    mkdirSync(process.env.SIM_POOL_OUT, { recursive: true })
    writeFileSync(join(process.env.SIM_POOL_OUT, `pool-${sums.relay}-${sums.shard}.json`), JSON.stringify(sums))
  }
  const note = budget.shardNote()
  if (!note) return
  console.warn(note)
  if (process.env.GITHUB_STEP_SUMMARY) appendFileSync(process.env.GITHUB_STEP_SUMMARY, `- warning: ${note}\n`)
}

describe.skipIf(!process.env.SIM_SWEEP)('seeds 1..400 against the failure budget', () => {
  it.each([['hostile', KNOWN_HOSTILE], ['honest', KNOWN_HONEST]] as const)('%s relay', async (relay, known) => {
    const failing: number[] = []
    const reports: string[] = []
    const budget = new AvailabilityBudget(relay, [index, count])
    for (let seed = 1; seed <= SEEDS; seed++) {
      if (!mine(seed)) continue
      // A run that throws counts as a failing seed and the sweep goes on.
      const outcome = await runSeed(seed, { relay }).then((result) => {
        budget.add(seed, known.includes(seed), availabilityOf(result))
        return result.violations.length ? report(result) : ''
      }, (err: unknown) => `threw: ${String(err)}`)
      if (!outcome) continue
      failing.push(seed)
      if (!known.includes(seed)) reports.push(`seed ${seed}: ${outcome}`)
    }
    record(budget)
    const regressions = failing.filter(seed => !known.includes(seed))
    const healed = known.filter(seed => mine(seed) && !failing.includes(seed))
    expect(budget.breaches(), `${relay} relay: member time without keys is over the budget LOST_BUDGET in groupAvailability.fixture.ts`).toEqual([])
    expect({ regressions, healed }, `${relay} relay: "regressions" fail outside the budget, "healed" are in the budget and pass now (move them to groupSim.test.ts)\n${reports.join('\n')}`).toEqual({ regressions: [], healed: [] })
  }, 3_600_000)
})
