import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useSocketStore, type SocketHost } from './socket'
import { useChatStore } from './chat'
import { useAuthStore } from './auth'
import { fixtureId, userFixture } from '../test-fixtures.fixture'

// The chat store reads its state again after a connect; no server answers here.
vi.mock('../shared/lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../shared/lib/api')>()),
  api: vi.fn(() => Promise.resolve([]))
}))

class FakeSocket {
  static opened: FakeSocket[] = []
  sent: string[] = []
  closed = false
  onopen: (() => void) | null = null
  onmessage: ((event: { data: string }) => void) | null = null
  onclose: ((event?: { code?: number }) => void) | null = null
  constructor(public url: string) { FakeSocket.opened.push(this) }
  send(data: string) { this.sent.push(data) }
  close() { this.closed = true }
}

const last = () => {
  const socket = FakeSocket.opened.at(-1)
  if (!socket) throw new Error('no socket was opened')
  return socket
}
const types = (socket: FakeSocket) => socket.sent.map(frame => (JSON.parse(frame) as { type: string }).type)

function hostFixture(overrides: Partial<SocketHost> = {}) {
  const calls: string[] = []
  const host: SocketHost = {
    accountId: () => fixtureId(1),
    isAuthenticated: () => true,
    checkAuth: () => Promise.resolve(true),
    url: () => 'wss://community.example/api/ws',
    opening: () => { calls.push('opening') },
    connected: () => { calls.push('connected') },
    resync: () => { calls.push('resync') },
    message: data => { calls.push(`message:${data}`) },
    dropped: () => { calls.push('dropped') },
    closing: () => { calls.push('closing') },
    ...overrides
  }
  return { host, calls }
}

beforeEach(() => {
  vi.useFakeTimers()
  setActivePinia(createPinia())
  FakeSocket.opened = []
  vi.stubGlobal('WebSocket', FakeSocket)
})
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('the socket store', () => {
  it('opens nothing while no host is attached', () => {
    useSocketStore().open()
    expect(FakeSocket.opened).toHaveLength(0)
  })

  it('opens nothing without an account', () => {
    const socket = useSocketStore()
    socket.transport().host = hostFixture({ accountId: () => undefined }).host
    socket.open()
    expect(FakeSocket.opened).toHaveLength(0)
  })

  it('connects, pings and tells the host in order', () => {
    const socket = useSocketStore()
    const { host, calls } = hostFixture()
    socket.transport().host = host
    socket.open()
    expect(last().url).toBe('wss://community.example/api/ws')
    expect(socket.isConnected).toBe(false)
    last().onopen?.()
    expect(socket.isConnected).toBe(true)
    expect(socket.wasConnected).toBe(true)
    expect(types(last())).toEqual(['ping'])
    expect(calls).toEqual(['opening', 'connected', 'resync'])
  })

  it('sends only while connected', () => {
    const socket = useSocketStore()
    socket.transport().host = hostFixture().host
    socket.open()
    socket.send('typing', { channel_id: fixtureId(2) })
    expect(last().sent).toHaveLength(0)
    last().onopen?.()
    socket.send('typing', { channel_id: fixtureId(2) })
    expect(types(last())).toEqual(['ping', 'typing'])
  })

  it('hands frames of the current connection to the host', () => {
    const socket = useSocketStore()
    const { host, calls } = hostFixture()
    socket.transport().host = host
    socket.open()
    const first = last()
    first.onopen?.()
    first.onmessage?.({ data: 'a' })
    socket.close()
    first.onmessage?.({ data: 'late' })
    expect(calls.filter(call => call.startsWith('message'))).toEqual(['message:a'])
  })

  it('reconnects with a growing wait after the connection drops and counts the reconnect', async () => {
    const socket = useSocketStore()
    const { host, calls } = hostFixture()
    socket.transport().host = host
    socket.open()
    last().onopen?.()
    last().onclose?.()
    expect(calls).toContain('dropped')
    expect(socket.isConnected).toBe(false)
    expect(socket.reconnectAttempt).toBe(1)
    await vi.advanceTimersByTimeAsync(1000)
    expect(FakeSocket.opened).toHaveLength(2)
    last().onopen?.()
    expect(socket.reconnectCount).toBe(1)
    expect(socket.reconnectAttempt).toBe(0)
  })

  it('keeps trying while the server cannot be reached and stops when the session is gone', async () => {
    const socket = useSocketStore()
    let answer: boolean | null = null
    socket.transport().host = hostFixture({ checkAuth: () => Promise.resolve(answer) }).host
    socket.open()
    last().onopen?.()
    last().onclose?.()
    await vi.advanceTimersByTimeAsync(1000)
    expect(FakeSocket.opened).toHaveLength(1)
    expect(socket.reconnectAttempt).toBe(2)
    answer = false
    await vi.advanceTimersByTimeAsync(2000)
    expect(FakeSocket.opened).toHaveLength(1)
    expect(socket.reconnectAttempt).toBe(2)
  })

  it('does not reconnect a tab the server closed for a newer one', async () => {
    const socket = useSocketStore()
    socket.transport().host = hostFixture().host
    socket.open()
    last().onopen?.()
    last().onclose?.({ code: 4001 })
    await vi.advanceTimersByTimeAsync(60000)
    expect(FakeSocket.opened).toHaveLength(1)
    expect(socket.nextRetryAt).toBe(0)
  })

  it('drops a socket that answers no ping', async () => {
    const socket = useSocketStore()
    socket.transport().host = hostFixture().host
    socket.open()
    const first = last()
    first.onopen?.()
    await vi.advanceTimersByTimeAsync(25000)
    expect(first.closed).toBe(true)
    expect(socket.isConnected).toBe(false)
  })

  it('retries at once when the member asks', async () => {
    const socket = useSocketStore()
    socket.transport().host = hostFixture().host
    socket.open()
    last().onopen?.()
    last().onclose?.()
    socket.retryNow()
    await vi.advanceTimersByTimeAsync(0)
    expect(FakeSocket.opened).toHaveLength(2)
  })

  it('closes on purpose without reconnecting', async () => {
    const socket = useSocketStore()
    const { host, calls } = hostFixture()
    socket.transport().host = host
    socket.open()
    const first = last()
    first.onopen?.()
    socket.close()
    expect(first.closed).toBe(true)
    expect(calls.at(-1)).toBe('closing')
    expect(socket.wasConnected).toBe(false)
    await vi.advanceTimersByTimeAsync(60000)
    expect(FakeSocket.opened).toHaveLength(1)
  })
})

describe('the chat store as the facade of the transport', () => {
  it('shows the connection state of the socket store and sends through it', () => {
    const chat = useChatStore()
    const socket = useSocketStore()
    useAuthStore().user = userFixture({ id: fixtureId(1) })
    chat.initWebSocket()
    last().onopen?.()
    expect(socket.isConnected).toBe(true)
    expect(chat.isConnected).toBe(true)
    socket.send('typing', { channel_id: fixtureId(2) })
    chat.sendWSEvent('typing', { channel_id: fixtureId(3) })
    expect(types(last()).filter(type => type === 'typing')).toHaveLength(2)
    chat.closeWebSocket()
    expect(socket.isConnected).toBe(false)
  })
})
