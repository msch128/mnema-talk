// The seeds of the simulator with a clock per member (see ClockPlan in groupSimRun.fixture.ts):
//   SIM_CLOCKS=1 [SIM_CLOCKS_VARIANT=<name>] [SIM_SHARD=<i>/<n>] [SIM_CLOCKS_SEEDS=1..400|a,b] [SIM_CLOCKS_OUT=<file>] npx vitest run src/lib/media/groupClocks.sweep.test.ts
// Every row says whether the seed broke an invariant, and whether it also does with one clock for all members
// (a seed of the failure budget of groupSim.known.fixture.ts does: then the clocks changed nothing about it).
import { appendFileSync } from 'node:fs'
import { webcrypto } from 'node:crypto'
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'
import { availabilityOf } from './groupAvailability.fixture'
import { KNOWN_CLOCKS } from './groupSweep.known.fixture'
import { CLOCK_VARIANTS } from './groupClocks.fixture'
import { occurrences, report, runSeed, STEP_MOVED, type SimResult } from './groupSimRun.fixture'

const previous = globalThis.crypto
beforeAll(() => { vi.stubGlobal('crypto', webcrypto) })
afterAll(() => { vi.stubGlobal('crypto', previous) })

function seeds(setting: string): number[] {
  const range = /^(\d+)\.\.(\d+)$/.exec(setting)
  if (range) return Array.from({ length: Number(range[2]) - Number(range[1]) + 1 }, (_, i) => Number(range[1]) + i)
  return setting.split(',').map(Number)
}

/** The seeds of `setting` this shard runs: sharded by position in the list, so a list of named seeds splits evenly too. */
function shardSeeds(setting: string): number[] {
  const [index, count] = (process.env.SIM_SHARD ?? '0/1').split('/').map(Number) as [number, number]
  return seeds(setting).filter((_, position) => position % count === index)
}

/** Runs `seed`; a run that throws counts as one that broke an invariant. */
async function runSafely(seed: number, plan: Parameters<typeof runSeed>[1]): Promise<SimResult> {
  const threw: SimResult = { seed, members: 0, events: 0, endMs: 0, agreedAfterMs: null, violations: [], trace: [] }
  return runSeed(seed, plan).catch((err: unknown) => ({ ...threw, violations: [`threw: ${String(err)}`] }))
}

/** The row of one seed, written to SIM_CLOCKS_OUT when set. */
function record(variant: string, relay: string, result: SimResult, listed: readonly number[]) {
  const a = availabilityOf(result)
  const row = { variant, relay, seed: result.seed, failed: result.violations.length > 0, known: listed.includes(result.seed), agreedAfterMs: result.agreedAfterMs, first: result.violations[0]?.slice(0, 200), lost: a.memberMs ? a.lostMs / a.memberMs : 0 }
  if (process.env.SIM_CLOCKS_OUT) appendFileSync(process.env.SIM_CLOCKS_OUT, `${JSON.stringify(row)}\n`)
  return row
}

const variants = Object.entries(CLOCK_VARIANTS).filter(([name]) => !process.env.SIM_CLOCKS_VARIANT || name === process.env.SIM_CLOCKS_VARIANT)

describe.skipIf(!process.env.SIM_CLOCKS)('seeds with a clock per member', () => {
  it.each(variants.flatMap(([name, clocks]) => (['honest', 'hostile'] as const).map(relay => [name, relay, clocks] as const)))('%s, %s relay', async (name, relay, clocks) => {
    const listed = KNOWN_CLOCKS[`${name}/${relay}`] ?? []
    const failing: number[] = []
    const ran: number[] = []
    const failures: string[] = []
    let skewed = 0
    let stepped = 0
    let runs = 0
    for (const seed of shardSeeds(process.env.SIM_CLOCKS_SEEDS ?? '1..400')) {
      ran.push(seed)
      const result = await runSafely(seed, { relay, clocks })
      runs++
      skewed += occurrences(result, /^\s*[\d.]+ clock of m\d+: offset -?[1-9]\d* ms|drift -?[1-9]\d* ppm/) > 0 ? 1 : 0
      stepped += occurrences(result, STEP_MOVED) > 0 ? 1 : 0
      const row = record(name, relay, result, listed)
      if (row.failed) failing.push(seed)
      if (!process.env.SIM_CLOCKS_OUT && row.failed && !row.known) failures.push(`${name} ${relay} seed ${seed}: ${report(result, 12)}`)
    }
    // The perturbation the variant names must have happened in the runs.
    if (clocks.offsetMs || clocks.driftPpm) expect(skewed, `${name}: runs with a skewed clock`).toBe(runs)
    if (clocks.stepMs) expect(stepped, `${name}: runs with a wall clock step`).toBeGreaterThan(runs / 4)
    // The failure budget may only shrink: a listed seed that passes now must leave the list (groupSweep.known.fixture.ts).
    const healed = process.env.SIM_CLOCKS_OUT ? [] : listed.filter(seed => ran.includes(seed) && !failing.includes(seed))
    expect(healed, 'seeds in the failure budget that pass now: delete them from the list').toEqual([])
    expect(failures, 'seeds outside the failure budget that fail').toEqual([])
  }, 7_200_000)
})
