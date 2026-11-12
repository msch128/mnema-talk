import { describe, expect, it } from 'vitest'
import { availability, AvailabilityBudget, EPOCH_LIFE_MS, share } from './groupAvailability.fixture'

const at = (seconds: number, text: string) => `${seconds.toFixed(3).padStart(8)} ${text}`

describe('availability read from a simulator trace', () => {
  it('counts a joiner\'s wait for its first keys as no keys, but not as lost', () => {
    const a = availability([at(1, 'join m1#1'), at(4, 'm1#1 keys 1:11111 22222 33333 44444 {1}')], 10_000)
    expect(a).toMatchObject({ memberMs: 9000, noKeysMs: 3000, lostMs: 0, startOvers: 0, longestLostMs: 0 })
  })

  it('counts the time between "keys none" and the next keys as lost, and the start-over once', () => {
    const a = availability([
      at(0, 'join m1#1'), at(1, 'm1#1 keys 1:1 {1}'),
      at(5, 'm1#1 keys none'), at(6, 'm1#1 keys none'), at(9, 'm1#1 keys 3:2 {1}'),
    ], 20_000)
    expect(a).toMatchObject({ memberMs: 20_000, noKeysMs: 1000 + 4000, lostMs: 4000, startOvers: 1, longestLostMs: 4000 })
  })

  it('stops counting a tab when it leaves or is reloaded away, and starts the new tab afresh', () => {
    const a = availability([
      at(0, 'join m1#1'), at(2, 'm1#1 keys 1:1 {1}'),
      at(3, 'reload m1#1 (roster shows it)'), at(3, 'join m1#2'),
      at(5, 'm1#2 keys 1:2 {1}'), at(8, 'leave m1#2'),
    ], 100_000)
    expect(a).toMatchObject({ memberMs: 3000 + 5000, noKeysMs: 2000 + 2000, lostMs: 0 })
  })

  it('measures how long one epoch\'s keys were held, across a change of epoch and at the end of the run', () => {
    const a = availability([at(0, 'join m1#1'), at(1, 'm1#1 keys 1:1 {1}'), at(4, 'm1#1 keys 2:2 {1}')], 10_000)
    expect(a.longestEpochMs).toBe(6000)
  })

  it('flags a seed over its limit and a pool over the shard limit, and a known seed gets the wider limit', () => {
    const run = (lostMs: number, memberMs = 1000) => ({ memberMs, noKeysMs: lostMs, lostMs, startOvers: 1, longestLostMs: lostMs, longestEpochMs: 0 })
    const budget = new AvailabilityBudget('honest')
    budget.add(1, false, run(100))
    budget.add(2, true, run(600, 10_000))
    expect(budget.breaches()).toEqual([])
    budget.add(3, false, run(350))
    budget.add(4, true, run(700))
    expect(budget.breaches().map(line => line.replace(/\d+\.\d%/g, 'N%'))).toEqual([
      'seed 3: N% of member time lost its keys (limit 30% for a seed outside the failure budget)',
      'seed 4: N% of member time lost its keys (limit 65% for a seed inside the failure budget)',
    ])
    const pooled = new AvailabilityBudget('honest')
    for (const seed of [1, 2, 3]) pooled.add(seed, false, run(250))
    expect(pooled.breaches().map(line => line.replace(/\d+\.\d%/g, 'N%'))).toEqual(['pooled N% of member time lost its keys (limit 22%)'])
  })

  it('flags a key held for longer than a key may live', () => {
    const budget = new AvailabilityBudget('hostile')
    budget.add(5, false, { memberMs: 1000, noKeysMs: 0, lostMs: 0, startOvers: 0, longestLostMs: 0, longestEpochMs: EPOCH_LIFE_MS + 1000 })
    expect(budget.breaches()).toEqual([`seed 5: one epoch's keys were held for ${(EPOCH_LIFE_MS + 1000) / 1000} s (limit ${EPOCH_LIFE_MS / 1000} s)`])
  })

  it('has no share without member time', () => {
    expect(share(0, 0)).toBe(0)
    expect(share(1, 4)).toBe(0.25)
  })
})
