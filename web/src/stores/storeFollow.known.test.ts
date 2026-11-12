// @vitest-environment node
// A failure found by the store trace (storeTrace.test.ts, "clean
// reply-follow"), shrunk to its smallest case; fixed, so a normal test now.
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

// H-L5-05 (fixed): the first reply under a message makes its author follow
// the thread in the reply's own transaction (internal/chat/messages/thread_follow.go,
// followOnReply), so the push goes to the author (push/recipients.go,
// threadAudience). Open clients learn of the follow from the follow tap
// (thread_follow_sync.go), which now runs before the hub hands the reply to
// any connection: thread_follow_updated arrives first, and the in-page
// notification and sound decide the reply from a list with the thread.
// doc/architecture.md: "a reply notifies only the thread's followers (push
// and in-page alike)".
describe('the first reply under my own message', () => {
  it('sanity: a later reply, once the follow is known, notifies', () => {
    const chat = useChatStore()
    chat.followKnown = true
    chat.handleWSEvent({ type: 'thread_follow_updated', payload: { thread_id: ROOT, channel_id: fixtureId(2), following: true } })
    chat.handleWSEvent({ type: 'message_create', payload: messageFixture({ id: fixtureId(61), user_id: BOB, username: 'bob', parent_id: ROOT, content: 'second' }) })
    expect(created).toHaveLength(1)
  })

  it('notifies in the page as the push does, in the order the server sends the two frames', () => {
    const chat = useChatStore()
    chat.followKnown = true
    chat.handleWSEvent({ type: 'thread_follow_updated', payload: { thread_id: ROOT, channel_id: fixtureId(2), following: true } })
    chat.handleWSEvent({ type: 'message_create', payload: messageFixture({ id: fixtureId(60), user_id: BOB, username: 'bob', parent_id: ROOT, content: 'first' }) })
    expect(created).toHaveLength(1)
  })
})
