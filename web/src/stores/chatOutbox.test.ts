import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useChatStore } from './chat'
import { useAuthStore } from './auth'
import { useServerFeaturesStore } from './serverFeatures'
import { channelFixture, messageFixture, userFixture, fixtureId } from '../test-fixtures.fixture'
import { required } from '../store-test-support.fixture'
import { ApiError, type ApiOptions } from '../lib/api'
import { DRAFT_SESSION_KEY, beginDraftSession, clearAllDrafts } from '../lib/drafts'
import type { Message } from '../types/domain'
import { t } from '../i18n'

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
const net = { down: true, refuse: null as ApiError | null, posted: [] as { content: string; nonce: string | undefined }[], gate: null as Promise<void> | null, status: 201 }

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
  localStorage.clear()
  vi.useFakeTimers()
  setActivePinia(createPinia())
  useServerFeaturesStore().list = ['messages.nonce']
  net.down = true
  net.refuse = null
  net.posted = []
  net.gate = null
  net.status = 201
  let n = 100
  http.handle = (url, opts) => {
    if (!url.endsWith('/messages') || opts?.method !== 'POST') return url === '/api/channels' ? { categories: [], uncategorized: [ch] } : []
    if (net.down) throw offline()
    if (net.refuse) throw net.refuse
    const json = opts.json as { content: string; nonce?: string }
    net.posted.push({ content: json.content, nonce: json.nonce })
    const message = stored(json.content, json.nonce ?? fixtureId(n), fixtureId(n++))
    return net.gate ? net.gate.then(() => message) : message
  }
  vi.mocked(api).mockReset().mockImplementation((url: string, options?: ApiOptions) => Promise.resolve(http.handle(url, options)).then(body => { options?.onStatus?.(net.status); return body }) as never)
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

/** Lets time pass with the socket answering its pings, so only the outbox is under test. */
async function stayConnected(chat: ReturnType<typeof setup>, ms: number) {
  for (let spent = 0; spent < ms; spent += 4000) {
    await vi.advanceTimersByTimeAsync(4000)
    chat.handleWSEvent({ type: 'pong', payload: { t: Date.now() } })
  }
}

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

  it('sends the nonce only to a server that lists messages.nonce (older ones reject unknown fields)', async () => {
    const chat = setup()
    net.down = false
    await chat.sendMessage('new server', null, null, [], ch.id)
    useServerFeaturesStore().list = []
    await chat.sendMessage('old server', null, null, [], ch.id)
    expect(net.posted.map(p => [p.content, typeof p.nonce])).toEqual([['new server', 'string'], ['old server', 'undefined']])
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

  it('lets the member change the text of a pending message no request has left for, and an emptied text discards it', async () => {
    const chat = setup()
    await chat.sendMessage('first try', null, null, [], ch.id)
    await chat.sendMessage('typo', null, null, [], ch.id)
    await chat.sendMessage('remove me', null, null, [], ch.id)
    chat.editPending(required(chat.outbox[1]).nonce, '  fixed  ')
    chat.editPending(required(chat.outbox[2]).nonce, '   ')
    expect(chat.outbox.map(p => p.content)).toEqual(['first try', 'fixed'])
    await connectAgain(chat)
    expect(net.posted.map(p => p.content)).toEqual(['first try', 'fixed'])
  })

  it('holds the queue while the member edits a pending message, then sends the saved text', async () => {
    const chat = setup()
    await chat.sendMessage('first', null, null, [], ch.id)
    await chat.sendMessage('typo', null, null, [], ch.id)
    const nonce = required(chat.outbox[1]).nonce
    chat.beginEditPending(nonce)
    await connectAgain(chat)
    await stayConnected(chat, 40_000)
    expect(net.posted.map(p => p.content)).toEqual(['first'])
    chat.editPending(nonce, 'fixed')
    await tick()
    expect(net.posted.map(p => p.content)).toEqual(['first', 'fixed'])
  })

  it('sends the old text after the member cancels the edit', async () => {
    const chat = setup()
    await chat.sendMessage('as written', null, null, [], ch.id)
    const nonce = required(chat.outbox[0]).nonce
    chat.beginEditPending(nonce)
    await connectAgain(chat)
    await stayConnected(chat, 40_000)
    expect(net.posted).toEqual([])
    chat.endEditPending(nonce)
    await tick()
    expect(net.posted.map(p => p.content)).toEqual(['as written'])
  })

  it('does not let the member change a message whose first try may have arrived, as the retry would return the old text', async () => {
    const chat = setup()
    await chat.sendMessage('sent into the void', null, null, [], ch.id)
    const nonce = required(chat.outbox[0]).nonce
    chat.editPending(nonce, 'different text')
    expect(chat.outbox.map(p => p.content)).toEqual(['sent into the void'])
    // A rate limit, a session or a CSRF refusal comes before the server looks at the nonce: it says nothing about the first try.
    net.down = false
    net.refuse = new ApiError(403, 'FORBIDDEN', 'missing permission')
    await connectAgain(chat)
    expect(chat.outbox.map(p => p.status)).toEqual(['failed'])
    chat.editPending(nonce, 'different text')
    expect(chat.outbox.map(p => p.content)).toEqual(['sent into the void'])
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

describe('a first send on a stalled network', () => {
  it('ends after the send timeout and waits in the outbox instead of locking the composer', async () => {
    const chat = setup()
    vi.mocked(api).mockImplementation((_url: string, options?: ApiOptions) =>
      new Promise((_, reject) => options?.signal?.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')))))
    let settled = false
    const sending = chat.sendMessage('stuck', null, null, [], ch.id).then(r => { settled = true; return r })
    await vi.advanceTimersByTimeAsync(14_000)
    expect(settled).toBe(false)
    await vi.advanceTimersByTimeAsync(2_000)
    expect(settled).toBe(true)
    expect(await sending).toBeTruthy()
    expect(chat.outbox.map(p => [p.content, p.status, p.possiblyDelivered])).toEqual([['stuck', 'waiting', true]])
  })
})

describe('when the server is the problem, not the connection', () => {
  it('shows a message as not sent after five failed tries instead of blocking the conversation for ever', async () => {
    const chat = setup()
    await chat.sendMessage('poison', null, null, [], ch.id)
    await chat.sendMessage('behind it', null, null, [], ch.id)
    const accept = http.handle
    http.handle = (url, opts) => (opts?.json as { content?: string } | undefined)?.content === 'poison' ? (() => { throw new ApiError(500, 'INTERNAL_ERROR', 'boom') })() : accept(url, opts)
    await connectAgain(chat)
    // While the server only failed, the message waits and keeps its place in line.
    expect(chat.outbox.map(p => [p.status, p.cause])).toEqual([['waiting', 'server'], ['waiting', 'offline']])
    expect(net.posted).toEqual([])
    await stayConnected(chat, 120_000)
    expect(chat.outbox.map(p => [p.content, p.status])).toEqual([['poison', 'failed']])
    expect(net.posted.map(p => p.content)).toEqual(['behind it'])
  })

  it('stops after five tries when the answer cannot be read at all', async () => {
    const chat = setup()
    await chat.sendMessage('garbled', null, null, [], ch.id)
    net.down = false
    net.refuse = new Error('bad json') as unknown as ApiError
    await connectAgain(chat)
    await stayConnected(chat, 120_000)
    expect(chat.outbox.map(p => p.status)).toEqual(['failed'])
    expect(vi.mocked(api).mock.calls.filter(([, o]) => o?.method === 'POST')).toHaveLength(5 + 1)
  })

  it('waits the time a slow-mode refusal names, and keeps the order', async () => {
    const chat = setup()
    await chat.sendMessage('one', null, null, [], ch.id)
    await chat.sendMessage('two', null, null, [], ch.id)
    net.down = false
    net.refuse = new ApiError(429, 'RATE_LIMITED', 'slow mode: wait 20 s before posting again', 20, true)
    await connectAgain(chat)
    net.refuse = null
    const calls = () => vi.mocked(api).mock.calls.filter(([, o]) => o?.method === 'POST').length
    const before = calls()
    await vi.advanceTimersByTimeAsync(15_000)
    expect(calls()).toBe(before)
    await vi.advanceTimersByTimeAsync(10_000)
    expect(net.posted.map(p => p.content)).toEqual(['one', 'two'])
    expect(chat.outbox).toEqual([])
  })

  it('drops a message quietly when the server says it was deleted meanwhile', async () => {
    const chat = setup()
    await chat.sendMessage('purged', null, null, [], ch.id)
    net.down = false
    net.refuse = new ApiError(410, 'MESSAGE_DELETED', 'this message was deleted and is not sent again')
    await connectAgain(chat)
    expect(chat.outbox).toEqual([])
    expect(chat.messages).toEqual([])
  })

  it('treats a request that hangs as a lost connection and tries again', async () => {
    const chat = setup()
    await chat.sendMessage('slow line', null, null, [], ch.id)
    const respond = http.handle
    let hung = true
    vi.mocked(api).mockImplementation((url: string, options?: ApiOptions) => hung && options?.method === 'POST'
      ? new Promise((_, reject) => options.signal?.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError'))))
      : Promise.resolve(respond(url, options)) as never)
    net.down = false
    chat.initWebSocket()
    required(Socket.latest).onopen?.()
    await vi.advanceTimersByTimeAsync(16_000)
    expect(chat.outbox.map(p => [p.status, p.cause])).toEqual([['waiting', 'offline']])
    hung = false
    await vi.advanceTimersByTimeAsync(40_000)
    expect(net.posted.map(p => p.content)).toEqual(['slow line'])
    expect(chat.outbox).toEqual([])
  })
})

describe('when the server announces the message before the request fails', () => {
  it('does not queue a second copy of a message that is already there', async () => {
    const chat = setup()
    const nonces: string[] = []
    http.handle = (url, opts) => {
      if (opts?.method !== 'POST') return []
      const nonce = (opts.json as { nonce: string }).nonce
      nonces.push(nonce)
      // The event beats the failing answer.
      chat.handleWSEvent({ type: 'message_create', payload: stored('raced', nonce, fixtureId(61)) })
      throw offline()
    }
    const sent = await chat.sendMessage('raced', null, null, [], ch.id)
    expect(sent).toBeTruthy()
    expect(chat.outbox).toEqual([])
    expect(chat.messages.map(m => m.id)).toEqual([fixtureId(61)])
  })
})

describe('after the page was closed or the session ended', () => {
  const keep = () => {
    const raw = { ...localStorage }
    return Object.keys(raw).filter(k => k.startsWith('mnema.outbox.'))
  }

  it('sends what was written offline once the member is back, after a reload', async () => {
    const chat = setup()
    await chat.sendMessage('written on the train', null, null, [], ch.id)
    expect(keep()).toHaveLength(1)
    // A reload: a new store, the same browser storage.
    setActivePinia(createPinia())
    const again = setup()
    expect(again.outbox).toEqual([])
    await connectAgain(again)
    expect(net.posted.map(p => p.content)).toEqual(['written on the train'])
    expect(again.outbox).toEqual([])
    expect(keep()).toEqual([])
  })

  it('keeps it through a community switch or an expired session and sends it for the same account later', async () => {
    const chat = setup()
    await chat.sendMessage('still mine', null, null, [], ch.id)
    chat.resetCommunityState()
    expect(chat.outbox).toEqual([])
    expect(keep()).toHaveLength(1)
    await connectAgain(chat)
    expect(net.posted.map(p => p.content)).toEqual(['still mine'])
  })

  it('never hands it to another account', async () => {
    const chat = setup()
    await chat.sendMessage('private', null, null, [], ch.id)
    setActivePinia(createPinia())
    useAuthStore().user = { ...me, id: fixtureId(9) }
    const other = useChatStore()
    other.activeChannel = { ...ch }
    await connectAgain(other)
    expect(net.posted).toEqual([])
    expect(other.outbox).toEqual([])
  })

  it('removes it from this browser when the member signs out', async () => {
    const chat = setup()
    await chat.sendMessage('shared computer', null, null, [], ch.id)
    expect(keep()).toHaveLength(1)
    await useAuthStore().logout()
    expect(keep()).toEqual([])
  })
})

const HOUR = 3600_000
const storageEvent = (key: string, newValue: string | null) => window.dispatchEvent(new StorageEvent('storage', { key, newValue }))
const newTab = () => { setActivePinia(createPinia()); return setup() }

describe('two tabs that are offline at the same time', () => {
  const keys = () => Object.keys(localStorage).filter(k => k.startsWith('mnema.outbox.'))

  it('keep both tabs\' messages and send each of them after a reload', async () => {
    const tabA = setup()
    await tabA.sendMessage('from tab A', null, null, [], ch.id)
    const tabB = newTab()
    await tabB.sendMessage('from tab B', null, null, [], ch.id)
    expect(keys()).toHaveLength(2)
    const reloaded = newTab()
    await connectAgain(reloaded)
    expect(net.posted.map(p => p.content).sort()).toEqual(['from tab A', 'from tab B'])
  })

  it('show the message the other tab wrote, and forget it when that tab sent or discarded it', async () => {
    const tabA = setup()
    const tabB = newTab()
    tabB.initWebSocket()
    await tabA.sendMessage('written in A', null, null, [], ch.id)
    const key = required(keys()[0])
    storageEvent(key, localStorage.getItem(key))
    expect(tabB.outbox.map(p => p.content)).toEqual(['written in A'])
    tabA.cancelPending(required(tabA.outbox[0]).nonce)
    storageEvent(key, null)
    expect(tabB.outbox).toEqual([])
  })

  it('leave what a tab discarded alone when another tab settles a different message', async () => {
    const tabA = setup()
    await tabA.sendMessage('one', null, null, [], ch.id)
    await tabA.sendMessage('two', null, null, [], ch.id)
    const tabB = newTab()
    tabB.initWebSocket()
    for (const k of keys()) storageEvent(k, localStorage.getItem(k))
    tabB.cancelPending(required(tabB.outbox[0]).nonce)
    expect(keys()).toHaveLength(1)
    expect(JSON.parse(localStorage.getItem(required(keys()[0])) ?? '{}')).toMatchObject({ content: 'two' })
  })
})

const posts = () => vi.mocked(api).mock.calls.filter(([url, options]) => url.endsWith('/messages') && options?.method === 'POST').length
/** What the storage event would tell the other tabs: every stored message as it is now. */
const tellOtherTabs = () => { for (const k of Object.keys(localStorage).filter(key => key.startsWith('mnema.outbox.'))) storageEvent(k, localStorage.getItem(k)) }

describe('two tabs that share one waiting message', () => {
  it('do not hand it back and forth while both are offline', async () => {
    const tabA = setup()
    tabA.initWebSocket()
    required(Socket.latest).onopen?.()
    const tabB = newTab()
    tabB.initWebSocket()
    required(Socket.latest).onopen?.()
    await tabA.sendMessage('shared', null, null, [], ch.id)
    for (let second = 0; second < 300; second++) {
      await vi.advanceTimersByTimeAsync(1000)
      for (const tab of [tabA, tabB]) tab.handleWSEvent({ type: 'pong', payload: { t: Date.now() } })
      tellOtherTabs()
    }
    // Backed-off tries of two tabs: a few dozen at most in five minutes, not one per event.
    expect(posts()).toBeLessThan(40)
  })

  it('send it once, not once per tab, when it is back online and one tab has the request out', async () => {
    const tabA = setup()
    tabA.initWebSocket()
    const tabB = newTab()
    tabB.initWebSocket()
    await tabA.sendMessage('shared', null, null, [], ch.id)
    tellOtherTabs()
    const before = posts()
    let release = () => {}
    net.gate = new Promise<void>(resolve => { release = resolve })
    await connectAgain(tabA)
    tellOtherTabs()
    await connectAgain(tabB)
    await vi.advanceTimersByTimeAsync(10000)
    expect(posts() - before).toBe(1)
    release()
    await tick()
    expect(net.posted).toHaveLength(1)
  })

  it('send what another tab was sending when that tab never answers, after the request could have ended', async () => {
    const entry = { nonce: 'n-dead', channelId: ch.id, userId: me.id, content: 'left behind', parentId: null, replyToId: null, createdAt: new Date().toISOString(), status: 'sending', error: '', cause: 'offline', attempts: 0, possiblyDelivered: true, notBefore: 0, owner: 'closed-tab', sendingSince: Date.now() }
    localStorage.setItem('mnema.outbox.' + me.id + '.n-dead', JSON.stringify(entry))
    const chat = setup()
    await connectAgain(chat)
    await stayConnected(chat, 12000)
    expect(net.posted).toEqual([])
    await stayConnected(chat, 32000)
    expect(net.posted.map(p => p.nonce)).toEqual(['n-dead'])
  })
})

describe('messages kept longer than the server remembers their nonce', () => {
  const stale = (over: object) => {
    const entry = { nonce: 'n-old', channelId: ch.id, userId: me.id, content: 'old text', parentId: null, replyToId: null, createdAt: new Date(Date.now() - 25 * HOUR).toISOString(), status: 'waiting', error: '', cause: 'offline', attempts: 0, possiblyDelivered: true, notBefore: 0, ...over }
    localStorage.setItem('mnema.outbox.' + me.id + '.' + entry.nonce, JSON.stringify(entry))
  }

  it('are not sent again by themselves when they may have arrived: they show as not sent', async () => {
    stale({})
    const chat = setup()
    await connectAgain(chat)
    expect(net.posted).toEqual([])
    expect(chat.outbox.map(p => [p.content, p.status])).toEqual([['old text', 'failed']])
  })

  it('are still sent when no try had reached the server', async () => {
    stale({ possiblyDelivered: false })
    const chat = setup()
    await connectAgain(chat)
    expect(net.posted.map(p => p.content)).toEqual(['old text'])
  })

  it('are dropped after a week, and a retry of an expired one is a new send with a new nonce', async () => {
    stale({ nonce: 'n-week', createdAt: new Date(Date.now() - 8 * 24 * HOUR).toISOString() })
    stale({})
    const chat = setup()
    await connectAgain(chat)
    expect(chat.outbox.map(p => p.nonce)).toEqual(['n-old'])
    expect(localStorage.getItem('mnema.outbox.' + me.id + '.n-week')).toBeNull()
    chat.retryPending('n-old')
    await tick()
    expect(net.posted).toHaveLength(1)
    expect(net.posted[0]?.nonce).not.toBe('n-old')
  })

  it('are marked as possibly arrived also when they had already failed, and the retry says so', async () => {
    stale({ status: 'failed', error: 'The server refused it' })
    const chat = setup()
    await connectAgain(chat)
    expect(net.posted).toEqual([])
    expect(required(chat.outbox[0]).error).toBe(t('outbox.expired'))
  })

  it('are marked before a try when they grow old while the page stays open', async () => {
    const chat = setup()
    await chat.sendMessage('long wait', null, null, [], ch.id)
    await vi.advanceTimersByTimeAsync(24 * HOUR)
    net.down = false
    await connectAgain(chat)
    expect(net.posted).toEqual([])
    expect(chat.outbox.map(p => p.status)).toEqual(['failed'])
  })
})

describe('signing out in another tab', () => {
  it('does not let this tab write its waiting messages back, and empties them here', async () => {
    beginDraftSession(me.id)
    try {
      const chat = setup()
      chat.initWebSocket()
      await chat.sendMessage('before sign-out', null, null, [], ch.id)
      expect(chat.outbox).toHaveLength(1)
      // The other tab signed out: the session token and the stored messages are gone.
      localStorage.clear()
      storageEvent(DRAFT_SESSION_KEY, null)
      expect(chat.outbox).toEqual([])
      await chat.sendMessage('after sign-out', null, null, [], ch.id).catch(() => {})
      expect(Object.keys(localStorage).filter(k => k.startsWith('mnema.outbox.'))).toEqual([])
    } finally {
      clearAllDrafts()
    }
  })
})

describe('the read marker after a send', () => {
  const lastRead = { channel_id: ch.id, notify_level: 'all' as const, unread_count: 3, mention_count: 1, last_read_at: '2026-02-01T00:00:00Z' }
  const answerWithOld = () => {
    http.handle = (url, opts) => url.endsWith('/messages') && opts?.method === 'POST'
      ? messageFixture({ id: fixtureId(70), channel_id: ch.id, user_id: me.id, content: 'old', created_at: '2026-01-01T00:00:00Z' })
      : []
  }

  it('does not move backwards when a repeated send answers with the old message', async () => {
    const chat = setup()
    await chat.sendMessage('old', null, null, [], ch.id)
    chat.readStates = { [ch.id]: { ...lastRead } }
    net.down = false
    net.status = 200
    answerWithOld()
    chat.retryPending(required(chat.outbox[0]).nonce)
    await tick()
    expect(chat.readStates[ch.id]).toMatchObject({ last_read_at: '2026-02-01T00:00:00Z', unread_count: 3, mention_count: 1 })
  })

  it('does not move backwards for a repeated send when this device clock is behind the server', async () => {
    const chat = setup()
    vi.setSystemTime(new Date('2025-12-01T00:00:00Z'))
    await chat.sendMessage('old', null, null, [], ch.id)
    chat.readStates = { [ch.id]: { ...lastRead } }
    net.down = false
    net.status = 200
    http.handle = (url, opts) => url.endsWith('/messages') && opts?.method === 'POST'
      ? messageFixture({ id: fixtureId(70), channel_id: ch.id, user_id: me.id, content: 'old', created_at: '2026-01-15T00:00:00Z' })
      : []
    chat.retryPending(required(chat.outbox[0]).nonce)
    await tick()
    expect(chat.readStates[ch.id]).toMatchObject({ last_read_at: '2026-02-01T00:00:00Z', unread_count: 3 })
  })

  it('moves the marker for a fresh message sent from the outbox even when the marker carries this device\'s clock', async () => {
    const chat = setup()
    await chat.sendMessage('later', null, null, [], ch.id)
    chat.readStates = { [ch.id]: { ...lastRead, last_read_at: '2026-03-01T00:00:00Z' } }
    net.down = false
    answerWithOld()
    chat.retryPending(required(chat.outbox[0]).nonce)
    await tick()
    expect(chat.readStates[ch.id]).toMatchObject({ last_read_at: '2026-01-01T00:00:00Z', unread_count: 0, mention_count: 0 })
  })

  it('still follows a fresh message whose server time is behind this device clock', async () => {
    const chat = setup()
    chat.readStates = { [ch.id]: { ...lastRead } }
    net.down = false
    answerWithOld()
    await chat.sendMessage('fresh', null, null, [], ch.id)
    expect(chat.readStates[ch.id]).toMatchObject({ last_read_at: '2026-01-01T00:00:00Z', unread_count: 0, mention_count: 0 })
  })
})

describe('another tab\'s offline message', () => {
  it('this tab follows the other tab\'s refusal and does not send it', async () => {
    const chat = setup()
    await connectAgain(chat)
    net.posted = []
    const key = 'mnema.outbox.' + me.id + '.n-shared'
    const base = { nonce: 'n-shared', channelId: ch.id, userId: me.id, content: 'shared', parentId: null, replyToId: null, createdAt: new Date().toISOString(), error: '', cause: 'offline', attempts: 0, possiblyDelivered: false, notBefore: 0 }
    const sending = JSON.stringify({ ...base, status: 'sending', owner: 'tab-A', sendingSince: Date.now() })
    localStorage.setItem(key, sending); storageEvent(key, sending)
    await tick()
    const failed = JSON.stringify({ ...base, status: 'failed', error: 'missing permission', attempts: 1 })
    localStorage.setItem(key, failed); storageEvent(key, failed)
    await tick()
    const afterFail = required(chat.outbox[0]).status
    await stayConnected(chat, 60000)
    expect({ afterFail, posted: net.posted.length, now: required(chat.outbox[0] ?? { status: 'gone' }).status, stored: JSON.parse(localStorage.getItem(key) ?? '{}').status }).toEqual({ afterFail: 'failed', posted: 0, now: 'failed', stored: 'failed' })
  })
})
