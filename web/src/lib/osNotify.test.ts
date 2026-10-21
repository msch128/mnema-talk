import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const host = vi.hoisted(() => ({
  invoke: vi.fn(),
  channels: [] as { onmessage?: (event: unknown) => void }[],
}))

vi.mock('@tauri-apps/api/core', () => ({
  invoke: host.invoke,
  Channel: class { onmessage?: (event: unknown) => void; constructor() { host.channels.push(this) } },
}))

import { browserNotify, createHostNotify } from './osNotify'

const toast = (onClick = vi.fn()) => ({ title: 'Anna (#music)', body: 'hello', channelId: 'c1', messageId: 'm1', onClick })
const settle = () => new Promise(resolve => setTimeout(resolve, 0))

afterEach(() => vi.unstubAllGlobals())

describe('browser adapter', () => {
  class Fixture {
    static permission: NotificationPermission = 'default'
    static requestPermission = vi.fn(async (): Promise<NotificationPermission> => 'granted')
    static made: Fixture[] = []
    onclick: (() => void) | null = null
    close = vi.fn()
    constructor(public title: string, public options: NotificationOptions) { Fixture.made.push(this) }
  }

  beforeEach(() => {
    Fixture.made = []
    Fixture.permission = 'default'
    Fixture.requestPermission.mockClear()
    vi.stubGlobal('Notification', Fixture)
  })

  it('reports unsupported without the Notification API', async () => {
    vi.stubGlobal('Notification', undefined)
    expect(browserNotify.permission()).toBe('unsupported')
    expect(await browserNotify.refreshPermission()).toBe('unsupported')
    expect(await browserNotify.requestPermission()).toBe('unsupported')
  })

  it('reads and requests the permission, falling back to the current one on failure', async () => {
    expect(browserNotify.permission()).toBe('default')
    expect(await browserNotify.requestPermission()).toBe('granted')
    Fixture.requestPermission.mockRejectedValueOnce(new Error('blocked'))
    Fixture.permission = 'denied'
    expect(await browserNotify.requestPermission()).toBe('denied')
  })

  it('shows a toast tagged by channel and focuses the window and the message on click', () => {
    const onClick = vi.fn()
    const focus = vi.spyOn(window, 'focus').mockImplementation(() => {})
    browserNotify.notify(toast(onClick))
    const shown = Fixture.made[0]
    expect(shown?.title).toBe('Anna (#music)')
    expect(shown?.options).toMatchObject({ body: 'hello', icon: '/favicon.svg', tag: 'c1' })
    shown?.onclick?.()
    expect(focus).toHaveBeenCalled()
    expect(onClick).toHaveBeenCalled()
    expect(shown?.close).toHaveBeenCalled()
  })

  it('survives a throwing Notification constructor', () => {
    vi.stubGlobal('Notification', class { constructor() { throw new Error('nope') } })
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    expect(() => browserNotify.notify(toast())).not.toThrow()
    expect(warn).toHaveBeenCalled()
  })

  it('sets and clears the app badge where the Badging API exists', () => {
    const set = vi.fn(async () => {})
    const clear = vi.fn(async () => {})
    vi.stubGlobal('navigator', { setAppBadge: set, clearAppBadge: clear })
    browserNotify.setBadge(4)
    browserNotify.setBadge(0)
    expect(set).toHaveBeenCalledWith(4)
    expect(clear).toHaveBeenCalledOnce()
  })

  it('ignores a missing or rejecting Badging API', () => {
    vi.stubGlobal('navigator', {})
    expect(() => browserNotify.setBadge(1)).not.toThrow()
    vi.stubGlobal('navigator', { setAppBadge: () => Promise.reject(new Error('no')) })
    expect(() => browserNotify.setBadge(1)).not.toThrow()
    vi.stubGlobal('navigator', undefined)
    expect(() => browserNotify.setBadge(1)).not.toThrow()
  })
})

describe('host adapter', () => {
  beforeEach(() => {
    host.invoke.mockReset()
    host.invoke.mockResolvedValue(null)
    host.channels.length = 0
  })

  it('starts permitted and takes what the host reports, asking it only through its commands', async () => {
    const adapter = createHostNotify()
    expect(adapter.permission()).toBe('granted')
    host.invoke.mockResolvedValueOnce('default')
    expect(await adapter.refreshPermission()).toBe('default')
    expect(adapter.permission()).toBe('default')
    expect(host.invoke).toHaveBeenLastCalledWith('desktop_notify_permission')
    host.invoke.mockResolvedValueOnce('denied')
    expect(await adapter.requestPermission()).toBe('denied')
    expect(host.invoke).toHaveBeenLastCalledWith('desktop_notify_request_permission')
    expect(adapter.permission()).toBe('denied')
  })

  it('keeps what it knows when the host cannot tell or answers something else', async () => {
    const adapter = createHostNotify()
    host.invoke.mockRejectedValueOnce(new Error('Unknown command.'))
    expect(await adapter.requestPermission()).toBe('granted')
    host.invoke.mockResolvedValueOnce('maybe')
    expect(await adapter.refreshPermission()).toBe('granted')
    host.invoke.mockResolvedValueOnce(null)
    expect(await adapter.refreshPermission()).toBe('granted')
  })

  it('opens the click channel once, then shows the toast with plain arguments', async () => {
    const adapter = createHostNotify()
    adapter.notify(toast())
    adapter.notify({ ...toast(), messageId: 'm2' })
    await settle()
    const opens = host.invoke.mock.calls.filter(([command]) => command === 'desktop_notify_open')
    expect(opens).toHaveLength(1)
    expect(host.invoke).toHaveBeenCalledWith('desktop_notify', { title: 'Anna (#music)', body: 'hello', channelId: 'c1', messageId: 'm1' })
    expect(host.invoke).toHaveBeenCalledWith('desktop_notify', expect.objectContaining({ messageId: 'm2' }))
  })

  it('runs the handler of the toast the host reports, once', async () => {
    const adapter = createHostNotify()
    const onClick = vi.fn()
    adapter.notify(toast(onClick))
    await settle()
    const channel = host.channels[0]
    channel?.onmessage?.({ channel_id: 'other', message_id: 'm1' })
    channel?.onmessage?.({ channel_id: 7, message_id: 'm1' })
    expect(onClick).not.toHaveBeenCalled()
    channel?.onmessage?.({ channel_id: 'c1', message_id: 'm1' })
    channel?.onmessage?.({ channel_id: 'c1', message_id: 'm1' })
    expect(onClick).toHaveBeenCalledOnce()
  })

  it('forgets the oldest waiting toasts', async () => {
    const adapter = createHostNotify()
    const first = vi.fn()
    adapter.notify({ ...toast(first), messageId: 'first' })
    for (let i = 0; i < 40; i++) adapter.notify({ ...toast(), messageId: `m${i}` })
    await settle()
    host.channels[0]?.onmessage?.({ channel_id: 'c1', message_id: 'first' })
    expect(first).not.toHaveBeenCalled()
  })

  it('drops the click handler when the host refuses the toast and retries opening the channel', async () => {
    host.invoke.mockRejectedValueOnce(new Error('denied'))
    const adapter = createHostNotify()
    const onClick = vi.fn()
    adapter.notify(toast(onClick))
    await settle()
    adapter.notify({ ...toast(), messageId: 'm2' })
    await settle()
    expect(host.invoke.mock.calls.filter(([command]) => command === 'desktop_notify_open')).toHaveLength(2)
    host.channels[1]?.onmessage?.({ channel_id: 'c1', message_id: 'm1' })
    expect(onClick).not.toHaveBeenCalled()
  })

  it('sends a bounded whole-number badge and swallows host errors', () => {
    const adapter = createHostNotify()
    adapter.setBadge(3.7)
    adapter.setBadge(-4)
    adapter.setBadge(5000)
    expect(host.invoke.mock.calls.map(([, args]) => (args as { count: number }).count)).toEqual([3, 0, 999])
    host.invoke.mockRejectedValueOnce(new Error('x'))
    expect(() => adapter.setBadge(1)).not.toThrow()
  })
})
