import { describe, expect, it } from 'vitest'
import { CycleDetector, signature } from './groupCycle.fixture'
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
