// Crash points (groupCrash.fixture.ts): a tab dies after each await boundary of its protocol steps, in each of the
// scripts of a small room (groupExplore.fixture.ts), and the room must agree again once it is back.
//   smoke (normal run): one script of a room of 2, the first boundaries of each member
//   deep:  SIM_CRASH=1 [SIM_CRASH_MEMBERS=2|3|4] [SIM_CRASH_MAX=400] [SIM_SHARD=<i>/<n>] [SIM_CRASH_OUT=<file>] npx vitest run src/lib/media/groupCrash.sweep.test.ts
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

describe('a tab that dies at an await boundary of its protocol steps', () => {
  it('leaves the room able to agree again: the first boundaries of each member of a room of 2', async () => {
    const named = scripts(2)[0] as NamedScript
    const base = await run(named, null)
    expect(base.result.violations).toEqual([])
    const failures: string[] = []
    for (const [user, total] of base.boundaries.counts) {
      for (let ordinal = 1; ordinal <= Math.min(total, 6); ordinal++) {
        for (const seen of [true, false]) {
          const { result, boundaries } = await run(named, { user, ordinal, seen })
          if (result.violations.length && boundaries.crashed) failures.push(`${user.slice(-2)}#${ordinal} ${seen ? 'seen' : 'unseen'}\n${report(result, 25)}`)
        }
      }
    }
    expect(failures).toEqual([])
  }, 120_000)
})

describe.skipIf(!process.env.SIM_CRASH)('every boundary of every step, in every script of a small room', () => {
  it(`${process.env.SIM_CRASH_MEMBERS ?? '2'} members`, async () => {
    const members = Number(process.env.SIM_CRASH_MEMBERS ?? 2)
    const most = Number(process.env.SIM_CRASH_MAX ?? 400)
    let runs = 0
    let failed = 0
    for (const [number, named] of scripts(members).entries()) {
      if (number % count !== index) continue
      const base = await run(named, null)
      for (const [user, total] of base.boundaries.counts) {
        for (let ordinal = 1; ordinal <= Math.min(total, most); ordinal++) {
          for (const seen of [true, false]) {
            const { result, boundaries } = await run(named, { user, ordinal, seen })
            runs++
            if (!result.violations.length || !boundaries.crashed) continue
            failed++
            const row = { script: named.name, user: user.slice(-2), ordinal, of: total, seen, first: result.violations[0]?.slice(0, 300), tail: result.trace.slice(-30) }
            if (process.env.SIM_CRASH_OUT) appendFileSync(process.env.SIM_CRASH_OUT, `${JSON.stringify(row)}\n`)
            else console.log(`${row.script}: ${row.user}#${ordinal} ${seen}\n${report(result, 25)}`)
          }
        }
      }
    }
    if (process.env.SIM_CRASH_OUT) appendFileSync(process.env.SIM_CRASH_OUT, `${JSON.stringify({ summary: true, members, most, shard: `${index}/${count}`, runs, failed })}\n`)
  }, 36_000_000)
})
