import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useChatStore } from './chat'
import { useAuthStore } from './auth'
import { channelFixture, messageFixture, userFixture, fixtureId } from '../test-fixtures.fixture'
import { required } from '../store-test-support.fixture'
import { ApiError, type ApiOptions } from '../lib/api'
import type { Message } from '../types/domain'

// What a member sees when they write while the connection is down.
const http = vi.hoisted(() => ({ handle: (_url: string, _opts?: ApiOptions): unknown => [] }))
vi.mock('../lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: vi.fn((url: string, options?: ApiOptions) => Promise.resolve(http.handle(url, options)))
}))
import { api } from '../lib/api'

const ch = channelFixture({ id: fixtureId(2) })
const me = userFixture({ id: fixtureId(1), username: 'me', presence: 'online' })
const tick = async () => { for (let i = 0; i < 20; i++) await Promise.resolve() }
const offline = () => new ApiError(0, 'NETWORK', 'Network error')

class Socket {
  static latest: Socket | null = null
  onopen: (() => void) | null = null
  onclose: (() => void) | null = null
  onmessage: ((event: { data: string }) => void) | null = null
  send = vi.fn<(data: string) => void>()
  close = vi.fn()
  constructor(public url: string) { Socket.latest = this }
}

/** The state of the network: while down, every message POST fails like a dropped connection. */
const net = { down: true, refuse: null as ApiError | null, posted: [] as { content: string; nonce: string }[] }

function stored(content: string, nonce: string, id: string): Message {
  return messageFixture({ id, channel_id: ch.id, user_id: me.id, content, nonce })
}

function setup() {
  const chat = useChatStore()
  useAuthStore().user = { ...me }
  chat.activeChannel = { ...ch }
  chat.uncategorized = [ch]
  chat.members = [me]
  return chat
}

beforeEach(() => {
  vi.useFakeTimers()
  setActivePinia(createPinia())
  net.down = true
  net.refuse = null
  net.posted = []
  let n = 100
  http.handle = (url, opts) => {
    if (!url.endsWith('/messages') || opts?.method !== 'POST') return url === '/api/channels' ? { categories: [], uncategorized: [ch] } : []
    if (net.down) throw offline()
    if (net.refuse) throw net.refuse
    const json = opts.json as { content: string; nonce: string }
    net.posted.push({ content: json.content, nonce: json.nonce })
    return stored(json.content, json.nonce, fixtureId(n++))
  }
  vi.mocked(api).mockClear()
  vi.stubGlobal('WebSocket', Socket)
  vi.stubGlobal('Notification', undefined)
  vi.spyOn(console, 'warn').mockImplementation(() => {})
  vi.spyOn(console, 'error').mockImplementation(() => {})
  Socket.latest = null
  vi.spyOn(window, 'addEventListener')
  vi.spyOn(document, 'addEventListener')
})
afterEach(() => {
  useChatStore().closeWebSocket()
  vi.clearAllTimers()
  vi.useRealTimers()
  for (const [type, listener, options] of vi.mocked(window.addEventListener).mock.calls) window.removeEventListener(type, listener, options)
  for (const [type, listener, options] of vi.mocked(document.addEventListener).mock.calls) document.removeEventListener(type, listener, options)
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

async function connectAgain(chat: ReturnType<typeof setup>) {
  net.down = false
  chat.initWebSocket()
  required(Socket.latest).onopen?.()
  await tick()
}

describe('writing while the connection is down', () => {
  it('keeps the message as pending instead of failing, so the composer can clear', async () => {
    const chat = setup()
    const sent = await chat.sendMessage('hello', null, null, [], ch.id)
    expect(sent).toBeTruthy()
    expect(chat.outbox.map(p => [p.content, p.status])).toEqual([['hello', 'waiting']])
    expect(chat.messages).toEqual([])
  })

  it('still reports a refusal by the server and keeps nothing', async () => {
    const chat = setup()
    net.down = false
    net.refuse = new ApiError(403, 'FORBIDDEN', 'missing permission')
    await expect(chat.sendMessage('nope', null, null, [], ch.id)).rejects.toMatchObject({ status: 403 })
    expect(chat.outbox).toEqual([])
  })

  it('does not keep a message with attachments: it fails as before', async () => {
    const chat = setup()
    await expect(chat.sendMessage('with file', null, null, [fixtureId(40)], ch.id)).rejects.toMatchObject({ code: 'NETWORK' })
    expect(chat.outbox).toEqual([])
  })

  it('sends what waited once the connection returns, in the order it was written and only once', async () => {
    const chat = setup()
    await chat.sendMessage('first', null, null, [], ch.id)
    await chat.sendMessage('second', null, null, [], ch.id)
    await chat.sendMessage('third', null, null, [], ch.id)
    expect(chat.outbox).toHaveLength(3)
    await connectAgain(chat)
    expect(net.posted.map(p => p.content)).toEqual(['first', 'second', 'third'])
    expect(new Set(net.posted.map(p => p.nonce)).size).toBe(3)
    expect(chat.outbox).toEqual([])
    expect(chat.messages.map(m => m.content)).toEqual(['first', 'second', 'third'])
    // Nothing is sent again later.
    await vi.advanceTimersByTimeAsync(120_000)
    expect(net.posted).toHaveLength(3)
  })

  it('tries again by itself while the socket stays up, keeping the order', async () => {
    const chat = setup()
    await chat.sendMessage('a', null, null, [], ch.id)
    await chat.sendMessage('b', null, null, [], ch.id)
    await vi.advanceTimersByTimeAsync(6000)
    expect(net.posted).toEqual([])
    net.down = false
    await vi.advanceTimersByTimeAsync(40_000)
    expect(net.posted.map(p => p.content)).toEqual(['a', 'b'])
    expect(chat.outbox).toEqual([])
  })

  it('queues a new message behind pending ones even when the network is back, so the order holds', async () => {
    const chat = setup()
    await chat.sendMessage('early', null, null, [], ch.id)
    net.down = false
    const sent = await chat.sendMessage('late', null, null, [], ch.id)
    expect(sent).toBeTruthy()
    await tick()
    expect(net.posted.map(p => p.content)).toEqual(['early', 'late'])
  })

  it('replaces the pending message in place when the server announces it with its nonce, and never posts it again', async () => {
    const chat = setup()
    await chat.sendMessage('echoed', null, null, [], ch.id)
    const nonce = required(chat.outbox[0]).nonce
    chat.handleWSEvent({ type: 'message_create', payload: stored('echoed', nonce, fixtureId(60)) })
    expect(chat.outbox).toEqual([])
    expect(chat.messages.map(m => m.id)).toEqual([fixtureId(60)])
    net.down = false
    await vi.advanceTimersByTimeAsync(60_000)
    expect(net.posted).toEqual([])
    expect(chat.messages).toHaveLength(1)
  })

  it('lets the member discard a pending message before it is sent', async () => {
    const chat = setup()
    await chat.sendMessage('oops', null, null, [], ch.id)
    await chat.sendMessage('keep', null, null, [], ch.id)
    chat.cancelPending(required(chat.outbox[0]).nonce)
    await connectAgain(chat)
    expect(net.posted.map(p => p.content)).toEqual(['keep'])
  })

  it('lets the member change the text of a pending message, and an emptied text discards it', async () => {
    const chat = setup()
    await chat.sendMessage('typo', null, null, [], ch.id)
    await chat.sendMessage('remove me', null, null, [], ch.id)
    chat.editPending(required(chat.outbox[0]).nonce, '  fixed  ')
    chat.editPending(required(chat.outbox[1]).nonce, '   ')
    expect(chat.outbox.map(p => p.content)).toEqual(['fixed'])
    await connectAgain(chat)
    expect(net.posted.map(p => p.content)).toEqual(['fixed'])
  })

  it('marks a message the server refuses as failed, offers a retry, and goes on with the next', async () => {
    const chat = setup()
    await chat.sendMessage('refused', null, null, [], ch.id)
    await chat.sendMessage('fine', null, null, [], ch.id)
    net.down = false
    const refusal = new ApiError(403, 'FORBIDDEN', 'missing permission')
    const accept = http.handle
    http.handle = (url, opts) => (opts?.json as { content?: string } | undefined)?.content === 'refused' ? (() => { throw refusal })() : accept(url, opts)
    chat.initWebSocket()
    required(Socket.latest).onopen?.()
    await tick()
    expect(chat.outbox.map(p => [p.content, p.status])).toEqual([['refused', 'failed']])
    expect(net.posted.map(p => p.content)).toEqual(['fine'])
    http.handle = accept
    chat.retryPending(required(chat.outbox[0]).nonce)
    await tick()
    expect(chat.outbox).toEqual([])
    expect(net.posted.map(p => p.content)).toEqual(['fine', 'refused'])
  })

  it('keeps a thread reply in its thread and sends it with its parent', async () => {
    const chat = setup()
    const root = fixtureId(70)
    await chat.sendThreadReply(root, ch.id, 'in thread')
    expect(chat.outbox.map(p => [p.parentId, p.channelId])).toEqual([[root, ch.id]])
    await connectAgain(chat)
    const last = vi.mocked(api).mock.calls.filter(([, o]) => o?.method === 'POST').at(-1)
    expect(last?.[1]?.json).toMatchObject({ content: 'in thread', parent_id: root })
  })

  it('drops pending messages of the previous community on a switch and sends nothing', async () => {
    const chat = setup()
    await chat.sendMessage('left behind', null, null, [], ch.id)
    chat.resetCommunityState()
    expect(chat.outbox).toEqual([])
    net.down = false
    await vi.advanceTimersByTimeAsync(60_000)
    expect(net.posted).toEqual([])
  })
})
