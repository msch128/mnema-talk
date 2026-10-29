import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useChatStore } from './chat'
import { useAuthStore } from './auth'
import { useServerFeaturesStore } from './serverFeatures'
import { channelFixture, fixtureId, messageFixture, userFixture } from '../test-fixtures.fixture'
import { dmFixture, dmMemberFixture } from '../dm.fixture'
import { createChatState, type Cx } from './chat/state'
import { useVoiceStore } from './voice'
import { saveDmSettings } from './chat/dmPrefs'
import { insertMessage, refreshLatest } from './chat/messageWindow'
import type { ApiOptions } from '../lib/api'
import { api } from '../lib/api'

const http = vi.hoisted(() => ({ handle: (_url: string, _opts?: ApiOptions): unknown => [] }))
vi.mock('../lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: vi.fn((url: string, options?: ApiOptions) => Promise.resolve(http.handle(url, options)))
}))

function deferred<T>() {
  let resolve: (value: T) => void = () => { throw new Error('Uninitialized deferred') }
  let reject: (reason: unknown) => void = () => { throw new Error('Uninitialized deferred') }
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
const ch = channelFixture({ id: fixtureId(2) })
const me = userFixture({ id: fixtureId(1), presence: 'online' })
const msg = (n: number) => messageFixture({ id: fixtureId(40 + n), channel_id: ch.id, user_id: fixtureId(5) })
const posts = (suffix: string) => vi.mocked(api).mock.calls.filter(([url, o]) => String(url).endsWith(suffix) && o?.method === 'POST')

function setup() {
  const chat = useChatStore()
  useAuthStore().user = { ...me }
  chat.activeChannel = { ...ch }
  return chat
}

beforeEach(() => {
  setActivePinia(createPinia())
  http.handle = () => []
  vi.mocked(api).mockClear()
  vi.spyOn(console, 'error').mockImplementation(() => {})
  vi.spyOn(console, 'warn').mockImplementation(() => {})
})
afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals() })

describe('channel selection', () => {
  it('does not mark a channel read when its messages failed to load', async () => {
    const chat = setup()
    http.handle = () => Promise.reject(new Error('offline'))
    await chat.selectChannel(ch)
    expect(posts(`/channels/${ch.id}/read`)).toHaveLength(0)
  })

  it('does not mark a channel read after the reader moved on before its page arrived', async () => {
    const chat = setup()
    const other = channelFixture({ id: fixtureId(3) })
    const wait = deferred<unknown>()
    http.handle = url => url.includes(`/channels/${ch.id}/messages`) ? wait.promise : []
    const first = chat.selectChannel(ch)
    await chat.selectChannel(other)
    wait.resolve([msg(1)])
    await first
    expect(posts(`/channels/${ch.id}/read`)).toHaveLength(0)
  })

  it('keeps the visible messages when a refresh fails', async () => {
    const chat = setup()
    http.handle = () => [msg(1)]
    await chat.fetchMessages(ch.id)
    http.handle = () => Promise.reject(new Error('502'))
    expect(await chat.fetchMessages(ch.id)).toBe(false)
    expect(chat.messages).toHaveLength(1)
  })

  it('keeps live messages held during a failed jump', async () => {
    const chat = setup()
    http.handle = () => [msg(1)]
    await chat.fetchMessages(ch.id)
    const wait = deferred<unknown>()
    http.handle = () => wait.promise
    const jump = chat.jumpToMessage(fixtureId(99))
    chat.handleWSEvent({ type: 'message_create', payload: msg(2) })
    wait.reject({ status: 404 })
    expect(await jump).toBe(false)
    expect(chat.messages.map(m => m.id)).toEqual([msg(1).id, msg(2).id])
  })
})

describe('reconnect refresh', () => {
  it('adds the missed messages and keeps the older history in place', async () => {
    setup()
    const cx: Cx = { cs: createChatState(() => {}), authStore: useAuthStore(), voiceStore: useVoiceStore() }
    cx.cs.activeChannel.value = { ...ch }
    cx.cs.messages.value = [msg(1), msg(2)]
    cx.cs.hasMoreBefore.value = true
    const seq = cx.cs.latestLoadSeq.value
    http.handle = () => [msg(2), msg(3)]
    expect(await refreshLatest(cx, ch.id)).toBe(true)
    expect(cx.cs.messages.value.map(m => m.id)).toEqual([msg(1).id, msg(2).id, msg(3).id])
    expect(cx.cs.hasMoreBefore.value).toBe(true)
    expect(cx.cs.latestLoadSeq.value).toBe(seq)
  })

  it('keeps a live message that arrives while the page is in flight', async () => {
    const cx: Cx = { cs: createChatState(() => {}), authStore: useAuthStore(), voiceStore: useVoiceStore() }
    cx.cs.activeChannel.value = { ...ch }
    cx.cs.messages.value = [msg(1), msg(2)]
    const wait = deferred<unknown>()
    http.handle = () => wait.promise
    const refresh = refreshLatest(cx, ch.id)
    insertMessage(cx, msg(4))
    wait.resolve([msg(2), msg(3)])
    expect(await refresh).toBe(true)
    expect(cx.cs.messages.value.map(m => m.id)).toEqual([msg(1).id, msg(2).id, msg(3).id, msg(4).id])
    expect(cx.cs.isLoadingWindow.value).toBe(false)
  })

  it('keeps holding live messages until the last of two overlapping refreshes ends', async () => {
    const cx: Cx = { cs: createChatState(() => {}), authStore: useAuthStore(), voiceStore: useVoiceStore() }
    cx.cs.activeChannel.value = { ...ch }
    cx.cs.messages.value = [msg(1), msg(2)]
    const first = deferred<unknown>()
    const second = deferred<unknown>()
    const pages = [first.promise, second.promise]
    http.handle = () => pages.shift()
    const a = refreshLatest(cx, ch.id)
    const b = refreshLatest(cx, ch.id)
    first.resolve([msg(2)])
    expect(await a).toBe(true)
    expect(cx.cs.isLoadingWindow.value).toBe(true)
    insertMessage(cx, msg(4))
    second.resolve([msg(2), msg(3)])
    expect(await b).toBe(true)
    expect(cx.cs.messages.value.map(m => m.id)).toEqual([msg(1).id, msg(2).id, msg(3).id, msg(4).id])
    expect(cx.cs.isLoadingWindow.value).toBe(false)
  })

  it('joins a live message held during a refresh that failed', async () => {
    const cx: Cx = { cs: createChatState(() => {}), authStore: useAuthStore(), voiceStore: useVoiceStore() }
    cx.cs.activeChannel.value = { ...ch }
    cx.cs.messages.value = [msg(1)]
    const wait = deferred<unknown>()
    http.handle = () => wait.promise
    const refresh = refreshLatest(cx, ch.id)
    insertMessage(cx, msg(2))
    wait.reject(new Error('502'))
    expect(await refresh).toBe(false)
    expect(cx.cs.messages.value.map(m => m.id)).toEqual([msg(1).id, msg(2).id])
  })

  it('leaves the window alone when the refresh fails', async () => {
    const cx: Cx = { cs: createChatState(() => {}), authStore: useAuthStore(), voiceStore: useVoiceStore() }
    cx.cs.activeChannel.value = { ...ch }
    cx.cs.messages.value = [msg(1)]
    http.handle = () => Promise.reject(new Error('502'))
    expect(await refreshLatest(cx, ch.id)).toBe(false)
    expect(cx.cs.messages.value).toHaveLength(1)
  })
})

describe('direct message list', () => {
  const dm = dmFixture({ id: fixtureId(100), members: [dmMemberFixture(1), dmMemberFixture(2)] })

  it('does not count a failed list read as loaded', async () => {
    const chat = setup()
    useServerFeaturesStore().list = ['dm']
    http.handle = () => Promise.reject(new Error('502'))
    await chat.fetchDms()
    expect(chat.dmsLoaded).toBe(false)
  })

  it('takes the receiver own counts from the row read after dm_created', async () => {
    const chat = setup()
    useServerFeaturesStore().list = ['dm']
    http.handle = url => url === `/api/dms/${dm.id}` ? { ...dm, unread_count: 4, notify_level: 'mentions' } : []
    chat.handleWSEvent({ type: 'dm_created', payload: { ...dm, unread_count: 0 } })
    await vi.waitFor(() => expect(chat.readStates[dm.id]).toMatchObject({ unread_count: 4, notify_level: 'mentions' }))
  })
})

describe('direct message settings', () => {
  function cxOf(): Cx {
    return { cs: createChatState(() => {}), authStore: useAuthStore(), voiceStore: useVoiceStore() }
  }

  it('rolls back to the confirmed value, not to the other save', async () => {
    const cx = cxOf()
    const first = deferred<unknown>()
    const second = deferred<unknown>()
    const answers = [first, second]
    http.handle = (url, o) => url === '/api/users/me/dm-settings' && o?.method === 'PUT' ? answers.shift()?.promise : []
    const a = saveDmSettings(cx, { mute_all: true })
    const b = saveDmSettings(cx, { list_collapsed: true })
    expect(cx.cs.dmSettings.value).toEqual({ mute_all: true, list_collapsed: true })
    first.reject(new Error('refused'))
    await expect(a).rejects.toThrow('refused')
    // The second change is still in flight: the screen keeps showing it.
    expect(cx.cs.dmSettings.value.list_collapsed).toBe(true)
    second.resolve({ mute_all: false, list_collapsed: true })
    await b
    expect(cx.cs.dmSettings.value).toEqual({ mute_all: false, list_collapsed: true })
  })

  it('sends one save at a time and ends on the last answer', async () => {
    const cx = cxOf()
    const sent: unknown[] = []
    const first = deferred<unknown>()
    http.handle = (url, o) => {
      if (url !== '/api/users/me/dm-settings' || o?.method !== 'PUT') return []
      sent.push(o.json)
      return sent.length === 1 ? first.promise : { mute_all: false, list_collapsed: true }
    }
    const a = saveDmSettings(cx, { list_collapsed: false })
    const b = saveDmSettings(cx, { list_collapsed: true })
    await Promise.resolve()
    expect(sent).toHaveLength(1)
    first.resolve({ mute_all: false, list_collapsed: false })
    await Promise.all([a, b])
    expect(sent).toHaveLength(2)
    expect(cx.cs.dmSettings.value.list_collapsed).toBe(true)
  })
})

describe('presence', () => {
  it('restores the member list entry when the change is refused', async () => {
    const chat = setup()
    chat.presenceById = { [me.id]: 'online' }
    http.handle = () => Promise.reject(new Error('offline'))
    await expect(chat.setMyPresence('dnd')).rejects.toThrow('offline')
    expect(chat.presenceById[me.id]).toBe('online')
  })
})

describe('community reset', () => {
  it('forgets the media handshake and forum tag handlers', () => {
    const chat = setup()
    const onMedia = vi.fn()
    chat.setWebRTCHandlers({ onOffer: vi.fn(), onCandidate: vi.fn(), onMedia })
    chat.resetCommunityState()
    chat.handleWSEvent({ type: 'media_handshake', payload: { channel_id: ch.id } as never })
    expect(onMedia).not.toHaveBeenCalled()
  })
})
