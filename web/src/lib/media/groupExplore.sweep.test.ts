// Small rooms, every schedule up to a bound (groupExplore.fixture.ts).
//   smoke (normal test run): the scripts of a room of 2, one deviation among the first 4 handshakes
//   deep:  SIM_EXPLORE=1 [SIM_EXPLORE_MEMBERS=2|3|4] [SIM_EXPLORE_DEPTH=1] [SIM_EXPLORE_POINTS=40] [SIM_SHARD=<i>/<n>] [SIM_EXPLORE_OUT=<file>] npx vitest run src/lib/media/groupExplore.sweep.test.ts
// A failing schedule is a counterexample: the script, the deviations and the end of the trace.
import { appendFileSync } from 'node:fs'
import { webcrypto } from 'node:crypto'
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'
import { describeSchedule, runSchedule, schedules, scripts } from './groupExplore.fixture'
import { report } from './groupSimRun.fixture'

const previous = globalThis.crypto
beforeAll(() => { vi.stubGlobal('crypto', webcrypto) })
afterAll(() => { vi.stubGlobal('crypto', previous) })

const [index, count] = (process.env.SIM_SHARD ?? '0/1').split('/').map(Number) as [number, number]

describe('a room of 2, every script, one deviation among the first 4 handshakes', () => {
  it('keeps the invariants', async () => {
    const [first] = scripts(2)
    const named = first as NonNullable<typeof first>
    const failures: string[] = []
    for (const schedule of schedules(4, 1)) {
      const { result } = await runSchedule(named, schedule)
      if (result.violations.length) failures.push(`${named.name}: ${describeSchedule(schedule)}\n${report(result, 30)}`)
    }
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
  }, 36_000_000)
})
