import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const host = vi.hoisted(() => ({
  desktop: false,
  selector: false,
  trusted: null as null | { role: string; version: string },
  invoke: vi.fn(),
  nativeTransport: vi.fn(),
  initialize: vi.fn(),
  connect: vi.fn(),
  disconnect: vi.fn(),
  capture: vi.fn(),
}))

vi.mock('@tauri-apps/api/core', async importOriginal => ({ ...await importOriginal<typeof import('@tauri-apps/api/core')>(), invoke: host.invoke }))
vi.mock('./desktopRuntime', () => ({ isDesktopRuntime: () => host.desktop, isWebDesktopSelector: () => host.selector }))
vi.mock('./trustedDesktop', () => ({ trustedApp: () => host.trusted, isTrustedDesktop: () => host.trusted !== null }))
vi.mock('./trustedSocket', () => ({ TrustedSocket: class TrustedSocket { kind = 'trusted' } }))
vi.mock('./nativeTransport', () => ({
  NativeSocket: class NativeSocket { kind = 'native' },
  nativeApiTransport: host.nativeTransport,
  initializeNativeContext: host.initialize,
  connectNative: host.connect,
  disconnectNative: host.disconnect,
  captureNativeTrustPresentation: host.capture,
}))

import { browserPlatform, desktopPlatform, platform } from './platform'

beforeEach(() => {
  host.desktop = false
  host.selector = false
  host.trusted = null
  vi.clearAllMocks()
})
afterEach(() => {
  vi.unstubAllGlobals()
  Reflect.deleteProperty(window, '__MNEMA_GAMING_SURFACE__')
  Reflect.deleteProperty(window, '__MNEMA_GAMING_BRIDGE__')
})

describe('browser platform', () => {
  it('is not a desktop, uses same-origin API calls and a plain WebSocket', () => {
    const socket = vi.fn()
    vi.stubGlobal('WebSocket', socket)
    expect(platform.isDesktop()).toBe(false)
    expect(platform.apiTransport()).toBeNull()
    platform.openSocket('wss://example.invalid/api/ws')
    expect(socket).toHaveBeenCalledWith('wss://example.invalid/api/ws')
  })

  it('opens the host relayed socket inside the verified desktop bundle', () => {
    host.trusted = { role: 'instance', version: '1.2.3' }
    expect(platform.isTrustedDesktop()).toBe(true)
    expect(platform.trustedApp()).toEqual(host.trusted)
    expect(platform.openSocket('wss://example.invalid/api/ws')).toMatchObject({ kind: 'trusted' })
  })

  it('reports the instance selector page', () => {
    host.selector = true
    expect(platform.isWebDesktopSelector()).toBe(true)
  })

  it('reads the gaming surface and bridge token only when they are strings', () => {
    expect(platform.gamingSurface()).toBeNull()
    expect(platform.gamingBridgeToken()).toBeNull()
    Reflect.set(window, '__MNEMA_GAMING_SURFACE__', 'sidepeek')
    Reflect.set(window, '__MNEMA_GAMING_BRIDGE__', 'token')
    expect(platform.gamingSurface()).toBe('sidepeek')
    expect(platform.gamingBridgeToken()).toBe('token')
    Reflect.set(window, '__MNEMA_GAMING_BRIDGE__', 7)
    expect(platform.gamingBridgeToken()).toBeNull()
  })

  it('reads no window global without a window', () => {
    vi.stubGlobal('window', undefined)
    expect(browserPlatform.gamingSurface()).toBeNull()
  })
})

describe('desktop platform', () => {
  beforeEach(() => { host.desktop = true })

  it('routes API calls and the socket through native IPC', () => {
    expect(platform.isDesktop()).toBe(true)
    expect(platform.apiTransport()).toBe(host.nativeTransport)
    expect(platform.openSocket('wss://ignored.invalid')).toMatchObject({ kind: 'native' })
    expect(desktopPlatform.isDesktop()).toBe(true)
  })

  it('forwards host commands and the native connection lifecycle', async () => {
    host.invoke.mockResolvedValue('reply')
    host.connect.mockResolvedValue({ context: 'c', status: 200, body: null })
    expect(await platform.invoke('desktop_open_instance', { address: 'a' })).toBe('reply')
    expect(host.invoke).toHaveBeenCalledWith('desktop_open_instance', { address: 'a' })
    await platform.initializeNative()
    await platform.connectNative('community.example.invalid')
    await platform.disconnectNative()
    platform.captureTrustPresentation()
    expect(host.initialize).toHaveBeenCalledOnce()
    expect(host.connect).toHaveBeenCalledWith('community.example.invalid')
    expect(host.disconnect).toHaveBeenCalledOnce()
    expect(host.capture).toHaveBeenCalledOnce()
  })
})
