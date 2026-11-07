import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { chordText } from './desktopGaming'
import { formatShortcut, isMac, keyLabel, SHORTCUT_GROUPS } from './shortcuts'
import { setLocale } from '../i18n'

beforeEach(() => setLocale('en'))
afterEach(() => {
  vi.unstubAllGlobals()
  setLocale('en')
})

describe('keyLabel', () => {
  it('writes the modifier the way the machine calls it and leaves other keys alone', () => {
    expect(keyLabel('mod', true)).toBe('⌘')
    expect(keyLabel('mod', false)).toBe('Ctrl')
    expect(keyLabel('Alt', true)).toBe('⌥')
    expect(keyLabel('K', true)).toBe('K')
    expect(keyLabel('Shift', false)).toBe('Shift')
  })
})

describe('formatShortcut', () => {
  it('writes ⌘K and ⌥↑ on a Mac, in any language', () => {
    for (const locale of ['en', 'de'] as const) {
      setLocale(locale)
      expect(formatShortcut(['mod', 'K'], true)).toBe('⌘K')
      expect(formatShortcut(['Alt', '↑'], true)).toBe('⌥↑')
      expect(formatShortcut(['Shift', 'Enter'], true)).toBe('⇧Enter')
    }
  })

  it('writes Ctrl+K in English and Strg+K in German elsewhere', () => {
    setLocale('en')
    expect(formatShortcut(['mod', 'K'], false)).toBe('Ctrl+K')
    expect(formatShortcut(['Shift', 'F10'], false)).toBe('Shift+F10')
    setLocale('de')
    expect(formatShortcut(['mod', 'K'], false)).toBe('Strg+K')
    expect(formatShortcut(['Shift', 'F10'], false)).toBe('Umschalt+F10')
    expect(formatShortcut(['?'], false)).toBe('?')
  })

  it('writes the space bar in the reader\'s language, as the gaming shortcut chord does', () => {
    setLocale('en')
    expect(formatShortcut(['Ctrl', 'Space'], false)).toBe('Ctrl+Space')
    setLocale('de')
    expect(formatShortcut(['Ctrl', 'Space'], false)).toBe('Strg+Leertaste')
    expect(chordText({ key: 32, control: true, alt: false, shift: false })).toBe('Strg+Leertaste')
  })

  it('follows the device when no platform is given', () => {
    vi.stubGlobal('navigator', { platform: 'MacIntel', userAgent: '' })
    expect(formatShortcut(['mod', 'K'])).toBe('⌘K')
    vi.stubGlobal('navigator', { platform: 'Win32', userAgent: '' })
    expect(formatShortcut(['mod', 'K'])).toBe('Ctrl+K')
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
