// The server's other powers on the simulator's seeds (groupPowers.fixture.ts):
//   SIM_POWERS=1 [SIM_POWER=replay|liar] [SIM_SHARD=<i>/<n>] [SIM_POWERS_SEEDS=1..400|a,b] [SIM_POWERS_OUT=<file>] npx vitest run src/lib/media/groupPowers.sweep.test.ts
// Every row says whether the seed broke an invariant, and whether it also fails without the power (the failure budget).
import { appendFileSync } from 'node:fs'
import { webcrypto } from 'node:crypto'
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'
import { availabilityOf } from './groupAvailability.fixture'
import { HARMLESS, liarPlan, replayPlan } from './groupPowers.fixture'
import { KNOWN_POWERS } from './groupSweep.known.fixture'
import { LIE_DELIVERED, occurrences, REPLAY_DELIVERED, report, runSeed, type SimPlan, type SimResult } from './groupSimRun.fixture'

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
async function runSafely(seed: number, plan: SimPlan): Promise<SimResult> {
  const threw: SimResult = { seed, members: 0, events: 0, endMs: 0, agreedAfterMs: null, violations: [], trace: [] }
  return runSeed(seed, plan).catch((err: unknown) => ({ ...threw, violations: [`threw: ${String(err)}`] }))
}

/** The row of one seed, written to SIM_POWERS_OUT when set. */
function record(power: string, relay: string, result: SimResult, listed: readonly number[]) {
  const a = availabilityOf(result)
  const row = { power, relay, seed: result.seed, failed: result.violations.length > 0, known: listed.includes(result.seed), agreedAfterMs: result.agreedAfterMs, first: result.violations[0]?.slice(0, 240), lost: a.memberMs ? a.lostMs / a.memberMs : 0 }
  if (process.env.SIM_POWERS_OUT) appendFileSync(process.env.SIM_POWERS_OUT, `${JSON.stringify(row)}\n`)
  return row
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
    const listed = KNOWN_POWERS[`${name}/${relay}`] ?? []
    const failing: number[] = []
    const ran: number[] = []
    const failures: string[] = []
    // What the power claims to do must show its effect in the traces: lies delivered, old handshakes that reached a member.
    const claimed = name === 'liar' ? LIE_DELIVERED : REPLAY_DELIVERED
    let happened = 0
    let agreed = 0
    for (const seed of shardSeeds(process.env.SIM_POWERS_SEEDS ?? '1..400')) {
      ran.push(seed)
      const result = await runSafely(seed, plan({ relay }))
      happened += occurrences(result, claimed) > 0 ? 1 : 0
      agreed += result.agreedAfterMs !== null ? 1 : 0
      const row = record(name, relay, result, listed)
      if (row.failed) failing.push(seed)
      if (!process.env.SIM_POWERS_OUT && row.failed && !row.known) failures.push(`${name} ${relay} seed ${seed}: ${report(result, 12)}`)
    }
    // A sweep in which the power never happened tested nothing (a power needs a room that agreed first, for a replay).
    expect(happened, `${name}: runs in which the power happened`).toBeGreaterThan(Math.floor(agreed / 4))
    // The failure budget may only shrink: a listed seed that passes now must leave the list (groupSweep.known.fixture.ts).
    const healed = process.env.SIM_POWERS_OUT ? [] : listed.filter(seed => ran.includes(seed) && !failing.includes(seed))
    expect(healed, 'seeds in the failure budget that pass now: delete them from the list').toEqual([])
    expect(failures, 'seeds outside the failure budget that fail').toEqual([])
  }, 7_200_000)
})
