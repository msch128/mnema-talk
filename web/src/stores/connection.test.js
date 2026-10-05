import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useChatStore } from './chat'
import { useAuthStore } from './auth'
import { setLocale } from '../i18n'

// Minimal WebSocket stand-in the tests open and close by hand.
class FakeSocket {
  static instances = []
  constructor(url) {
    this.url = url
    this.sent = []
    FakeSocket.instances.push(this)
  }
  send(data) { this.sent.push(JSON.parse(data)) }
  close() { this.onclose?.() }
  open() { this.readyState = 1; this.onopen?.() }
  drop() { this.onclose?.() }
}

function jsonResponse(body, status = 200) {
  return Promise.resolve({ status, ok: status < 400, json: () => Promise.resolve(body) })
}

beforeEach(() => {
  setLocale('de')
  setActivePinia(createPinia())
  FakeSocket.instances = []
  vi.stubGlobal('WebSocket', Object.assign(FakeSocket, { OPEN: 1 }))
  vi.useFakeTimers()
})

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('auth.checkAuth', () => {
  it('keeps the user on network errors and server errors', async () => {
    const auth = useAuthStore()
    auth.user = { id: 'u1' }
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('offline')))
    expect(await auth.checkAuth()).toBeNull()
    expect(auth.user).toEqual({ id: 'u1' })

    vi.stubGlobal('fetch', vi.fn(() => jsonResponse({ error: { code: 'INTERNAL_ERROR' } }, 502)))
    expect(await auth.checkAuth()).toBeNull()
    expect(auth.isAuthenticated).toBe(true)
  })

  it('logs out only on 401', async () => {
    const auth = useAuthStore()
    auth.user = { id: 'u1' }
    vi.stubGlobal('fetch', vi.fn(() => jsonResponse({ error: { code: 'UNAUTHORIZED' } }, 401)))
    expect(await auth.checkAuth()).toBe(false)
    expect(auth.user).toBeNull()
  })
})

describe('websocket reconnect', () => {
  function setup() {
    const auth = useAuthStore()
    auth.user = { id: 'u1' }
    const chat = useChatStore()
    const calls = []
    vi.stubGlobal('fetch', vi.fn(url => {
      calls.push(url)
      if (url === '/api/auth/me') return jsonResponse({ id: 'u1' })
      if (url === '/api/channels') return jsonResponse({ categories: [], uncategorized: [{ id: 'ch1', type: 'text' }] })
      if (url === '/api/members') return jsonResponse([])
      return jsonResponse({ messages: [{ id: 'm-new', channel_id: 'ch1' }], has_more: false })
    }))
    chat.activeChannel = { id: 'ch1', type: 'text' }
    return { chat, calls }
  }

  it('resyncs channels, members and the open channel after a reconnect', async () => {
    const { chat, calls } = setup()
    chat.initWebSocket()
    FakeSocket.instances[0].open()
    expect(calls).toEqual([]) // the first connect needs no resync

    FakeSocket.instances[0].drop()
    await vi.advanceTimersByTimeAsync(1000)
    FakeSocket.instances[1].open()
    await vi.runOnlyPendingTimersAsync()

    expect(calls).toContain('/api/channels')
    expect(calls).toContain('/api/members')
    expect(calls.some(u => u.startsWith('/api/channels/ch1/messages'))).toBe(true)
    expect(chat.reconnectCount).toBe(1)
  })

  it('keeps retrying while the server is unreachable instead of logging out', async () => {
    const { chat } = setup()
    const auth = useAuthStore()
    chat.initWebSocket()
    FakeSocket.instances[0].open()
    FakeSocket.instances[0].drop()

    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('offline')))
    await vi.advanceTimersByTimeAsync(1000)
    expect(auth.isAuthenticated).toBe(true)
    expect(FakeSocket.instances).toHaveLength(1)

    vi.stubGlobal('fetch', vi.fn(() => jsonResponse({ id: 'u1' })))
    await vi.advanceTimersByTimeAsync(2000)
    expect(FakeSocket.instances).toHaveLength(2)
  })
})
