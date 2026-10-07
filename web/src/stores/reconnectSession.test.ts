import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useAuthStore } from './auth'
import { useChatStore } from './chat'
import { fixtureId, userFixture } from '../test-fixtures.fixture'
import { required } from '../store-test-support.fixture'

class Socket {
  static instances: Socket[] = []
  onopen: (() => void) | null = null
  onclose: (() => void) | null = null
  onmessage: ((event: { data: string }) => void) | null = null
  send = vi.fn<(data: string) => void>()
  close = vi.fn()
  constructor() { Socket.instances.push(this) }
}
function deferredCheck() {
  let finish: (value: boolean | null) => void = () => { throw new Error('Missing auth check') }
  const promise = new Promise<boolean | null>(resolve => { finish = resolve })
  return { promise, finish }
}
const me = userFixture({ id: fixtureId(1) })
const flush = async () => { for (let i = 0; i < 20; i++) await Promise.resolve() }
function setup() {
  useAuthStore().user = { ...me }
  return { chat: useChatStore(), auth: useAuthStore() }
}
beforeEach(() => {
  vi.useFakeTimers(); setActivePinia(createPinia()); Socket.instances = []
  vi.stubGlobal('WebSocket', Socket)
  vi.stubGlobal('fetch', vi.fn<typeof fetch>().mockImplementation(async input => new Response(JSON.stringify(String(input) === '/api/channels' ? { categories: [], uncategorized: [] } : []), { status: 200 })))
  vi.spyOn(window, 'addEventListener'); vi.spyOn(document, 'addEventListener')
})
afterEach(() => {
  useChatStore().closeWebSocket(); vi.clearAllTimers(); vi.useRealTimers()
  for (const [type, listener, options] of vi.mocked(window.addEventListener).mock.calls) window.removeEventListener(type, listener, options)
  for (const [type, listener, options] of vi.mocked(document.addEventListener).mock.calls) document.removeEventListener(type, listener, options)
  vi.restoreAllMocks(); vi.unstubAllGlobals()
})

describe('websocket session lifecycle', () => {
  it('does not reconnect or schedule retries after logout while an auth check is pending', async () => {
    const { chat, auth } = setup(); const pending = deferredCheck()
    const checking = vi.spyOn(auth, 'checkAuth').mockReturnValue(pending.promise)
    chat.retryNow(); expect(checking).toHaveBeenCalledOnce()
    auth.user = null; chat.closeWebSocket(); pending.finish(true); await flush()
    expect(Socket.instances).toHaveLength(0); expect(chat.reconnectAttempt).toBe(0)
    await vi.advanceTimersByTimeAsync(30000)
    expect(checking).toHaveBeenCalledOnce()
  })

  it('does not retry after a stale offline result, even when the same account signs in again', async () => {
    const { chat, auth } = setup(); const pending = deferredCheck()
    const checking = vi.spyOn(auth, 'checkAuth').mockReturnValue(pending.promise)
    chat.retryNow(); auth.user = null; chat.closeWebSocket(); auth.user = { ...me }
    pending.finish(null); await flush(); await vi.advanceTimersByTimeAsync(30000)
    expect(Socket.instances).toHaveLength(0); expect(checking).toHaveBeenCalledOnce()
  })

  it('does not reconnect a different account from the previous account check', async () => {
    const { chat, auth } = setup(); const pending = deferredCheck()
    vi.spyOn(auth, 'checkAuth').mockReturnValue(pending.promise)
    chat.retryNow(); auth.user = userFixture({ id: fixtureId(8) }); pending.finish(true); await flush()
    expect(Socket.instances).toHaveLength(0)
  })

  it('keeps the latest reconnect check in control when checks resolve out of order', async () => {
    const { chat, auth } = setup(); const first = deferredCheck(); const second = deferredCheck()
    const checking = vi.spyOn(auth, 'checkAuth').mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise)
    chat.retryNow(); chat.retryNow(); first.finish(true); await flush()
    expect(Socket.instances).toHaveLength(0)
    second.finish(true); await flush()
    expect(Socket.instances).toHaveLength(1); expect(checking).toHaveBeenCalledTimes(2)
  })

  it('ignores queued old open, message and close callbacks after a new socket is created', async () => {
    const { chat, auth } = setup(); chat.initWebSocket()
    const old = required(Socket.instances[0])
    const open = required(old.onopen), message = required(old.onmessage), close = required(old.onclose)
    chat.closeWebSocket(); auth.user = userFixture({ id: fixtureId(8) }); chat.initWebSocket()
    const current = required(Socket.instances[1]); required(current.onopen)(); await flush()
    open(); message({ data: JSON.stringify({ type: 'presence_snapshot', payload: [me.id] }) }); close()
    expect(chat.isConnected).toBe(true); expect(chat.wasConnected).toBe(true); expect(chat.presenceOf(me.id)).toBe('offline')
    expect(chat.reconnectAttempt).toBe(0); expect(old.send).not.toHaveBeenCalled()
    expect(current.send).toHaveBeenCalled()
  })

  it('ignores queued open/messages after logout even before the caller closes the socket', () => {
    const { chat, auth } = setup(); chat.initWebSocket()
    const socket = required(Socket.instances[0]); auth.user = null
    required(socket.onopen)(); required(socket.onmessage)({ data: JSON.stringify({ type: 'presence_snapshot', payload: [me.id] }) })
    expect(chat.isConnected).toBe(false); expect(chat.presenceOf(me.id)).toBe('offline'); expect(socket.send).not.toHaveBeenCalled()
    required(socket.onclose)(); expect(chat.reconnectAttempt).toBe(0)
  })

  it('makes an unauthenticated explicit retry harmless', async () => {
    const { chat, auth } = setup(); auth.user = null
    const checking = vi.spyOn(auth, 'checkAuth'); chat.retryNow(); await flush()
    expect(checking).not.toHaveBeenCalled(); expect(Socket.instances).toHaveLength(0)
  })
})
