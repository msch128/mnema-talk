import { describe, expect, it } from 'vitest'
import { counts, CycleDetector, deep, Recurrence, signature } from './groupCycle.fixture'
import type { GroupSim } from './groupSim.fixture'

interface Fake { id: string, current: { epoch: number, members: string[] } | null, staleAt: number | null }
const id = (n: number) => `00000000-0000-4000-8000-${n.toString(16).padStart(12, '0')}`
const room = (members: Fake[], at = 0) => ({ live: () => members, now: Date.UTC(2026, 0, 1) + at, trace: [] as string[] }) as unknown as GroupSim
const keyed = (n: number, epoch: number, size: number): Fake => ({ id: id(n), current: { epoch, members: Array.from({ length: size }, (_, i) => id(i + 1)) }, staleAt: null })
const none = (n: number): Fake => ({ id: id(n), current: null, staleAt: 0 })

describe('the state a member can feel', () => {
  it('names epochs by their distance to the newest, so a climbing epoch is the same state', () => {
    expect(signature(room([keyed(1, 7, 2), none(2)]))).toBe('m1:0/2 m2:none')
    expect(signature(room([keyed(1, 70, 2), none(2)]))).toBe('m1:0/2 m2:none')
    expect(signature(room([keyed(1, 8, 2), keyed(2, 7, 3)]))).toBe('m1:0/2 m2:-1/3')
  })

  it('treats a member that lost its group as holding none, whatever it held before', () => {
    const lost: Fake = { id: id(2), current: { epoch: 3, members: [id(1), id(2)] }, staleAt: 5 }
    expect(signature(room([keyed(1, 3, 2), lost]))).toBe('m1:0/2 m2:none')
  })
})

describe('the detector of recurring states', () => {
  it('counts a state again only when the room left it and came back, and says when', () => {
    const detector = new CycleDetector()
    const a = [keyed(1, 5, 2), none(2)]
    const b = [keyed(1, 6, 2), keyed(2, 5, 2)]
    for (const [state, at] of [[a, 0], [a, 1000], [b, 2000], [a, 3000], [b, 4000], [a, 9000]] as const) detector.observe(room([...state], at))
    expect(detector.states()).toBe(2)
    expect(detector.top()).toMatchObject({ state: 'm1:0/2 m2:none', visits: 3, firstAtS: 0, lastAtS: 9 })
    expect(detector.recurring(3).map(visit => visit.visits)).toEqual([3])
    expect(detector.recurring(4)).toEqual([])
  })
})

describe('the multiset of the room', () => {
  it('counts members without keys, at the newest epoch (and the size of that group) and behind, whoever they are', () => {
    expect(counts(room([keyed(1, 7, 3), keyed(2, 7, 3), keyed(3, 6, 2), none(4)]))).toBe('1 none, 2 newest in a group of 3, 1 behind')
    expect(counts(room([keyed(1, 7, 3), none(2), keyed(3, 6, 2), keyed(4, 7, 3)]))).toBe('1 none, 2 newest in a group of 3, 1 behind')
  })
})

interface Inside { current: { epoch: number, verification: { members: string[] } } | null, alive: Map<string, number>, resets: { waitMs(key: string): number }, restarting: Set<string>, pending: Map<string, unknown>, heard: Set<string> }
const inside = (epoch: number | null, size: number, alive: [number, number][] = [], wait = 0): Inside => ({
  current: epoch === null ? null : { epoch, verification: { members: Array.from({ length: size }, (_, i) => id(i + 1)) } },
  alive: new Map(alive.map(([n, at]) => [id(n), at])), resets: { waitMs: () => wait }, restarting: new Set(), pending: new Map(), heard: new Set(),
})
const whole = (members: Inside[], at = 0, flight: { to: string, kind: string, due: number }[] = [], timers: { owner: string, due: number }[] = []) => ({
  live: () => members.map((group, index) => ({ id: id(index + 1), label: `m${index + 1}#1`, group })),
  asleep: () => false, now: Date.UTC(2026, 0, 1) + at, inFlight: () => flight, ownTimers: () => timers,
}) as unknown as GroupSim

describe('the whole-room state', () => {
  const start = Date.UTC(2026, 0, 1)
  it('is the same for the same epochs seen at another time, when what each member heard is as old', () => {
    const a = whole([inside(7, 2, [[2, start + 1000]]), inside(7, 2, [[1, start + 1000]])], 5000)
    const b = whole([inside(70, 2, [[2, start + 11_000]]), inside(70, 2, [[1, start + 11_000]])], 15_000)
    expect(deep(a)).toBe(deep(b))
  })

  it('differs when a member heard of another one longer ago, waits longer to start over, or a handshake is on its way', () => {
    const base = deep(whole([inside(7, 2, [[2, 0]]), inside(7, 2)], 4000))
    expect(deep(whole([inside(7, 2, [[2, -4000]]), inside(7, 2)], 4000))).not.toBe(base)
    expect(deep(whole([inside(7, 2, [[2, 0]], 30_000), inside(7, 2)], 4000))).not.toBe(base)
    expect(deep(whole([inside(7, 2, [[2, 0]]), inside(7, 2)], 4000, [{ to: id(1), kind: 'commit@7[]', due: 300 }]))).not.toBe(base)
  })

  it('differs while a tab waits on a timer of its own, even with no group yet (seed 171 of the cycle sweep, hostile)', () => {
    const waiting = (due: number) => deep(whole([inside(null, 0), inside(null, 0)], 4000, [], [{ owner: 'm1#1', due }]))
    expect(waiting(2000)).not.toBe(waiting(980))
    expect(waiting(2000)).toBe(waiting(2020))
  })

  it('says when the room is in a state again, and not while it stays in one', () => {
    const recurrence = new Recurrence()
    const a = inside(7, 2)
    const b = inside(8, 2)
    for (const [members, at] of [[[a, a], 0], [[a, a], 1000], [[b, a], 2000], [[a, a], 3000]] as const) recurrence.observe(whole([...members], at))
    expect(recurrence.states()).toBe(2)
    expect(recurrence.repeats).toBe(1)
    expect(recurrence.first).toEqual({ firstAtS: 0, againAtS: 3, repeats: 1 })
  })
})
