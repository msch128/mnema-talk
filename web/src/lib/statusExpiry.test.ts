import { describe, expect, it } from 'vitest'
import { expiryTime, isFuture, toLocalInput } from './statusExpiry'

const now = new Date(2026, 9, 24, 14, 30, 15)

describe('status expiry choices', () => {
  it('computes each preset from the local clock', () => {
    expect(expiryTime('never', now)).toBeNull()
    expect(expiryTime('hour', now)?.getTime()).toBe(now.getTime() + 3_600_000)
    expect(expiryTime('tonight', now)).toEqual(new Date(2026, 9, 24, 23, 59))
    expect(expiryTime('tomorrow', now)).toEqual(new Date(2026, 9, 25, 8, 0))
  })

  it('rolls tomorrow morning over the end of a month', () => {
    expect(expiryTime('tomorrow', new Date(2026, 9, 31, 22, 0))).toEqual(new Date(2026, 10, 1, 8, 0))
  })

  it('reads a custom datetime-local value as local time', () => {
    expect(expiryTime('custom', now, '2026-10-26T09:05')).toEqual(new Date(2026, 9, 26, 9, 5))
  })

  it('rejects empty, invalid and past moments', () => {
    expect(isFuture(expiryTime('custom', now, ''), now)).toBe(false)
    expect(isFuture(expiryTime('custom', now, '2026-10-24T14:00'), now)).toBe(false)
    expect(isFuture(expiryTime('tonight', new Date(2026, 9, 24, 23, 59, 30)), new Date(2026, 9, 24, 23, 59, 30))).toBe(false)
    expect(isFuture(expiryTime('hour', now), now)).toBe(true)
    expect(isFuture(null, now)).toBe(false)
  })

  it('formats a date for a datetime-local input', () => {
    expect(toLocalInput(new Date(2026, 0, 5, 7, 3))).toBe('2026-01-05T07:03')
  })
})
