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
      if (url === '/api/read-state') return jsonResponse([])
      return jsonResponse([{ id: 'm-new', channel_id: 'ch1' }])
    }))
    chat.activeChannel = { id: 'ch1', type: 'text' }
    return { chat, calls }
  }

  it('resyncs channels, members and the open channel after a reconnect', async () => {
    const { chat, calls } = setup()
    chat.initWebSocket()
    FakeSocket.instances[0].open()
    await vi.advanceTimersByTimeAsync(0)
    calls.length = 0

    FakeSocket.instances[0].drop()
    await vi.advanceTimersByTimeAsync(1000)
    FakeSocket.instances[1].open()
    await vi.runOnlyPendingTimersAsync()

    expect(calls).toContain('/api/channels')
    expect(calls).toContain('/api/members')
    expect(calls.some(u => u.startsWith('/api/channels/ch1/messages'))).toBe(true)
    expect(chat.reconnectCount).toBe(1)
  })

  it('catches changes between the initial snapshot and the first socket opening', async () => {
    const { chat, calls } = setup()
    await chat.fetchChannels()
    expect(chat.allChannels.map(c => c.id)).toEqual(['ch1'])

    // The server changes after the snapshot, before this client can receive
    // broadcasts. Opening the socket must recover the missed state.
    const fetch = globalThis.fetch
    fetch.mockImplementation(url => {
      calls.push(url)
      if (url === '/api/channels') return jsonResponse({ categories: [], uncategorized: [
        { id: 'ch1', type: 'text' }, { id: 'ch2', type: 'voice' }
      ] })
      if (url === '/api/members') return jsonResponse([{ id: 'u2', display_name: 'New member' }])
      if (url === '/api/read-state') return jsonResponse([{ channel_id: 'ch1', unread_count: 1 }])
      return jsonResponse([{ id: 'm-new', channel_id: 'ch1' }])
    })

    chat.initWebSocket()
    FakeSocket.instances[0].open()
    await vi.advanceTimersByTimeAsync(0)

    expect(chat.allChannels.map(c => c.id)).toEqual(['ch1', 'ch2'])
    expect(chat.members.map(u => u.id)).toEqual(['u2'])
    expect(chat.readStates.ch1.unread_count).toBe(1)
    expect(chat.messages.map(m => m.id)).toEqual(['m-new'])
    // Initial catch-up must not trigger a media rejoin.
    expect(chat.reconnectCount).toBe(0)
    chat.closeWebSocket()
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
