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
    const release = clock.retainClock()
    vi.advanceTimersByTime(1000)
    expect(clock.now.value).toBe(Date.parse('2026-10-07T12:00:11Z'))
    expect(clock.secondsSince('2026-10-07T12:00:10Z')).toBe(1)
    expect(clock.totalParts(undefined)).toEqual({ key: 'activity.underMinute', params: {} })
    release()
    expect(vi.getTimerCount()).toBe(0)
    vi.useRealTimers()
  })

  it('ticks only while something shows it, and not in a hidden page', async () => {
    vi.useFakeTimers()
    vi.resetModules()
    const clock = await import('./clock')
    expect(vi.getTimerCount()).toBe(0)
    const first = clock.retainClock()
    const second = clock.retainClock()
    expect(vi.getTimerCount()).toBe(1)
    first()
    first()
    expect(vi.getTimerCount()).toBe(1)
    Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => 'hidden' })
    document.dispatchEvent(new Event('visibilitychange'))
    expect(vi.getTimerCount()).toBe(0)
    Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => 'visible' })
    document.dispatchEvent(new Event('visibilitychange'))
    expect(vi.getTimerCount()).toBe(1)
    second()
    expect(vi.getTimerCount()).toBe(0)
    vi.useRealTimers()
  })
  it('runs for a component only while it is mounted and needs the time', async () => {
    vi.useFakeTimers()
    vi.resetModules()
    const clock = await import('./clock')
    const { defineComponent, h, nextTick, ref } = await import('vue')
    const { mount } = await import('@vue/test-utils')
    const needed = ref(false)
    const Probe = defineComponent({ setup() { clock.useServerClock(() => needed.value); return () => h('span') } })
    const w = mount(Probe)
    const idle = vi.getTimerCount()
    needed.value = true
    await nextTick()
    expect(vi.getTimerCount()).toBe(idle + 1)
    needed.value = false
    await nextTick()
    expect(vi.getTimerCount()).toBe(idle)
    needed.value = true
    await nextTick()
    w.unmount()
    expect(vi.getTimerCount()).toBe(idle)
    clock.useServerClock()
    expect(vi.getTimerCount()).toBe(idle + 1)
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
