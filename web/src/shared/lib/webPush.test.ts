import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const fakeApi = vi.hoisted(() => ({
  calls: [] as Array<{ path: string, method: string, json: unknown, quiet: boolean }>,
  devices: [] as Array<Record<string, unknown>>,
  failPath: '',
}))

vi.mock('./api', () => ({
  api: vi.fn(async (path: string, options: { method?: string, json?: unknown, decode?: (v: unknown) => unknown, shouldNotifyUnauthorized?: () => boolean } = {}) => {
    const method = options.method ?? 'GET'
    fakeApi.calls.push({ path, method, json: options.json, quiet: options.shouldNotifyUnauthorized?.() === false })
    if (path === fakeApi.failPath) throw new Error('server said no')
    let body: unknown = null
    if (path === '/api/push/config') body = { enabled: true, public_key: 'AQID', preview_allowed: true, preview: true, max_devices: 10 }
    else if (path === '/api/push/subscriptions' && method === 'GET') body = { subscriptions: fakeApi.devices }
    else if (path === '/api/push/subscriptions') body = { id: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa', label: 'x', fingerprint: 'f', created_at: '2026-10-22T10:00:00Z', last_success_at: null }
    else if (path === '/api/push/test') body = { delivered: 1, failed: 0 }
    return options.decode ? options.decode(body) : body
  }),
}))

const pwa = vi.hoisted(() => ({ allowed: true, ios: false, standalone: false }))
vi.mock('./pwa', () => ({
  serviceWorkerAllowed: () => pwa.allowed,
  isIosDevice: () => pwa.ios,
  isStandalone: () => pwa.standalone,
}))

import {
  currentSubscription, deviceLabel, enablePush, endpointFingerprint, listenForOpenRequests, loadConfig, parseOpenEvent, parseOpenRequest,
  pushAvailability, pushOnThisDevice, refreshPushState, releaseThisDevice, removeDevice, sendTestPush, setPreview, thisDevice,
} from './webPush'

const ENDPOINT = 'https://push.example.com/abc'
const DEV1 = 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb'
const DEV2 = 'dddddddd-dddd-4ddd-8ddd-dddddddddddd'

interface FakeSub {
  endpoint: string
  options: { applicationServerKey: ArrayBuffer | null }
  unsubscribe: ReturnType<typeof vi.fn>
  toJSON(): { endpoint: string, keys: { p256dh: string, auth: string } }
}

function subscription(key: number[] = [1, 2, 3], endpoint = ENDPOINT): FakeSub {
  return {
    endpoint,
    options: { applicationServerKey: new Uint8Array(key).buffer },
    unsubscribe: vi.fn(async () => true),
    toJSON: () => ({ endpoint, keys: { p256dh: 'PUB', auth: 'SEC' } }),
  }
}

let current: FakeSub | null
let subscribe: ReturnType<typeof vi.fn>
let registered: boolean

function stubBrowser(permission: NotificationPermission = 'granted', ask: NotificationPermission = permission) {
  current = null
  registered = true
  subscribe = vi.fn(async () => { current = subscription(); return current })
  const pushManager = { getSubscription: vi.fn(async () => current), subscribe }
  vi.stubGlobal('navigator', {
    serviceWorker: {
      getRegistration: vi.fn(async () => registered ? {} : undefined),
      ready: Promise.resolve({ pushManager }),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    },
  })
  vi.stubGlobal('PushManager', class {})
  vi.stubGlobal('Notification', Object.assign(class {}, { permission, requestPermission: vi.fn(async () => ask) }))
  Object.assign(window, { PushManager: class {}, Notification: globalThis.Notification })
}

beforeEach(() => {
  fakeApi.calls = []
  fakeApi.devices = []
  fakeApi.failPath = ''
  Object.assign(pwa, { allowed: true, ios: false, standalone: false })
  pushOnThisDevice.value = false
  stubBrowser()
})

afterEach(() => {
  vi.unstubAllGlobals()
  Reflect.deleteProperty(window, 'PushManager')
})

describe('where push can work', () => {
  it('is ready in a browser with service workers, the Push API and notifications', () => {
    expect(pushAvailability()).toBe('ready')
  })

  it('is unsupported without the Push API or in the desktop client', () => {
    Reflect.deleteProperty(window, 'PushManager')
    expect(pushAvailability()).toBe('unsupported')
    stubBrowser()
    pwa.allowed = false
    expect(pushAvailability()).toBe('unsupported')
  })

  it('on iPhone and iPad needs the installed app first', () => {
    pwa.ios = true
    Reflect.deleteProperty(window, 'PushManager')
    expect(pushAvailability()).toBe('needs-install')
    pwa.standalone = true
    stubBrowser()
    expect(pushAvailability()).toBe('ready')
  })
})

describe('naming a device', () => {
  it('says which browser on which system', () => {
    expect(deviceLabel('Mozilla/5.0 (X11; Linux x86_64; rv:130.0) Gecko/20100101 Firefox/130.0')).toBe('Firefox on Linux')
    expect(deviceLabel('Mozilla/5.0 (Windows NT 10.0) AppleWebKit/537.36 Chrome/120 Safari/537.36 Edg/120')).toBe('Edge on Windows')
    expect(deviceLabel('Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605 Version/17.0 Safari/604')).toBe('Safari on iOS')
    expect(deviceLabel('Mozilla/5.0 (Linux; Android 14) Chrome/120 Mobile Safari/537')).toBe('Chrome on Android')
    expect(deviceLabel('curl/8')).toBe('')
    expect(deviceLabel('Something (Macintosh)')).toBe('macOS')
  })

  it('recognises the server\'s fingerprint of an endpoint', async () => {
    // SHA-256("https://push.example.com/abc"), first 32 hex characters
    const fp = await endpointFingerprint(ENDPOINT)
    expect(fp).toMatch(/^[0-9a-f]{32}$/)
    expect(await endpointFingerprint(ENDPOINT)).toBe(fp)
    expect(await endpointFingerprint(ENDPOINT + 'x')).not.toBe(fp)
  })
})

describe('turning push on', () => {
  it('asks for permission, subscribes with the server key and registers the device', async () => {
    const config = await loadConfig()
    expect(await enablePush(config)).toBe('enabled')
    expect(Notification.requestPermission).toHaveBeenCalled()
    expect(subscribe).toHaveBeenCalledWith({ userVisibleOnly: true, applicationServerKey: new Uint8Array([1, 2, 3]) })
    const post = fakeApi.calls.find(c => c.method === 'POST')
    expect(post).toMatchObject({ path: '/api/push/subscriptions', json: { endpoint: ENDPOINT, p256dh: 'PUB', auth: 'SEC', label: '' } })
    expect(pushOnThisDevice.value).toBe(true)
  })

  it('is not allowed to subscribe when the person says no', async () => {
    stubBrowser('default', 'denied')
    expect(await enablePush(await loadConfig())).toBe('denied')
    expect(subscribe).not.toHaveBeenCalled()
    expect(pushOnThisDevice.value).toBe(false)
  })

  it('reuses a subscription made for this server and replaces one made for another key', async () => {
    current = subscription([1, 2, 3])
    await enablePush(await loadConfig())
    expect(subscribe).not.toHaveBeenCalled()

    const old = subscription([9, 9, 9])
    current = old
    await enablePush(await loadConfig())
    expect(old.unsubscribe).toHaveBeenCalled()
    expect(subscribe).toHaveBeenCalledTimes(1)
  })

  it('does nothing when the operator turned push off, or when no worker is registered', async () => {
    expect(await enablePush({ ...(await loadConfig()), enabled: false })).toBe('unavailable')
    expect(await enablePush({ ...(await loadConfig()), public_key: '' })).toBe('unavailable')
    registered = false
    expect(await enablePush(await loadConfig())).toBe('unavailable')
    expect(subscribe).not.toHaveBeenCalled()
  })
})

describe('this device in the list', () => {
  async function knownDevice(): Promise<Record<string, unknown>> {
    return { id: DEV1, label: 'Firefox on Linux', fingerprint: await endpointFingerprint(ENDPOINT), created_at: '2026-10-22T10:00:00Z', last_success_at: null }
  }

  it('is found by the fingerprint of this browser\'s subscription', async () => {
    current = subscription()
    fakeApi.devices = [{ ...(await knownDevice()), id: 'cccccccc-cccc-4ccc-8ccc-cccccccccccc', fingerprint: 'f'.repeat(32) }, await knownDevice()]
    const devices = (await import('./webPush')).loadDevices
    expect((await thisDevice(await devices()))?.id).toBe(DEV1)
    current = null
    expect(await thisDevice(await devices())).toBeNull()
  })

  it('decides whether the page leaves notifications to the worker', async () => {
    current = subscription()
    fakeApi.devices = [await knownDevice()]
    expect(await refreshPushState()).toBe(true)
    expect(pushOnThisDevice.value).toBe(true)
    stubBrowser('denied')
    current = subscription()
    expect(await refreshPushState()).toBe(false)
    fakeApi.failPath = '/api/push/subscriptions'
    stubBrowser('granted')
    expect(await refreshPushState()).toBe(false)
  })

  it('removing this device ends its subscription too; removing another leaves it alone', async () => {
    current = subscription()
    const mine = await knownDevice()
    await removeDevice({ ...mine } as never)
    expect(fakeApi.calls.at(-1)).toMatchObject({ path: `/api/push/subscriptions/${DEV1}`, method: 'DELETE' })
    expect(current?.unsubscribe).toHaveBeenCalled()

    current = subscription()
    await removeDevice({ ...mine, id: DEV2, fingerprint: 'e'.repeat(32) } as never)
    expect(current.unsubscribe).not.toHaveBeenCalled()
  })

  it('removes another device from the list even when the service worker never becomes active', async () => {
    vi.useFakeTimers()
    try {
      const sw = (navigator as unknown as { serviceWorker: { ready: Promise<unknown> } }).serviceWorker
      sw.ready = new Promise(() => {})
      const removing = removeDevice({ id: DEV2, fingerprint: 'e'.repeat(32) } as never)
      await vi.advanceTimersByTimeAsync(10_000)
      await removing
      expect(fakeApi.calls.at(-1)).toMatchObject({ path: `/api/push/subscriptions/${DEV2}`, method: 'DELETE' })
    } finally { vi.useRealTimers() }
  })

  it('no subscription means nothing to find', async () => {
    expect(await currentSubscription()).toBeNull()
    pwa.allowed = false
    expect(await currentSubscription()).toBeNull()
  })
})

describe('signing out', () => {
  it('releases this browser quietly, so the next account here is not sent these messages', async () => {
    current = subscription()
    fakeApi.devices = [{ id: DEV1, label: 'x', fingerprint: await endpointFingerprint(ENDPOINT), created_at: '2026-10-22T10:00:00Z', last_success_at: null }]
    pushOnThisDevice.value = true
    await releaseThisDevice()
    expect(fakeApi.calls.filter(c => c.method !== 'GET' || c.path.includes('subscriptions')).every(c => c.quiet)).toBe(true)
    expect(fakeApi.calls.some(c => c.method === 'DELETE')).toBe(true)
    expect(pushOnThisDevice.value).toBe(false)
  })

  it('never fails sign-out and never waits long for a slow server', async () => {
    current = subscription()
    fakeApi.failPath = '/api/push/subscriptions'
    await expect(releaseThisDevice()).resolves.toBeUndefined()
    pwa.allowed = false
    await expect(releaseThisDevice()).resolves.toBeUndefined()
  })
})

describe('the small calls', () => {
  it('send the test, and the preview choice', async () => {
    expect(await sendTestPush()).toEqual({ delivered: 1, failed: 0 })
    await setPreview(false)
    expect(fakeApi.calls.at(-1)).toMatchObject({ path: '/api/push/preferences', method: 'PUT', json: { preview: false } })
  })
})

describe('a click on a notification reaches the page', () => {
  const channel = '11111111-1111-4111-8111-111111111111'
  const message = '22222222-2222-4222-8222-222222222222'

  it('is read only when the worker sent it', () => {
    expect(parseOpenRequest({ type: 'mnema:open-message', channel_id: channel, message_id: message, parent_id: null })).toEqual({ channel_id: channel, message_id: message, parent_id: null })
    expect(parseOpenRequest({ type: 'mnema:open-message', channel_id: channel, message_id: message, parent_id: message })?.parent_id).toBe(message)
    for (const bad of [null, 'x', {}, { type: 'cccccccc-cccc-4ccc-8ccc-cccccccccccc', channel_id: channel, message_id: message }, { type: 'mnema:open-message', channel_id: 'x', message_id: message }, { type: 'mnema:open-message', channel_id: channel }]) {
      expect(parseOpenRequest(bad)).toBeNull()
    }
  })

  it('calls back for each valid message and stops on request', () => {
    const open = vi.fn()
    const stop = listenForOpenRequests(open)
    const add = vi.mocked(navigator.serviceWorker.addEventListener)
    const handler = add.mock.calls[0]![1] as (e: MessageEvent) => void
    handler(new MessageEvent('message', { data: { type: 'mnema:open-message', channel_id: channel, message_id: message, parent_id: null } }))
    handler(new MessageEvent('message', { data: { type: 'something-else' } }))
    expect(open).toHaveBeenCalledTimes(1)
    stop()
    expect(navigator.serviceWorker.removeEventListener).toHaveBeenCalledWith('message', handler)
  })

  it('reads a clicked event reminder, and passes it on separately from messages', () => {
    const event = '44444444-4444-4444-8444-444444444444'
    expect(parseOpenEvent({ type: 'mnema:open-event', event_id: event })).toBe(event)
    for (const bad of [null, 'x', {}, { type: 'mnema:open-event', event_id: '../x' }, { type: 'mnema:open-message', event_id: event }]) {
      expect(parseOpenEvent(bad)).toBeNull()
    }
    const open = vi.fn()
    const openEvent = vi.fn()
    listenForOpenRequests(open, openEvent)
    const add = vi.mocked(navigator.serviceWorker.addEventListener)
    const handler = add.mock.calls.at(-1)![1] as (e: MessageEvent) => void
    handler(new MessageEvent('message', { data: { type: 'mnema:open-event', event_id: event } }))
    expect(openEvent).toHaveBeenCalledWith(event)
    expect(open).not.toHaveBeenCalled()
  })

  it('is a no-op in a browser without service workers', () => {
    vi.stubGlobal('navigator', {})
    expect(listenForOpenRequests(vi.fn())()).toBeUndefined()
  })
})
