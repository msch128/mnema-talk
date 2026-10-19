import { afterEach, expect, it, vi } from 'vitest'
import { isTrustedDesktop, trustedApp } from './trustedDesktop'

const valid = { version: '0.8.0', source: 'builtin', webClientApi: 1, keyId: '1069c76ff2e11272', role: 'instance' }
function host(origin: string, ...identity: [unknown?]) {
  vi.stubGlobal('isTauri', true)
  vi.stubGlobal('location', new URL(origin))
  vi.stubGlobal('__MNEMA_TRUSTED_APP__', identity.length ? identity[0] : valid)
}
afterEach(() => vi.unstubAllGlobals())

it('is absent in an ordinary browser, even if a page defines the identity itself', () => {
  vi.stubGlobal('__MNEMA_TRUSTED_APP__', valid)
  expect(trustedApp()).toBeNull()
  expect(isTrustedDesktop()).toBe(false)
})

it.each(['mnema://localhost/', 'https://mnema.localhost/c/1'])('reads the verified bundle announced on %s', origin => {
  host(origin)
  expect(trustedApp()).toEqual({ version: '0.8.0', source: 'builtin', webClientApi: 1, keyId: '1069c76ff2e11272', role: 'instance' })
  expect(isTrustedDesktop()).toBe(true)
  expect(Object.isFrozen(trustedApp())).toBe(true)
})

it.each(['https://community.example/', 'mnema://localhost:9000/', 'https://mnema.localhost:9000/', 'http://mnema.localhost/', 'mnema://evil/', 'tauri://localhost/'])('ignores the identity on %s', origin => {
  host(origin)
  expect(trustedApp()).toBeNull()
})

it('rejects a page without any identity', () => {
  host('mnema://localhost/', undefined)
  expect(trustedApp()).toBeNull()
})

it('requires the Tauri host', () => {
  host('mnema://localhost/')
  vi.stubGlobal('isTauri', false)
  expect(trustedApp()).toBeNull()
})

it.each([
  ['not an object', 'verified'],
  ['null', null],
  ['version', { ...valid, version: '0.8' }],
  ['version type', { ...valid, version: 8 }],
  ['source', { ...valid, source: 'server' }],
  ['api type', { ...valid, webClientApi: '1' }],
  ['api range', { ...valid, webClientApi: 0 }],
  ['api fraction', { ...valid, webClientApi: 1.5 }],
  ['key id', { ...valid, keyId: 'ABC' }],
  ['key id type', { ...valid, keyId: 1 }],
  ['role', { ...valid, role: 'admin' }],
  ['role type', { ...valid, role: 1 }],
  ['source type', { ...valid, source: 1 }],
])('rejects a malformed identity: %s', (_name, identity) => {
  host('mnema://localhost/', identity)
  expect(trustedApp()).toBeNull()
})

it('is safe without browser globals', () => {
  vi.stubGlobal('window', undefined)
  expect(trustedApp()).toBeNull()
})
