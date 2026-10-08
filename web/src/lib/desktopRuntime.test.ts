import { afterEach, expect, it, vi } from 'vitest'
import { isDesktopRuntime } from './desktopRuntime'

afterEach(() => vi.unstubAllGlobals())

it('keeps ordinary browsers on browser transport even if a Tauri-shaped object exists', () => {
  vi.stubGlobal('__TAURI_INTERNALS__', {})
  expect(isDesktopRuntime()).toBe(false)
})

it('recognizes the flag installed by the actual Tauri host', () => {
  vi.stubGlobal('isTauri', true)
  expect(isDesktopRuntime()).toBe(true)
})

it('remains safe in a process without browser globals', () => {
  vi.stubGlobal('window', undefined)
  vi.stubGlobal('isTauri', true)
  expect(isDesktopRuntime()).toBe(false)
})
