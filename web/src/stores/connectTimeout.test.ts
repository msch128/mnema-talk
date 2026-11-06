import { required } from '../store-test-support.fixture'
import { fixtureId, userFixture } from '../test-fixtures.fixture'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useChatStore } from './chat'
import { useAuthStore } from './auth'

const USER_ID = fixtureId(1)

// Minimal WebSocket stand-in the tests open and close by hand.
class FakeSocket {
  static instances: FakeSocket[] = []
  closed = false
  onclose: ((event?: { code?: number }) => void) | null = null
  onopen: (() => void) | null = null
  constructor(readonly url: string) { FakeSocket.instances.push(this) }
  send() {}
  close() { this.closed = true }
  open() { this.onopen?.() }
  drop() { this.onclose?.() }
}

function setup() {
  const auth = useAuthStore()
  auth.user = userFixture({ id: USER_ID })
  vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(new Response(JSON.stringify(userFixture({ id: USER_ID })), { status: 200 }))))
  return useChatStore()
}

// The store listens on window and document for its whole life: drop the listeners of each test's store.
const listeners: Array<[EventTarget, string, EventListenerOrEventListenerObject]> = []

beforeEach(() => {
  for (const target of [window, document]) {
    const add = target.addEventListener.bind(target)
    vi.spyOn(target, 'addEventListener').mockImplementation((type: string, fn: EventListenerOrEventListenerObject, opts?: boolean | AddEventListenerOptions) => {
      listeners.push([target, type, fn])
      add(type, fn, opts)
    })
  }
  setActivePinia(createPinia())
  FakeSocket.instances = []
  vi.stubGlobal('WebSocket', Object.assign(FakeSocket, { OPEN: 1 }))
  vi.useFakeTimers()
})

afterEach(() => {
  vi.restoreAllMocks()
  for (const [target, type, fn] of listeners.splice(0)) target.removeEventListener(type, fn)
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('a websocket connect that never opens', () => {
  it('is closed after the connect timeout and replaced with backoff', async () => {
    const chat = setup()
    chat.initWebSocket()
    await vi.advanceTimersByTimeAsync(10_000)
    expect(required(FakeSocket.instances[0]).closed).toBe(true)
    expect(chat.isConnected).toBe(false)
    await vi.advanceTimersByTimeAsync(1000)
    expect(FakeSocket.instances).toHaveLength(2)
    chat.closeWebSocket()
  })

  it('is not timed out once it opened', async () => {
    const chat = setup()
    chat.initWebSocket()
    required(FakeSocket.instances[0]).open()
    await vi.advanceTimersByTimeAsync(12_000)
    expect(chat.isConnected).toBe(true)
    expect(required(FakeSocket.instances[0]).closed).toBe(false)
    chat.closeWebSocket()
  })

  it('"Retry now" replaces a socket stuck in connecting; the old one can no longer interfere', async () => {
    const chat = setup()
    chat.initWebSocket()
    chat.retryNow()
    await vi.advanceTimersByTimeAsync(0)
    expect(required(FakeSocket.instances[0]).closed).toBe(true)
    expect(FakeSocket.instances).toHaveLength(2)
    required(FakeSocket.instances[1]).open()
    expect(chat.isConnected).toBe(true)
    required(FakeSocket.instances[0]).drop()
    expect(chat.isConnected).toBe(true)
    chat.closeWebSocket()
  })

  it.each(['online', 'visibilitychange'])('a %s event leaves a young connect alone and replaces one past half the timeout', async (event) => {
    const chat = setup()
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' })
    chat.initWebSocket()
    const fire = () => (event === 'online' ? window : document).dispatchEvent(new Event(event))
    await vi.advanceTimersByTimeAsync(2000)
    fire()
    await vi.advanceTimersByTimeAsync(0)
    expect(FakeSocket.instances).toHaveLength(1)
    expect(required(FakeSocket.instances[0]).closed).toBe(false)

    await vi.advanceTimersByTimeAsync(3500)
    fire()
    await vi.advanceTimersByTimeAsync(0)
    expect(required(FakeSocket.instances[0]).closed).toBe(true)
    expect(FakeSocket.instances).toHaveLength(2)
    chat.closeWebSocket()
  })
})
