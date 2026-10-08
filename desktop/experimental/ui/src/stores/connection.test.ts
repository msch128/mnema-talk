import { required } from '../store-test-support.fixture'
import { fixtureId, messageFixture, readStateFixture } from '../test-fixtures.fixture'
const USER_ID = fixtureId(1), CHANNEL_ID = fixtureId(2), OTHER_CHANNEL_ID = fixtureId(4), OTHER_USER_ID = fixtureId(5), MESSAGE_ID = fixtureId(6)
import { channelFixture, userFixture } from '../test-fixtures.fixture'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useChatStore } from './chat'
import { useAuthStore } from './auth'
import { setLocale } from '../i18n'

// Minimal WebSocket stand-in the tests open and close by hand.
class FakeSocket {
  static instances: FakeSocket[] = []
  url: string
  sent: unknown[] = []
  readyState = 0
  onclose: (() => void) | null = null
  onopen: (() => void) | null = null
  constructor(url: string) {
    this.url = url
    this.sent = []
    FakeSocket.instances.push(this)
  }
  send(data: string) { this.sent.push(JSON.parse(data)) }
  close() { this.onclose?.() }
  open() { this.readyState = 1; this.onopen?.() }
  drop() { this.onclose?.() }
}

function jsonResponse(body: unknown, status = 200): Promise<Response> {
  return Promise.resolve(new Response(status === 204 ? null : JSON.stringify(body), { status }))
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
    auth.user = userFixture({ id: USER_ID })
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('offline')))
    expect(await auth.checkAuth()).toBeNull()
    expect(auth.user).toEqual(userFixture({ id: USER_ID }))

    vi.stubGlobal('fetch', vi.fn(() => jsonResponse({ error: { code: 'INTERNAL_ERROR' } }, 502)))
    expect(await auth.checkAuth()).toBeNull()
    expect(auth.isAuthenticated).toBe(true)
  })

  it('logs out only on 401', async () => {
    const auth = useAuthStore()
    auth.user = userFixture({ id: USER_ID })
    vi.stubGlobal('fetch', vi.fn(() => jsonResponse({ error: { code: 'UNAUTHORIZED' } }, 401)))
    expect(await auth.checkAuth()).toBe(false)
    expect(auth.user).toBeNull()
  })
})

describe('websocket reconnect', () => {
  function setup() {
    const auth = useAuthStore()
    auth.user = userFixture({ id: USER_ID })
    const chat = useChatStore()
    const calls: string[] = []
    vi.stubGlobal('fetch', vi.fn((url: string) => {
      calls.push(url)
      if (url === '/api/auth/me') return jsonResponse(userFixture({ id: USER_ID }))
      if (url === '/api/channels') return jsonResponse({ categories: [], uncategorized: [channelFixture({ id: CHANNEL_ID, type: 'text' })] })
      if (url === '/api/members') return jsonResponse([])
      if (url === '/api/read-state') return jsonResponse([])
      return jsonResponse([messageFixture({ id: MESSAGE_ID, channel_id: CHANNEL_ID })])
    }))
    chat.activeChannel = channelFixture({ id: CHANNEL_ID, type: 'text' })
    return { chat, calls }
  }

  it('resyncs channels, members and the open channel after a reconnect', async () => {
    const { chat, calls } = setup()
    chat.initWebSocket()
    required(FakeSocket.instances[0]).open()
    await vi.advanceTimersByTimeAsync(0)
    calls.length = 0

    required(FakeSocket.instances[0]).drop()
    await vi.advanceTimersByTimeAsync(1000)
    required(FakeSocket.instances[1]).open()
    await vi.runOnlyPendingTimersAsync()

    expect(calls).toContain('/api/channels')
    expect(calls).toContain('/api/members')
    expect(calls.some(u => u.startsWith(`/api/channels/${CHANNEL_ID}/messages`))).toBe(true)
    expect(chat.reconnectCount).toBe(1)
  })

  it('catches changes between the initial snapshot and the first socket opening', async () => {
    const { chat, calls } = setup()
    await chat.fetchChannels()
    expect(chat.allChannels.map(c => c.id)).toEqual([CHANNEL_ID])

    // The server changes after the snapshot, before this client can receive
    // broadcasts. Opening the socket must recover the missed state.
    const fetch = vi.mocked(globalThis.fetch)
    fetch.mockImplementation(input => {
      const url = String(input)
      calls.push(url)
      if (url === '/api/channels') return jsonResponse({ categories: [], uncategorized: [
        channelFixture({ id: CHANNEL_ID, type: 'text' }), channelFixture({ id: OTHER_CHANNEL_ID, type: 'voice' })
      ] })
      if (url === '/api/members') return jsonResponse([userFixture({ id: OTHER_USER_ID, display_name: 'New member' })])
      if (url === '/api/read-state') return jsonResponse([readStateFixture({ channel_id: CHANNEL_ID, unread_count: 1 })])
      return jsonResponse([messageFixture({ id: MESSAGE_ID, channel_id: CHANNEL_ID })])
    })

    chat.initWebSocket()
    required(FakeSocket.instances[0]).open()
    await vi.advanceTimersByTimeAsync(0)

    expect(chat.allChannels.map(c => c.id)).toEqual([CHANNEL_ID, OTHER_CHANNEL_ID])
    expect(chat.members.map(u => u.id)).toEqual([OTHER_USER_ID])
    expect(required(chat.readStates[CHANNEL_ID]).unread_count).toBe(1)
    expect(chat.messages.map(m => m.id)).toEqual([MESSAGE_ID])
    // Initial catch-up must not trigger a media rejoin.
    expect(chat.reconnectCount).toBe(0)
    chat.closeWebSocket()
  })

  it('keeps retrying while the server is unreachable instead of logging out', async () => {
    const { chat } = setup()
    const auth = useAuthStore()
    chat.initWebSocket()
    required(FakeSocket.instances[0]).open()
    required(FakeSocket.instances[0]).drop()

    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('offline')))
    await vi.advanceTimersByTimeAsync(1000)
    expect(auth.isAuthenticated).toBe(true)
    expect(FakeSocket.instances).toHaveLength(1)

    vi.stubGlobal('fetch', vi.fn(() => jsonResponse(userFixture({ id: USER_ID }))))
    await vi.advanceTimersByTimeAsync(2000)
    expect(FakeSocket.instances).toHaveLength(2)
  })
})
