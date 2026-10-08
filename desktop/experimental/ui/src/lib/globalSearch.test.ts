import { afterEach, describe, expect, it, vi } from 'vitest'

vi.mock('../components/GlobalSearch.vue', () => ({ default: { template: '<div data-search-host>Search</div>' } }))
import { searchOpen, installGlobalSearch, uninstallGlobalSearch } from './globalSearch'

afterEach(() => {
  uninstallGlobalSearch()
  vi.unstubAllGlobals()
})

describe('global search keyboard lifecycle', () => {
  it('installs once, mounts one modal, toggles Ctrl/Meta+K and removes the listener at logout', async () => {
    uninstallGlobalSearch()
    const count = () => document.querySelectorAll('[data-search-host]').length
    const initial = count()
    const add = vi.spyOn(window, 'addEventListener')
    installGlobalSearch()
    installGlobalSearch()
    expect(add.mock.calls.filter(([type]) => type === 'keydown')).toHaveLength(1)
    for (const options of [
      { key: 'k' }, { key: 'j', ctrlKey: true }, { key: 'k', ctrlKey: true, altKey: true }, { key: 'k', ctrlKey: true, shiftKey: true },
    ]) {
      const ignored = new KeyboardEvent('keydown', { ...options, cancelable: true })
      window.dispatchEvent(ignored)
      expect(ignored.defaultPrevented).toBe(false)
      expect(searchOpen.value).toBe(false)
    }
    const open = new KeyboardEvent('keydown', { key: 'K', ctrlKey: true, cancelable: true })
    window.dispatchEvent(open)
    expect(open.defaultPrevented).toBe(true)
    await vi.dynamicImportSettled()
    expect(searchOpen.value).toBe(true)
    expect(count()).toBe(initial + 1)
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'k', metaKey: true }))
    await vi.dynamicImportSettled()
    expect(searchOpen.value).toBe(false)
    expect(count()).toBe(initial + 1)
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'k', ctrlKey: true }))
    await vi.dynamicImportSettled()
    expect(searchOpen.value).toBe(true)
    uninstallGlobalSearch()
    expect(searchOpen.value).toBe(false)
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'k', ctrlKey: true }))
    await vi.dynamicImportSettled()
    expect(searchOpen.value).toBe(false)
    add.mockRestore()
  })
  it('does not install a browser listener in a server-side environment', () => {
    uninstallGlobalSearch()
    vi.stubGlobal('window', undefined)
    expect(() => installGlobalSearch()).not.toThrow()
    expect(searchOpen.value).toBe(false)
  })
})
