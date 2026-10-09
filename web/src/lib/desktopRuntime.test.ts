import { afterEach, expect, it, vi } from 'vitest'
import { isDesktopRuntime } from './desktopRuntime'

afterEach(() => vi.unstubAllGlobals())

it('keeps ordinary browsers on browser transport even if a Tauri-shaped object exists', () => {
  vi.stubGlobal('__TAURI_INTERNALS__', {})
  expect(isDesktopRuntime()).toBe(false)
})

it('recognizes the flag installed by the actual Tauri host', () => {
  vi.stubGlobal('isTauri', true); vi.stubGlobal('location', new URL('http://tauri.localhost/'))
  expect(isDesktopRuntime()).toBe(true)
})

it('remains safe in a process without browser globals', () => {
  vi.stubGlobal('window', undefined)
  vi.stubGlobal('isTauri', true); vi.stubGlobal('location', new URL('http://tauri.localhost/'))
  expect(isDesktopRuntime()).toBe(false)
})

it('keeps a selected HTTPS instance on the full browser client despite Tauri injection', () => {
  vi.stubGlobal('isTauri', true)
  vi.stubGlobal('location', new URL('https://community.example/'))
  expect(isDesktopRuntime()).toBe(false)
})

it.each(['http://tauri.localhost:9000/', 'https://tauri.localhost/', 'http://localhost/'])('rejects nonbundled origins: %s', origin => {
  vi.stubGlobal('isTauri', true)
  vi.stubGlobal('location', new URL(origin))
  expect(isDesktopRuntime()).toBe(false)
})
