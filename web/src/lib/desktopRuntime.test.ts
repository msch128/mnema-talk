import { afterEach, expect, it, vi } from 'vitest'
import { isDesktopRuntime, isWebDesktopSelector } from './desktopRuntime'

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

it('treats the verified bundle in its selector role as the instance selector, and no other role', () => {
  vi.stubGlobal('isTauri', true)
  vi.stubGlobal('location', new URL('mnema://localhost/'))
  const identity = { version: '0.8.0', source: 'builtin', webClientApi: 1, keyId: '1069c76ff2e11272' }
  vi.stubGlobal('__MNEMA_TRUSTED_APP__', { ...identity, role: 'selector' })
  expect(isWebDesktopSelector()).toBe(true)
  expect(isDesktopRuntime()).toBe(false)
  vi.stubGlobal('__MNEMA_TRUSTED_APP__', { ...identity, role: 'instance' })
  expect(isWebDesktopSelector()).toBe(false)
})
