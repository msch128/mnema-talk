// Small rooms, every schedule up to a bound (groupExplore.fixture.ts).
//   smoke (normal test run): all 24 scripts of a room of 2, one deviation among the first 4 handshakes
//   deep:  SIM_EXPLORE=1 [SIM_EXPLORE_MEMBERS=2|3|4] [SIM_EXPLORE_DEPTH=1] [SIM_EXPLORE_POINTS=40] [SIM_SHARD=<i>/<n>] [SIM_EXPLORE_OUT=<file>] npx vitest run src/lib/media/groupExplore.sweep.test.ts
// A failing schedule is a counterexample: the script, the deviations and the end of the trace.
import { appendFileSync } from 'node:fs'
import { webcrypto } from 'node:crypto'
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'
import { describeSchedule, runSchedule, schedules, scripts } from './groupExplore.fixture'
import { occurrences, report } from './groupSimRun.fixture'

const previous = globalThis.crypto
beforeAll(() => { vi.stubGlobal('crypto', webcrypto) })
afterAll(() => { vi.stubGlobal('crypto', previous) })

const [index, count] = (process.env.SIM_SHARD ?? '0/1').split('/').map(Number) as [number, number]

describe('a room of 2, all 24 scripts, one deviation among the first 4 handshakes', () => {
  it('keeps the invariants', async () => {
    const all = scripts(2)
    expect(all).toHaveLength(24)
    const failures: string[] = []
    let runs = 0
    let dropped = 0
    let overtaken = 0
    for (const named of all) {
      for (const schedule of schedules(4, 1)) {
        const { result, asked } = await runSchedule(named, schedule)
        runs++
        dropped += occurrences(result, /\(the schedule\)/)
        overtaken += occurrences(result, /\bovertake /)
        // The deviation must be one that could happen: the run asked about at least as many handshakes as it deviated at.
        if (schedule.size) expect(asked, `${named.name}: ${describeSchedule(schedule)}`).toBeGreaterThan([...schedule.keys()][0] as number)
        if (result.violations.length) failures.push(`${named.name}: ${describeSchedule(schedule)}\n${report(result, 30)}`)
      }
    }
    expect(runs).toBeGreaterThan(24 * 4)
    // The relay really deviated: handshakes were lost and others overtook.
    expect(dropped, 'handshakes lost by the schedule').toBeGreaterThan(0)
    expect(overtaken, 'handshakes overtaken').toBeGreaterThan(0)
    expect(failures).toEqual([])
  }, 120_000)
})

describe.skipIf(!process.env.SIM_EXPLORE)('every schedule of a small room up to the bound', () => {
  it(`${process.env.SIM_EXPLORE_MEMBERS ?? '2'} members`, async () => {
    const members = Number(process.env.SIM_EXPLORE_MEMBERS ?? 2)
    const depth = Number(process.env.SIM_EXPLORE_DEPTH ?? 1)
    const horizon = Number(process.env.SIM_EXPLORE_POINTS ?? 40)
    let runs = 0
    let failed = 0
    for (const [number, named] of scripts(members).entries()) {
      if (number % count !== index) continue
      const base = await runSchedule(named, new Map())
      const points = Math.min(base.asked, horizon)
      for (const schedule of schedules(points, depth)) {
        const { result, asked } = schedule.size === 0 ? base : await runSchedule(named, schedule)
        runs++
        if (!result.violations.length) continue
        failed++
        const row = { script: named.name, schedule: describeSchedule(schedule), asked, first: result.violations[0]?.slice(0, 300), tail: result.trace.slice(-25) }
        if (process.env.SIM_EXPLORE_OUT) appendFileSync(process.env.SIM_EXPLORE_OUT, `${JSON.stringify(row)}\n`)
        else console.log(`${row.script}: ${row.schedule}\n${report(result, 25)}`)
      }
    }
    if (process.env.SIM_EXPLORE_OUT) appendFileSync(process.env.SIM_EXPLORE_OUT, `${JSON.stringify({ summary: true, members, depth, horizon, shard: `${index}/${count}`, runs, failed })}\n`)
    expect(runs, 'runs').toBeGreaterThan(0)
    expect(failed, 'schedules that break an invariant').toBe(0)
  }, 36_000_000)
})
