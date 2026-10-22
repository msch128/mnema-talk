import { describe, expect, it, vi } from 'vitest'
import { notificationOptions, onNotificationClick, onPush, onSubscriptionChange, openTarget, parsePush } from './push'
import type { ClientLike, PushScope } from './push'

const CHANNEL = '11111111-1111-4111-8111-111111111111'
const MESSAGE = '22222222-2222-4222-8222-222222222222'
const ROOT = '33333333-3333-4333-8333-333333333333'

const payload = (over: Record<string, unknown> = {}) => ({
  v: 1, type: 'message', title: 'Alice (#general)', body: 'Hello', tag: CHANNEL, ts: 1_700_000_000_000,
  channel_id: CHANNEL, message_id: MESSAGE, mention: false, ...over,
})

function client(visibilityState: string) {
  return { visibilityState, focus: vi.fn(async () => {}), postMessage: vi.fn<(message: unknown) => void>() } satisfies ClientLike
}

function scope(options: { windows?: ClientLike[], waiting?: number } = {}) {
  const shown: Array<{ title: string, options: Record<string, unknown> }> = []
  const nav = { setAppBadge: vi.fn(async () => {}), clearAppBadge: vi.fn(async () => {}) }
  const subscribe = vi.fn(async () => ({ toJSON: () => ({ endpoint: 'https://push.example/new', keys: { p256dh: 'PUB', auth: 'SECRET' } }) }))
  const fetch = vi.fn(async () => new Response(null, { status: 201 }))
  const s: PushScope = {
    registration: {
      showNotification: async (title, opts) => { shown.push({ title, options: opts }) },
      getNotifications: async () => Array.from({ length: options.waiting ?? shown.length }),
      pushManager: { subscribe },
    },
    clients: { matchAll: async () => options.windows ?? [], openWindow: vi.fn(async () => null) },
    navigator: nav,
    fetch,
  }
  return { scope: s, shown, nav, subscribe, fetch }
}

const event = (data: unknown) => ({ data: data === null ? null : { json: () => data }, waitUntil: vi.fn() })

describe('reading a push', () => {
  it('accepts the server payload and normalises the ids', () => {
    expect(parsePush(payload({ parent_id: ROOT.toUpperCase(), mention: true }))).toEqual({
      type: 'message', title: 'Alice (#general)', body: 'Hello', tag: CHANNEL, timestamp: 1_700_000_000_000,
      channelId: CHANNEL, messageId: MESSAGE, parentId: ROOT, mention: true,
    })
  })

  it('refuses what is not ours', () => {
    for (const bad of [null, 'text', 42, {}, payload({ v: 2 }), payload({ type: 'other' }), payload({ title: 7 }), payload({ body: 'x'.repeat(600) })]) {
      expect(parsePush(bad)).toBeNull()
    }
  })

  it('drops ids that are not UUIDs, so they cannot shape a URL', () => {
    const parsed = parsePush(payload({ channel_id: '../../admin', message_id: 'x', parent_id: 5, tag: undefined, ts: 'now' }))
    expect(parsed).toMatchObject({ channelId: null, messageId: null, parentId: null, tag: 'message' })
    expect(Number.isFinite(parsed?.timestamp)).toBe(true)
  })

  it('tags the notification with the channel so the page\'s own one replaces it', () => {
    const options = notificationOptions(parsePush(payload())!)
    expect(options).toMatchObject({ body: 'Hello', tag: CHANNEL, renotify: true, icon: '/pwa-icon-192.png', badge: '/pwa-badge-96.png' })
    expect(options['data']).toEqual({ channel_id: CHANNEL, message_id: MESSAGE, parent_id: null })
  })
})

describe('a push arrives', () => {
  it('shows the notification and puts the number of waiting ones on the app icon', async () => {
    const { scope: s, shown, nav } = scope({ windows: [client('hidden')] })
    await onPush(s, event(payload()))
    expect(shown).toHaveLength(1)
    expect(shown[0]).toMatchObject({ title: 'Alice (#general)', options: { body: 'Hello' } })
    expect(nav.setAppBadge).toHaveBeenCalledWith(1)
  })

  it('shows nothing for a message while the app is on screen: the page shows it itself', async () => {
    const { scope: s, shown } = scope({ windows: [client('hidden'), client('visible')] })
    await onPush(s, event(payload()))
    expect(shown).toEqual([])
  })

  it('always shows the test push, even with the app on screen', async () => {
    const { scope: s, shown } = scope({ windows: [client('visible')] })
    await onPush(s, event(payload({ type: 'test', title: 'Mnema Talk', body: 'Push works', tag: 'test', channel_id: undefined, message_id: undefined })))
    expect(shown).toHaveLength(1)
    expect(shown[0]?.title).toBe('Mnema Talk')
  })

  it('still shows something for a payload it cannot read, as the browser requires', async () => {
    for (const data of [null, { v: 1 }, 'not an object']) {
      const { scope: s, shown } = scope()
      await onPush(s, event(data))
      expect(shown).toHaveLength(1)
      expect(shown[0]?.title).toBe('Mnema Talk')
    }
    const broken = { data: { json: () => { throw new SyntaxError('bad') } }, waitUntil: vi.fn() }
    const { scope: s, shown } = scope()
    await onPush(s, broken)
    expect(shown).toHaveLength(1)
  })

  it('clears the badge when no notification is left, and survives a browser without one', async () => {
    const { scope: s, nav } = scope({ waiting: 0 })
    await onPush(s, event(payload()))
    expect(nav.clearAppBadge).toHaveBeenCalled()
    const { scope: bare } = scope()
    delete (bare as { navigator?: unknown }).navigator
    await expect(onPush(bare, event(payload()))).resolves.toBeUndefined()
  })

  it('does not fail when the badge cannot be set', async () => {
    const { scope: s, nav } = scope()
    nav.setAppBadge.mockRejectedValueOnce(new Error('denied'))
    await expect(onPush(s, event(payload()))).resolves.toBeUndefined()
  })
})

describe('a notification is clicked', () => {
  const click = (data: unknown) => ({ notification: { data, close: vi.fn() }, waitUntil: vi.fn() })

  it('where to go: the message, or the root of its thread, or the start page', () => {
    expect(openTarget({ channel_id: CHANNEL, message_id: MESSAGE, parent_id: null }).url).toBe(`/c/${CHANNEL}/m/${MESSAGE}`)
    expect(openTarget({ channel_id: CHANNEL, message_id: MESSAGE, parent_id: ROOT }).url).toBe(`/c/${CHANNEL}/m/${ROOT}`)
    expect(openTarget({ channel_id: CHANNEL, message_id: MESSAGE, parent_id: ROOT }).message).toEqual({
      type: 'mnema:open-message', channel_id: CHANNEL, message_id: MESSAGE, parent_id: ROOT,
    })
    for (const odd of [undefined, null, 'x', {}, { channel_id: '../x', message_id: MESSAGE }]) {
      expect(openTarget(odd)).toEqual({ url: '/', message: null })
    }
  })

  it('focuses the app on screen and tells it which message to show', async () => {
    const hidden = client('hidden')
    const shown = client('visible')
    const { scope: s } = scope({ windows: [hidden, shown], waiting: 0 })
    const e = click({ channel_id: CHANNEL, message_id: MESSAGE, parent_id: null })
    await onNotificationClick(s, e)
    expect(e.notification.close).toHaveBeenCalled()
    expect(shown.focus).toHaveBeenCalled()
    expect(shown.postMessage).toHaveBeenCalledWith({ type: 'mnema:open-message', channel_id: CHANNEL, message_id: MESSAGE, parent_id: null })
    expect(hidden.postMessage).not.toHaveBeenCalled()
  })

  it('falls back to the first open window when none is on screen', async () => {
    const only = client('hidden')
    const { scope: s } = scope({ windows: [only] })
    await onNotificationClick(s, click({ channel_id: CHANNEL, message_id: MESSAGE }))
    expect(only.focus).toHaveBeenCalled()
  })

  it('opens the app at the message when it is closed', async () => {
    const { scope: s } = scope({ windows: [] })
    await onNotificationClick(s, click({ channel_id: CHANNEL, message_id: MESSAGE, parent_id: ROOT }))
    expect(s.clients.openWindow).toHaveBeenCalledWith(`/c/${CHANNEL}/m/${ROOT}`)
  })

  it('only focuses for a test notification', async () => {
    const win = client('visible')
    const { scope: s } = scope({ windows: [win] })
    await onNotificationClick(s, click({ channel_id: null, message_id: null, parent_id: null }))
    expect(win.focus).toHaveBeenCalled()
    expect(win.postMessage).not.toHaveBeenCalled()
  })
})

describe('the browser replaces the subscription', () => {
  it('subscribes the same way again and tells the server', async () => {
    const { scope: s, subscribe, fetch } = scope()
    const options = { userVisibleOnly: true, applicationServerKey: new Uint8Array([1, 2, 3]) }
    await onSubscriptionChange(s, { oldSubscription: { options }, waitUntil: vi.fn() })
    expect(subscribe).toHaveBeenCalledWith(options)
    const [url, init] = fetch.mock.calls[0] as unknown as [string, RequestInit]
    expect(url).toBe('/api/push/subscriptions')
    expect(init).toMatchObject({ method: 'POST', credentials: 'same-origin' })
    expect(JSON.parse(String(init.body))).toEqual({ endpoint: 'https://push.example/new', p256dh: 'PUB', auth: 'SECRET', label: '' })
  })

  it('does nothing without an old subscription to copy, or without keys', async () => {
    const { scope: s, subscribe, fetch } = scope()
    await onSubscriptionChange(s, { oldSubscription: null, waitUntil: vi.fn() })
    expect(subscribe).not.toHaveBeenCalled()
    subscribe.mockResolvedValueOnce({ toJSON: () => ({ endpoint: 'https://push.example/x', keys: { p256dh: '', auth: '' } }) })
    await onSubscriptionChange(s, { oldSubscription: { options: {} }, waitUntil: vi.fn() })
    expect(fetch).not.toHaveBeenCalled()
  })
})
