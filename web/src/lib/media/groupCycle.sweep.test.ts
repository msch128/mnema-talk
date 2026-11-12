// Recurrence of the room's state before it agrees (groupCycle.fixture.ts), per seed, nightly:
//   SIM_CYCLE=1 [SIM_CYCLE_SEEDS=1..400 | a,b,c] [SIM_CYCLE_MS=240000] [SIM_SHARD=<i>/<n>] [SIM_CYCLE_OUT=<file>] npx vitest run src/lib/media/groupCycle.sweep.test.ts
// Two detectors watch every run until the room agrees:
//  - the whole-room state (Recurrence): per member the epoch distance, its group, how long ago it heard of each member,
//    its wait to start over, and the handshakes on their way. A return to such a state after more than a second, with
//    no agreement in between, is an exact cycle: the run would repeat forever. None may occur.
//  - the multiset (CycleDetector with `counts`): how many members hold nothing, hold the newest epoch, or are behind.
//    A room that churns returns to one of these again and again: seeds outside the failure budget stay below
//    CHURN_LIMIT, and the sweep must see the churn it exists to see (some seed of the budget reaches CHURN_SEEN).
// NOT EXAMINED by the nightly job: the second detector's "must see churn" check runs only on seeds of the failure
// budget of groupSim.known.fixture.ts, and that budget is empty since 3.0.9, so the check never runs there. The
// equal-epoch forks of H-L2-06 (seeds 5 and 120 of the clock sweep) are not runs of this sweep (no clock plan) and
// were not tried against the detector; a fork that stays put is no return to a state either.
import { appendFileSync } from 'node:fs'
import { webcrypto } from 'node:crypto'
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'
import { counts, CycleDetector, Recurrence } from './groupCycle.fixture'
import { KNOWN_HONEST, KNOWN_HOSTILE } from './groupSim.known.fixture'
import { runSeed } from './groupSimRun.fixture'

/** The most returns to one multiset a seed outside the failure budget shows (measured at v3.0.8: 10 honest, 11 hostile). */
export const CHURN_LIMIT = 14
/** Returns that mark churn: 16 of 17 honest and 25 of 26 hostile seeds of the failure budget reach it. */
export const CHURN_SEEN = 8

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
    const known = relay === 'honest' ? KNOWN_HONEST : KNOWN_HOSTILE
    const list = process.env.SIM_CYCLE_SEEDS ?? [...new Set([...known, ...Array.from({ length: 50 }, (_, i) => 8 * i + 3)])].join(',')
    const problems: string[] = []
    let ran = 0
    let churning = 0
    let inBudget = 0
    // Sharded by position in the list, not by seed value: the default list is all odd.
    for (const seed of seeds(list).filter((_, position) => position % count === index)) {
      const exact = new Recurrence()
      const coarse = new CycleDetector(counts)
      const result = await runSeed(seed, { relay, agreeWithinMs: Number(process.env.SIM_CYCLE_MS ?? 240_000), watch: (sim) => { exact.observe(sim); coarse.observe(sim) } })
      const top = coarse.top()?.visits ?? 0
      ran++
      if (known.includes(seed)) {
        inBudget++
        churning += top >= CHURN_SEEN ? 1 : 0
      } else if (top >= CHURN_LIMIT) {
        problems.push(`seed ${seed}: the room returned ${top} times to "${coarse.top()?.state}" without agreeing (limit ${CHURN_LIMIT})`)
      }
      if (exact.first && exact.first.againAtS - exact.first.firstAtS > 1) problems.push(`seed ${seed}: the whole room returned to the state of ${exact.first.firstAtS} s at ${exact.first.againAtS} s without agreeing: an exact cycle`)
      if (process.env.SIM_CYCLE_OUT) appendFileSync(process.env.SIM_CYCLE_OUT, `${JSON.stringify({ relay, seed, members: result.members, agreedAfterMs: result.agreedAfterMs, states: exact.states(), exactFirst: exact.first, coarseTop: top, coarseState: coarse.top()?.state })}\n`)
    }
    expect(ran, 'runs').toBeGreaterThan(0)
    // The detector must see what it is for: churn in the seeds of the failure budget (when this shard has some).
    if (inBudget >= 3) expect(churning, `${relay}: seeds of the failure budget in which the detector saw churn`).toBeGreaterThan(0)
    expect(problems).toEqual([])
  }, 7_200_000)
})
