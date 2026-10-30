import { afterEach, describe, expect, it, vi } from 'vitest'
import { isMac, keyLabel, SHORTCUT_GROUPS } from './shortcuts'

afterEach(() => vi.unstubAllGlobals())

describe('keyLabel', () => {
  it('writes the modifier the way the machine calls it and leaves other keys alone', () => {
    expect(keyLabel('mod', true)).toBe('⌘')
    expect(keyLabel('mod', false)).toBe('Ctrl')
    expect(keyLabel('K', true)).toBe('K')
    expect(keyLabel('Shift', false)).toBe('Shift')
  })
})

describe('isMac', () => {
  it('recognises Apple platforms by platform, or by user agent when there is none', () => {
    vi.stubGlobal('navigator', { platform: 'MacIntel', userAgent: '' })
    expect(isMac()).toBe(true)
    vi.stubGlobal('navigator', { platform: 'Win32', userAgent: 'Windows' })
    expect(isMac()).toBe(false)
    vi.stubGlobal('navigator', { platform: '', userAgent: 'Mozilla (iPhone; CPU iPhone OS)' })
    expect(isMac()).toBe(true)
    vi.stubGlobal('navigator', { platform: '', userAgent: 'X11; Linux' })
    expect(isMac()).toBe(false)
  })

  it('is false without a navigator', () => {
    vi.stubGlobal('navigator', undefined)
    expect(isMac()).toBe(false)
  })
})

describe('the shortcut list', () => {
  it('has no empty group and no duplicate row in a group', () => {
    for (const group of SHORTCUT_GROUPS) {
      expect(group.items.length).toBeGreaterThan(0)
      expect(new Set(group.items.map(i => i.id)).size).toBe(group.items.length)
    }
  })
})
