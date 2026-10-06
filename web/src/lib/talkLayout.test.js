import { describe, it, expect } from 'vitest'
import { loadStripCollapsed, saveStripCollapsed, fitAspect, STRIP_COLLAPSED_KEY } from './talkLayout'

function memoryStorage() {
  const data = new Map()
  return {
    getItem: k => (data.has(k) ? data.get(k) : null),
    setItem: (k, v) => data.set(k, String(v)),
    removeItem: k => data.delete(k),
    data
  }
}

const blocked = {
  getItem() { throw new Error('blocked') },
  setItem() { throw new Error('blocked') },
  removeItem() { throw new Error('blocked') }
}

describe('strip collapsed preference', () => {
  it('is expanded by default and remembers a collapse', () => {
    const s = memoryStorage()
    expect(loadStripCollapsed(s)).toBe(false)
    expect(saveStripCollapsed(true, s)).toBe(true)
    expect(s.data.get(STRIP_COLLAPSED_KEY)).toBe('true')
    expect(loadStripCollapsed(s)).toBe(true)
    saveStripCollapsed(false, s)
    expect(s.data.has(STRIP_COLLAPSED_KEY)).toBe(false)
    expect(loadStripCollapsed(s)).toBe(false)
  })

  it('works without storage or with storage blocked', () => {
    expect(loadStripCollapsed(null)).toBe(false)
    expect(saveStripCollapsed(true, null)).toBe(false)
    expect(loadStripCollapsed(blocked)).toBe(false)
    expect(saveStripCollapsed(true, blocked)).toBe(false)
  })

  it('defaults to the browser storage', () => {
    localStorage.clear()
    saveStripCollapsed(true)
    expect(localStorage.getItem(STRIP_COLLAPSED_KEY)).toBe('true')
    expect(loadStripCollapsed()).toBe(true)
    localStorage.clear()
  })
})

describe('fitAspect', () => {
  it('fills the width when the area is tall, the height when it is wide', () => {
    expect(fitAspect(1600, 1200, 16 / 9)).toEqual({ width: 1600, height: 900 })
    expect(fitAspect(2000, 900, 16 / 9)).toEqual({ width: 1600, height: 900 })
  })

  it('follows the video shape and falls back to 16:9', () => {
    expect(fitAspect(1000, 1000, 4 / 3)).toEqual({ width: 1000, height: 750 })
    expect(fitAspect(1600, 2000, 0)).toEqual({ width: 1600, height: 900 })
    expect(fitAspect(1600, 2000, NaN)).toEqual({ width: 1600, height: 900 })
  })

  it('is empty without an area', () => {
    expect(fitAspect(0, 500)).toEqual({ width: 0, height: 0 })
    expect(fitAspect(500, -10)).toEqual({ width: 0, height: 0 })
  })
})
