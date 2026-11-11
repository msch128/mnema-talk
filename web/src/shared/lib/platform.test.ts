import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const host = vi.hoisted(() => ({
  selector: false,
  trusted: null as null | { role: string; version: string; capabilities: string[] },
  invoke: vi.fn(),
}))

vi.mock('@tauri-apps/api/core', async importOriginal => ({ ...await importOriginal<typeof import('@tauri-apps/api/core')>(), invoke: host.invoke }))
vi.mock('./desktopRuntime', () => ({ isWebDesktopSelector: () => host.selector }))
vi.mock('./native/trustedDesktop', () => ({ trustedApp: () => host.trusted, isTrustedDesktop: () => host.trusted !== null }))
const notify = vi.hoisted(() => ({
  browser: { permission: vi.fn(() => 'default'), refreshPermission: vi.fn(async () => 'default'), requestPermission: vi.fn(async () => 'granted'), notify: vi.fn(), setBadge: vi.fn() },
  host: { permission: vi.fn(() => 'granted'), refreshPermission: vi.fn(async () => 'default'), requestPermission: vi.fn(async () => 'granted'), notify: vi.fn(), setBadge: vi.fn(), reportUnread: vi.fn() },
}))
vi.mock('./osNotify', () => ({ browserNotify: notify.browser, hostNotify: notify.host }))
vi.mock('./native/trustedSocket', () => ({ TrustedSocket: class TrustedSocket { kind = 'trusted'; constructor(public path?: string) {} } }))
import { isTrustedDesktop } from './native/trustedDesktop'
import { browserPlatform, platform } from './platform'

beforeEach(() => {
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
  it('opens a plain WebSocket', () => {
    const socket = vi.fn()
    vi.stubGlobal('WebSocket', socket)
    platform.openSocket('wss://example.invalid/api/ws')
    expect(socket).toHaveBeenCalledWith('wss://example.invalid/api/ws')
  })

  it('keeps the relayed socket off the browser platform when the page is not trusted', () => {
    const socket = vi.fn()
    vi.stubGlobal('WebSocket', socket)
    expect(isTrustedDesktop()).toBe(false)
    expect(browserPlatform.openSocket('wss://example.invalid/api/ws')).not.toMatchObject({ kind: 'trusted' })
    expect(socket).toHaveBeenCalledWith('wss://example.invalid/api/ws')
  })

  it('opens the host relayed socket inside the verified desktop bundle', () => {
    host.trusted = { role: 'instance', version: '1.2.3', capabilities: [] }
    expect(platform.isTrustedDesktop()).toBe(true)
    expect(platform.trustedApp()).toEqual(host.trusted)
    expect(platform.openSocket('wss://example.invalid/api/ws')).toMatchObject({ kind: 'trusted' })
  })

  it('asks the host for conversation events only when it relays the query', () => {
    const open = (url: string) => (platform.openSocket(url) as unknown as { path: string }).path
    host.trusted = { role: 'instance', version: '1.2.3', capabilities: ['notify', 'ws_query'] }
    expect(open('wss://example.invalid/api/ws?dm=1')).toBe('/api/ws?dm=1')
    expect(open('wss://example.invalid/api/ws')).toBe('/api/ws')
    expect(open('wss://example.invalid/api/ws?x=1')).toBe('/api/ws')
    expect(open('wss://example.invalid/other?dm=1')).toBe('/api/ws')
    expect(open('not a url')).toBe('/api/ws')
    host.trusted = { role: 'instance', version: '1.2.3', capabilities: ['notify'] }
    expect(open('wss://example.invalid/api/ws?dm=1')).toBe('/api/ws')
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

describe('notifications', () => {
  const toast = { title: 't', body: 'b', channelId: 'c', messageId: 'm', onClick: () => {} }

  it('uses the browser adapter outside the client and in clients without the capability', async () => {
    for (const trusted of [null, { role: 'instance', version: '1.2.3', capabilities: [] }, { role: 'selector', version: '1.2.3', capabilities: ['notify'] }]) {
      host.trusted = trusted
      vi.clearAllMocks()
      expect(platform.notificationPermission()).toBe('default')
      await platform.requestNotificationPermission()
      expect(await platform.refreshNotificationPermission()).toBe('default')
      platform.notify(toast)
      platform.setBadge(2)
      expect(notify.browser.notify).toHaveBeenCalledWith(toast)
      expect(notify.browser.setBadge).toHaveBeenCalledWith(2)
      expect(notify.host.notify).not.toHaveBeenCalled()
    }
  })

  it('hands toasts and the badge to the client when its instance window announces notify', async () => {
    host.trusted = { role: 'instance', version: '1.2.3', capabilities: ['notify'] }
    expect(platform.notificationPermission()).toBe('granted')
    expect(await platform.requestNotificationPermission()).toBe('granted')
    expect(await platform.refreshNotificationPermission()).toBe('default')
    platform.notify(toast)
    platform.setBadge(3)
    expect(notify.host.notify).toHaveBeenCalledWith(toast)
    expect(notify.host.setBadge).toHaveBeenCalledWith(3)
    expect(notify.browser.notify).not.toHaveBeenCalled()
  })

  describe('unread report (multi-instance client)', () => {
    const counts = { unread: 3, mentions: 1 }

    it('goes to the host only when its instance window announces unread', () => {
      host.trusted = { role: 'instance', version: '1.2.3', capabilities: ['notify', 'unread'] }
      expect(platform.supportsUnreadReport()).toBe(true)
      platform.reportUnread(counts)
      expect(notify.host.reportUnread).toHaveBeenCalledWith(counts)
      expect(notify.browser.setBadge).not.toHaveBeenCalled()
    })

    it('is a no-op in a browser, in older clients and outside the instance window', () => {
      for (const trusted of [null, { role: 'instance', version: '1.2.3', capabilities: ['notify'] }, { role: 'selector', version: '1.2.3', capabilities: ['unread'] }, { role: 'surface', version: '1.2.3', capabilities: ['unread'] }]) {
        host.trusted = trusted
        expect(platform.supportsUnreadReport()).toBe(false)
        expect(() => platform.reportUnread(counts)).not.toThrow()
      }
      expect(notify.host.reportUnread).not.toHaveBeenCalled()
    })
  })
})

describe('host commands', () => {
  it('forwards host commands', async () => {
    host.invoke.mockResolvedValue('reply')
    expect(await platform.invoke('desktop_open_instance', { address: 'a' })).toBe('reply')
    expect(host.invoke).toHaveBeenCalledWith('desktop_open_instance', { address: 'a' })
  })
})
