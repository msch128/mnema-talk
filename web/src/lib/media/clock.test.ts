import { afterEach, describe, expect, it, vi } from 'vitest'
import { monotonicNow } from './clock'

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

describe('media clock', () => {
  it('follows the monotonic clock, not the wall clock', () => {
    vi.spyOn(performance, 'now').mockReturnValue(1000)
    const before = monotonicNow()
    vi.spyOn(Date, 'now').mockReturnValue(0)
    expect(monotonicNow()).toBe(before)
    vi.spyOn(performance, 'now').mockReturnValue(2500)
    expect(monotonicNow() - before).toBe(1500)
  })

  it('falls back to the wall clock where there is no performance clock', () => {
    vi.stubGlobal('performance', undefined)
    vi.spyOn(Date, 'now').mockReturnValue(42)
    expect(monotonicNow()).toBe(42)
  })
})
