import { describe, it, expect } from 'vitest'
import { formatClock, secondsSince, totalParts } from './clock'

describe('clock', () => {
  it('formats running timers', () => {
    expect(formatClock(0)).toBe('0:00')
    expect(formatClock(65)).toBe('1:05')
    expect(formatClock(3723)).toBe('1:02:03')
  })
  it('counts seconds since a timestamp, never negative', () => {
    const at = Date.parse('2026-10-05T10:00:10Z')
    expect(secondsSince('2026-10-05T10:00:00Z', at)).toBe(10)
    expect(secondsSince('2026-10-05T10:00:20Z', at)).toBe(0)
    expect(secondsSince(null, at)).toBe(0)
  })
  it('splits totals into hours and minutes', () => {
    expect(totalParts(30)).toEqual({ key: 'activity.underMinute', params: {} })
    expect(totalParts(45 * 60)).toEqual({ key: 'activity.minutes', params: { m: 45 } })
    expect(totalParts(12 * 3600 + 4 * 60)).toEqual({ key: 'activity.hoursMinutes', params: { h: 12, m: 4 } })
  })
})
