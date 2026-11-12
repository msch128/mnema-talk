// Crash points (groupCrash.fixture.ts): a tab dies after each await boundary of its protocol steps, in each of the
// scripts of a small room (groupExplore.fixture.ts), and the room must agree again once it is back.
//   smoke (normal run): one script of a room of 2, every boundary of each member up to 60
//   Boundaries of one protocol step give the same run from outside (nothing it was about to send or adopt gets out), so the
//   count of different traces is the count of steps interrupted; it is recorded next to the count of boundaries and checked to be large.
//   deep:  SIM_CRASH=1 [SIM_CRASH_MEMBERS=2|3|4] [SIM_CRASH_MAX=400] [SIM_CRASH_STRIDE=1] [SIM_SHARD=<i>/<n>] [SIM_CRASH_OUT=<file>] npx vitest run src/lib/media/groupCrash.sweep.test.ts
import { appendFileSync } from 'node:fs'
import { webcrypto } from 'node:crypto'
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'
import { arm, crashingCrypto, disarm, type Boundaries } from './groupCrash.fixture'
import { scripts, type NamedScript } from './groupExplore.fixture'
import { report, runScript, type SimResult } from './groupSimRun.fixture'

// Every call of the group into ts-mls is an await boundary; WebCrypto's are counted through the stubbed global.
vi.mock('ts-mls', async (importOriginal) => {
  const { wrapModule } = await import('./groupCrash.fixture')
  return wrapModule(await importOriginal<typeof import('ts-mls')>())
})

const previous = globalThis.crypto
beforeAll(() => { vi.stubGlobal('crypto', crashingCrypto(webcrypto as Crypto)) })
afterAll(() => { vi.stubGlobal('crypto', previous) })

const [index, count] = (process.env.SIM_SHARD ?? '0/1').split('/').map(Number) as [number, number]

async function run(named: NamedScript, point: { user: string, ordinal: number, seen: boolean } | null): Promise<{ result: SimResult, boundaries: Boundaries }> {
  let boundaries: Boundaries | null = null
  try {
    const result = await runScript({ ...named.script, setup: (sim) => { boundaries = arm(sim, point) } })
    return { result, boundaries: boundaries as unknown as Boundaries }
  } finally {
    disarm()
  }
}

const mask = (trace: readonly string[]) => trace.map(line => line.replace(/(\d+):\d{5} \d{5} \d{5} \d{5}/g, '$1')).join('\n')

/** Every crash point of a script up to `most` per user; the runs that fail, how many ran and how many were different. */
async function crashAll(named: NamedScript, most: number, stride = 1) {
  const base = await run(named, null)
  const failures: string[] = []
  const distinct = new Set<string>()
  let runs = 0
  for (const [user, total] of base.boundaries.counts) {
    for (let ordinal = 1; ordinal <= Math.min(total, most); ordinal += stride) {
      for (const seen of [true, false]) {
        const { result, boundaries } = await run(named, { user, ordinal, seen })
        runs++
        expect(boundaries.crashed, `the crash at ${user.slice(-2)}#${ordinal} did not happen`).toBe(true)
        distinct.add(mask(result.trace))
        if (result.violations.length) failures.push(`${named.name}: ${user.slice(-2)}#${ordinal} ${seen ? 'seen' : 'unseen'}\n${report(result, 25)}`)
      }
    }
  }
  return { base, failures, distinct: distinct.size, runs }
}

describe('a tab that dies at an await boundary of its protocol steps', () => {
  it('leaves the room able to agree again, and different steps give different runs: a room of 2, every 3rd boundary', async () => {
    const { base, failures, distinct, runs } = await crashAll(scripts(2)[0] as NamedScript, 400, 3)
    // The wrapper counted: both members passed boundaries, many of them.
    expect(base.boundaries.counts.size).toBe(2)
    for (const total of base.boundaries.counts.values()) expect(total).toBeGreaterThan(100)
    expect(runs).toBeGreaterThan(200)
    // What a tab loses at a boundary shows outside only through what it would have sent or adopted at the end of
    // its step, so the boundaries of one step give one run: what must differ is the steps. Demand that they do.
    expect(distinct).toBeGreaterThan(10)
    expect(failures).toEqual([])
  }, 180_000)
})

describe.skipIf(!process.env.SIM_CRASH)('every boundary of every step, in every script of a small room', () => {
  it(`${process.env.SIM_CRASH_MEMBERS ?? '2'} members`, async () => {
    const members = Number(process.env.SIM_CRASH_MEMBERS ?? 2)
    const most = Number(process.env.SIM_CRASH_MAX ?? 400)
    let runs = 0
    let distinct = 0
    const failures: string[] = []
    for (const [number, named] of scripts(members).entries()) {
      if (number % count !== index) continue
      const done = await crashAll(named, most, Number(process.env.SIM_CRASH_STRIDE ?? 1))
      runs += done.runs
      distinct += done.distinct
      failures.push(...done.failures)
    }
    if (process.env.SIM_CRASH_OUT) appendFileSync(process.env.SIM_CRASH_OUT, `${JSON.stringify({ summary: true, members, most, shard: `${index}/${count}`, runs, distinct, failed: failures.length, first: failures[0]?.slice(0, 600) })}\n`)
    expect(runs).toBeGreaterThan(0)
    expect(distinct).toBeGreaterThan(10)
    expect(failures).toEqual([])
  }, 36_000_000)
})
