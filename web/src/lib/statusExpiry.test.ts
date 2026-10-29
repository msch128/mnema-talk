import { describe, expect, it } from 'vitest'
import { expiryTime, isFuture } from './statusExpiry'

const now = new Date(2026, 9, 24, 14, 30, 15) // Saturday

describe('status expiry choices', () => {
  it('computes each preset from the local clock', () => {
    expect(expiryTime('never', now)).toBeNull()
    expect(expiryTime('min30', now)?.getTime()).toBe(now.getTime() + 30 * 60_000)
    expect(expiryTime('hour', now)?.getTime()).toBe(now.getTime() + 3_600_000)
    expect(expiryTime('hours4', now)?.getTime()).toBe(now.getTime() + 4 * 3_600_000)
    expect(expiryTime('today', now)).toEqual(new Date(2026, 9, 24, 23, 59))
  })

  it('ends the week on Sunday evening, and on the next one when it is Sunday already', () => {
    expect(expiryTime('week', now)).toEqual(new Date(2026, 9, 25, 23, 59))
    expect(expiryTime('week', new Date(2026, 9, 21, 9, 0))).toEqual(new Date(2026, 9, 25, 23, 59))
    expect(expiryTime('week', new Date(2026, 9, 25, 9, 0))).toEqual(new Date(2026, 9, 25, 23, 59))
    expect(expiryTime('week', new Date(2026, 9, 25, 23, 59, 30))).toEqual(new Date(2026, 10, 1, 23, 59))
  })

  it('rolls over the end of a month', () => {
    expect(expiryTime('week', new Date(2026, 9, 28, 12, 0))).toEqual(new Date(2026, 10, 1, 23, 59))
  })

  it('knows a moment that is still ahead', () => {
    expect(isFuture(expiryTime('today', new Date(2026, 9, 24, 23, 59, 30)), new Date(2026, 9, 24, 23, 59, 30))).toBe(false)
    expect(isFuture(expiryTime('hour', now), now)).toBe(true)
    expect(isFuture(new Date('nope'), now)).toBe(false)
    expect(isFuture(null, now)).toBe(false)
  })
})
