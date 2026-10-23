// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useChatStore } from './chat'
import { useAuthStore } from './auth'
import { setLocale } from '../i18n'
import { fixtureId, memoryStorageFixture, messageFixture, userFixture } from '../test-fixtures.fixture'
import { notifyShowsText } from '../lib/notifyPreview'

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

    mockApi.mockRejectedValueOnce(new Error('not found'))
    await chat.fetchFollowedThreads()
    expect(chat.followKnown).toBe(false)
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

