import { describe, it, expect, vi } from 'vitest'
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

describe('server clock synchronization', () => {
  it('aligns to server time, advances on the shared interval, and ignores invalid updates', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-10-07T12:00:00Z'))
    vi.resetModules()
    const clock = await import('./clock')
    clock.syncServerClock('2026-10-07T12:00:10Z')
    expect(clock.serverOffset.value).toBe(10000)
    expect(clock.now.value).toBe(Date.parse('2026-10-07T12:00:10Z'))
    clock.syncServerClock('invalid')
    expect(clock.serverOffset.value).toBe(10000)
    vi.advanceTimersByTime(1000)
    expect(clock.now.value).toBe(Date.parse('2026-10-07T12:00:11Z'))
    expect(clock.secondsSince('2026-10-07T12:00:10Z')).toBe(1)
    expect(clock.totalParts(undefined)).toEqual({ key: 'activity.underMinute', params: {} })
    vi.clearAllTimers()
    vi.useRealTimers()
  })
  it('loads without installing a browser interval in a server-side environment', async () => {
    vi.resetModules()
    vi.stubGlobal('window', undefined)
    const interval = vi.spyOn(globalThis, 'setInterval')
    try {
      const clock = await import('./clock')
      expect(clock.now.value).toBeGreaterThan(0)
      expect(interval).not.toHaveBeenCalled()
    } finally { vi.unstubAllGlobals(); interval.mockRestore() }
  })
})
