import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { onVideoSeenChange, onVisibilityChange, pageHidden, visibleInterval } from './pageVisibility'

function setVisibility(state: 'visible' | 'hidden') {
  Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => state })
  document.dispatchEvent(new Event('visibilitychange'))
}

describe('page visibility', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    setVisibility('visible')
  })
  afterEach(() => {
    vi.useRealTimers()
    setVisibility('visible')
  })

  it('follows the page and tells subscribers once per change', () => {
    const seen: boolean[] = []
    const stop = onVisibilityChange(hidden => seen.push(hidden))
    setVisibility('hidden')
    setVisibility('hidden')
    expect(pageHidden.value).toBe(true)
    setVisibility('visible')
    stop()
    setVisibility('hidden')
    expect(seen).toEqual([true, false])
  })

  it('treats the freeze and resume of the Page Lifecycle like hiding and showing', () => {
    document.dispatchEvent(new Event('freeze'))
    expect(pageHidden.value).toBe(true)
    document.dispatchEvent(new Event('resume'))
    expect(pageHidden.value).toBe(false)
  })

  it('does not tick while hidden and catches up once when shown', () => {
    const fn = vi.fn()
    const stop = visibleInterval(fn, 1000)
    vi.advanceTimersByTime(3000)
    expect(fn).toHaveBeenCalledTimes(3)
    setVisibility('hidden')
    vi.advanceTimersByTime(60_000)
    expect(fn).toHaveBeenCalledTimes(3)
    setVisibility('visible')
    expect(fn).toHaveBeenCalledTimes(4)
    vi.advanceTimersByTime(1000)
    expect(fn).toHaveBeenCalledTimes(5)
    stop()
    vi.advanceTimersByTime(5000)
    expect(fn).toHaveBeenCalledTimes(5)
  })

  it('can skip the catch-up and slow down instead of pausing', () => {
    const fn = vi.fn()
    const stop = visibleInterval(fn, 1000, { hiddenMs: 10_000, catchUp: false })
    setVisibility('hidden')
    vi.advanceTimersByTime(30_000)
    expect(fn).toHaveBeenCalledTimes(3)
    setVisibility('visible')
    expect(fn).toHaveBeenCalledTimes(3)
    vi.advanceTimersByTime(2000)
    expect(fn).toHaveBeenCalledTimes(5)
    stop()
  })

  it('starts paused when the page is already hidden and a stop while hidden stays stopped', () => {
    setVisibility('hidden')
    const fn = vi.fn()
    const stop = visibleInterval(fn, 1000)
    vi.advanceTimersByTime(5000)
    expect(fn).not.toHaveBeenCalled()
    stop()
    setVisibility('visible')
    vi.advanceTimersByTime(5000)
    expect(fn).not.toHaveBeenCalled()
  })

  it('watches video without a document', () => {
    vi.stubGlobal('document', undefined)
    try {
      const stop = onVideoSeenChange(() => {})
      expect(() => stop()).not.toThrow()
    } finally {
      vi.unstubAllGlobals()
    }
  })
})
