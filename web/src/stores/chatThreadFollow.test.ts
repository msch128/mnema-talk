// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useChatStore } from './chat'
import { useAuthStore } from './auth'
import { setLocale } from '../i18n'
import { fixtureId, memoryStorageFixture, messageFixture, userFixture } from '../test-fixtures.fixture'
import { notifyShowsText } from '../lib/notifyPreview'
import { ApiError } from '../lib/api'
import { useToastStore } from './toast'

const mockApi = vi.fn()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: unknown[]) => mockApi(...args)
}))

const created: { title: string; options: NotificationOptions }[] = []
class HeadlessNotification {
  static permission: NotificationPermission = 'granted'
  onclick: (() => void) | null = null
  close = vi.fn()
  constructor(public title: string, public options: NotificationOptions) { created.push(this) }
}

const ROOT = fixtureId(50)
const me = fixtureId(1)

beforeEach(() => {
  setActivePinia(createPinia())
  vi.stubGlobal('localStorage', memoryStorageFixture())
  vi.stubGlobal('Notification', HeadlessNotification)
  vi.useFakeTimers()
  setLocale('en')
  created.length = 0
  mockApi.mockReset()
  notifyShowsText.value = true
  useAuthStore().user = userFixture({ id: me, username: 'me' })
})
afterEach(() => { useChatStore().closeWebSocket(); vi.clearAllTimers(); vi.useRealTimers(); vi.unstubAllGlobals() })

function reply(extra: Record<string, unknown> = {}) {
  return messageFixture({ id: fixtureId(60), user_id: fixtureId(9), parent_id: ROOT, content: 'a reply', ...extra })
}

describe('thread replies and the in-page notification', () => {
  it('notify only followers once the server has told which threads are followed', async () => {
    const chat = useChatStore()
    chat.followKnown = true
    chat.handleWSEvent({ type: 'message_create', payload: reply() })
    expect(created).toHaveLength(0)

    chat.handleWSEvent({ type: 'thread_follow_updated', payload: { thread_id: ROOT, channel_id: fixtureId(2), following: true } })
    chat.handleWSEvent({ type: 'message_create', payload: reply({ id: fixtureId(61) }) })
    expect(created).toHaveLength(1)

    chat.handleWSEvent({ type: 'thread_follow_updated', payload: { thread_id: ROOT, channel_id: fixtureId(2), following: false } })
    chat.handleWSEvent({ type: 'message_create', payload: reply({ id: fixtureId(62) }) })
    expect(created).toHaveLength(1)
  })

  it('still notify without a list (older server), and reach a member addressed by name', () => {
    const chat = useChatStore()
    chat.handleWSEvent({ type: 'message_create', payload: reply() })
    expect(created).toHaveLength(1)

    chat.followKnown = true
    chat.handleWSEvent({ type: 'message_create', payload: reply({ id: fixtureId(63), content: 'hello @me' }) })
    expect(created).toHaveLength(2)
  })

  it('show no message text when the setting or the operator turned it off', () => {
    const chat = useChatStore()
    chat.handleWSEvent({ type: 'message_create', payload: messageFixture({ id: fixtureId(64), user_id: fixtureId(9), content: 'secret words' }) })
    expect(created[0]?.options.body).toBe('secret words')
    notifyShowsText.value = false
    chat.handleWSEvent({ type: 'message_create', payload: messageFixture({ id: fixtureId(65), user_id: fixtureId(9), content: 'secret words' }) })
    expect(created[1]?.options.body).toBe('New message')
  })
})

describe('following a thread', () => {
  it('is optimistic, kept from the server answer and undone when the server refuses', async () => {
    const chat = useChatStore()
    mockApi.mockResolvedValueOnce({ thread_id: ROOT, following: true })
    await chat.setThreadFollow(ROOT, true)
    expect(chat.followedThreads.has(ROOT)).toBe(true)
    expect(mockApi).toHaveBeenCalledWith(`/api/threads/${ROOT}/follow`, expect.objectContaining({ method: 'PUT' }))

    mockApi.mockRejectedValueOnce(new Error('offline'))
    await chat.setThreadFollow(ROOT, false)
    expect(chat.followedThreads.has(ROOT)).toBe(true)

    mockApi.mockResolvedValueOnce({ thread_id: ROOT, following: false })
    await chat.setThreadFollow(ROOT, false)
    expect(chat.followedThreads.has(ROOT)).toBe(false)
  })
})

describe('the followed list', () => {
  it('is read from the server, marks the list as known, and is not trusted from a server without the route', async () => {
    const chat = useChatStore()
    mockApi.mockResolvedValueOnce({ thread_ids: [ROOT] })
    await chat.fetchFollowedThreads()
    expect(chat.followKnown).toBe(true)
    expect(chat.followedThreads.has(ROOT)).toBe(true)

    mockApi.mockRejectedValueOnce(new ApiError(404, 'NOT_FOUND', 'not found'))
    await chat.fetchFollowedThreads()
    expect(chat.followKnown).toBe(false)
  })

  it('keeps what it knew when the fetch fails for another reason', async () => {
    const chat = useChatStore()
    mockApi.mockResolvedValueOnce({ thread_ids: [ROOT] })
    await chat.fetchFollowedThreads()
    mockApi.mockRejectedValueOnce(new ApiError(503, 'UNAVAILABLE', 'down'))
    await chat.fetchFollowedThreads()
    mockApi.mockRejectedValueOnce(new TypeError('network'))
    await chat.fetchFollowedThreads()
    expect(chat.followKnown).toBe(true)
    expect(chat.followedThreads.has(ROOT)).toBe(true)
  })

  it('ignores a failure that arrives after the community changed', async () => {
    const chat = useChatStore()
    let fail: (e: unknown) => void = () => {}
    mockApi.mockReturnValueOnce(new Promise((_, reject) => { fail = reject }))
    const pending = chat.fetchFollowedThreads()
    chat.resetCommunityState()
    chat.followKnown = true
    fail(new ApiError(404, 'NOT_FOUND', 'not found'))
    await pending
    expect(chat.followKnown).toBe(true)
  })

  it('takes the state of an opened thread, and ignores a server that does not send it', async () => {
    const chat = useChatStore()
    mockApi.mockResolvedValueOnce({ root: messageFixture({ id: ROOT }), replies: [], following: true })
    await chat.openThread(ROOT)
    expect(chat.followedThreads.has(ROOT)).toBe(true)

    mockApi.mockResolvedValueOnce({ root: messageFixture({ id: ROOT }), replies: [] })
    await chat.openThread(ROOT)
    expect(chat.followedThreads.has(ROOT)).toBe(true)
    chat.handleWSEvent({ type: 'thread_follow_updated', payload: null as never })
  })
})


describe('opening a thread', () => {
  it('keeps a reply that arrived live while the thread was loading, in order', async () => {
    const chat = useChatStore()
    const early = reply({ id: fixtureId(70), created_at: '2026-01-01T10:00:00Z' })
    const live = reply({ id: fixtureId(71), created_at: '2026-01-01T10:05:00Z' })
    const between = reply({ id: fixtureId(72), created_at: '2026-01-01T10:02:00Z' })
    let answer: (v: unknown) => void = () => {}
    mockApi.mockReturnValueOnce(new Promise(resolve => { answer = resolve }))
    const opening = chat.openThread(ROOT)
    chat.handleWSEvent({ type: 'message_create', payload: live })
    chat.handleWSEvent({ type: 'message_create', payload: between })
    answer({ root: messageFixture({ id: ROOT }), replies: [early, between] })
    await opening
    expect(chat.threadReplies.map(m => m.id)).toEqual([early.id, between.id, live.id])
  })

  it('tells the member when the thread cannot be loaded', async () => {
    const chat = useChatStore()
    vi.spyOn(console, 'error').mockImplementation(() => {})
    mockApi.mockRejectedValueOnce(new ApiError(404, 'NOT_FOUND', 'gone'))
    await chat.openThread(ROOT)
    expect(useToastStore().toasts.map(toast => toast.text)).toEqual(['Couldn’t load the thread. Try again.'])
    expect(chat.isThreadLoading).toBe(false)
  })
})
