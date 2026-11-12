import { describe, expect, it } from 'vitest'
import { availability, share } from './groupAvailability.fixture'

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

  it('has no share without member time', () => {
    expect(share(0, 0)).toBe(0)
    expect(share(1, 4)).toBe(0.25)
  })
})
