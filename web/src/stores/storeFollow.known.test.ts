// @vitest-environment node
// Known failure found by the store trace (storeTrace.test.ts, "clean
// reply-follow"), shrunk to its smallest case. It runs, must fail, and turns
// red on the day it passes: then move it to a normal test.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useChatStore } from './chat'
import { useAuthStore } from './auth'
import { setLocale } from '../i18n'
import { fixtureId, memoryStorageFixture, messageFixture, userFixture } from '../test-fixtures.fixture'

const created: string[] = []
class HeadlessNotification {
  static permission: NotificationPermission = 'granted'
  onclick: (() => void) | null = null
  close = vi.fn()
  constructor(public title: string) { created.push(title) }
}

const ME = fixtureId(1)
const BOB = fixtureId(9)
const ROOT = fixtureId(50) // a message of mine

beforeEach(() => {
  setActivePinia(createPinia())
  vi.stubGlobal('localStorage', memoryStorageFixture())
  vi.stubGlobal('Notification', HeadlessNotification)
  setLocale('en')
  created.length = 0
  useAuthStore().user = userFixture({ id: ME, username: 'max' })
})
afterEach(() => { useChatStore().closeWebSocket(); vi.unstubAllGlobals() })

// H-L5-05: the first reply under a message makes its author follow the thread
// in the reply's own transaction (internal/chat/messages/thread_follow.go,
// followOnReply), so the push goes to the author (push/recipients.go,
// threadAudience). Open clients learn of the follow from the follow tap,
// which runs after the reply was published (thread_follow_sync.go): the reply
// arrives first, and the in-page notification and sound decide it from a list
// without the thread. doc/architecture.md: "a reply notifies only the thread's
// followers (push and in-page alike)".
describe('the first reply under my own message', () => {
  it('sanity: a later reply, once the follow is known, notifies', () => {
    const chat = useChatStore()
    chat.followKnown = true
    chat.handleWSEvent({ type: 'thread_follow_updated', payload: { thread_id: ROOT, channel_id: fixtureId(2), following: true } })
    chat.handleWSEvent({ type: 'message_create', payload: messageFixture({ id: fixtureId(61), user_id: BOB, username: 'bob', parent_id: ROOT, content: 'second' }) })
    expect(created).toHaveLength(1)
  })

  it.fails('notifies in the page as the push does, in the order the server sends the two frames', () => {
    const chat = useChatStore()
    chat.followKnown = true
    chat.handleWSEvent({ type: 'message_create', payload: messageFixture({ id: fixtureId(60), user_id: BOB, username: 'bob', parent_id: ROOT, content: 'first' }) })
    chat.handleWSEvent({ type: 'thread_follow_updated', payload: { thread_id: ROOT, channel_id: fixtureId(2), following: true } })
    expect(created).toHaveLength(1)
  })
})
