// Known failures found by the store trace (storeTrace.test.ts), shrunk to
// their smallest case: what an open client shows after a reconnect or when an
// answer and an event cross, against what the server holds. Each runs, must
// fail, and turns red on the day it passes: then move it to a normal test.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useAuthStore } from './auth'
import { useChatStore } from './chat'
import { useLaterStore } from './later'
import { useSavedStore } from './saved'
import { fixtureId, FIXTURE_TIMESTAMP, messageFixture, userFixture } from '../test-fixtures.fixture'

const ME = fixtureId(1)
const BOB = fixtureId(9)
const LOBBY = fixtureId(2)
const X = fixtureId(3)

class Socket {
  static instances: Socket[] = []
  onopen: (() => void) | null = null
  onclose: (() => void) | null = null
  onmessage: ((event: { data: string }) => void) | null = null
  send = vi.fn<(data: string) => void>()
  close = vi.fn()
  constructor() { Socket.instances.push(this) }
}

const channel = (id: string, name: string) => ({
  id, number: 1, category_id: null, name, type: 'text', topic: '', sort_order: 0, created_at: FIXTURE_TIMESTAMP,
  user_limit: 0, slow_mode_seconds: 0, slug: name, previous_slugs: []
})
const reminder = { id: fixtureId(40), remind_at: FIXTURE_TIMESTAMP, created_at: FIXTURE_TIMESTAMP, delivered_at: null, available: true,
  message: { id: fixtureId(41), channel_id: X, parent_id: null, channel_name: 'x', dm: false, author: 'bob', snippet: 'call mum' } }
const savedEntry = { message_id: fixtureId(41), note: '', saved_at: FIXTURE_TIMESTAMP, available: true,
  message: messageFixture({ id: fixtureId(41), channel_id: X, user_id: BOB }) }

/** What the server answers now; a test changes it as the world changes. */
let server: Record<string, unknown>
/** Requests held back: the answer is computed when asked, and delivered when the test says so. */
let held: { path: string; release: () => void }[]
let holding = false

function answer(path: string) {
  return new Response(JSON.stringify(server[path] ?? { error: { code: 'NOT_FOUND' } }), { status: path in server ? 200 : 404 })
}

const settle = async () => { for (let i = 0; i < 6; i++) await new Promise(r => setImmediate(r)) }

beforeEach(() => {
  setActivePinia(createPinia())
  Socket.instances = []
  held = []
  holding = false
  server = {
    '/api/channels': { categories: [], uncategorized: [channel(LOBBY, 'lobby'), channel(X, 'x')] },
    '/api/read-state': [], '/api/threads/following': { thread_ids: [] }, '/api/members': [],
    '/api/me/reminders': { items: [] }, '/api/me/scheduled': { items: [] }, '/api/me/saved': { items: [], has_more: false }
  }
  vi.stubGlobal('WebSocket', Socket)
  vi.stubGlobal('fetch', vi.fn(async (input: unknown) => {
    const path = String(input).split('?')[0] ?? ''
    const now = answer(path)
    if (!holding || path !== '/api/read-state') return now
    return new Promise<Response>(resolve => { held.push({ path, release: () => resolve(now) }) })
  }))
  const auth = useAuthStore()
  auth.user = userFixture({ id: ME, username: 'max' })
  vi.spyOn(auth, 'checkAuth').mockResolvedValue(true)
  vi.spyOn(console, 'warn').mockImplementation(() => undefined)
  vi.spyOn(console, 'error').mockImplementation(() => undefined)
})
afterEach(() => { useChatStore().closeWebSocket(); vi.restoreAllMocks(); vi.unstubAllGlobals() })

/** Opens the socket (first time or after a drop) and lets the resync run. */
async function connect(first: boolean) {
  const chat = useChatStore()
  if (first) chat.initWebSocket()
  else chat.retryNow()
  await settle()
  Socket.instances.at(-1)?.onopen?.()
  await settle()
}
const frame = (type: string, payload: unknown) => Socket.instances.at(-1)?.onmessage?.({ data: JSON.stringify({ type, payload }) })

/** The header's panel loads its lists when it mounts (SavedLaterPanel.vue), then the socket drops. */
async function mountedThenDropped() {
  await connect(true)
  void useSavedStore().ensureLoaded()
  void useLaterStore().load({ reminders: true, scheduled: true })
  await settle()
  Socket.instances.at(-1)?.onclose?.()
  await settle()
}

// H-L5-02: the resync after a reconnect (stores/chat/socket.ts resyncState)
// reads channels, read state, followed threads and more, but not the saved
// list or the reminders and scheduled messages; their events from the time
// the socket was down are lost.
describe('lists the member keeps, after a reconnect', () => {
  it.fails('a reminder made in another session while the socket was down is listed', async () => {
    await mountedThenDropped()
    server['/api/me/reminders'] = { items: [reminder] }
    await connect(false)
    expect(useLaterStore().reminders.map(r => r.id)).toEqual([reminder.id])
  })

  it.fails('a reminder that fell due while the socket was down counts as due', async () => {
    server['/api/me/reminders'] = { items: [reminder] }
    await mountedThenDropped()
    server['/api/me/reminders'] = { items: [{ ...reminder, delivered_at: FIXTURE_TIMESTAMP }] }
    await connect(false)
    expect(useLaterStore().dueCount).toBe(1)
  })

  it.fails('a message saved in another session while the socket was down is listed', async () => {
    await mountedThenDropped()
    server['/api/me/saved'] = { items: [savedEntry], has_more: false }
    await connect(false)
    expect(useSavedStore().items.map(e => e.message_id)).toEqual([savedEntry.message_id])
  })
})

// H-L5-03: the server forgets a saved message when the message is deleted;
// the open client keeps showing it in the saved list.
describe('saved list against a deletion', () => {
  it.fails('a saved message that is deleted leaves the list', async () => {
    server['/api/me/saved'] = { items: [savedEntry], has_more: false }
    await connect(true)
    await useSavedStore().ensureLoaded()
    frame('message_delete', { id: savedEntry.message_id, channel_id: X, parent_id: null })
    await settle()
    expect(useSavedStore().items).toEqual([])
  })
})

// H-L5-04 (fixed): the read state read at a (re)connect and the live message
// events cross. The answer says per channel the number of the newest message
// its counts include (last_message_number; the server stores a channel's
// messages in the order of their numbers), so the client counts a message on
// top of the answer when its number is above that mark and not again when it
// is not, in either order.
describe('unread counters when the resync answer and a message cross', () => {
  const message = messageFixture({ id: fixtureId(50), number: 3, channel_id: X, user_id: BOB, username: 'bob', content: 'hello' })
  const state = (unread: number, newest: number) =>
    [{ channel_id: X, unread_count: unread, mention_count: 0, last_read_at: null, notify_level: 'all', last_message_number: newest }]

  it('sanity: in order, the message counts once', async () => {
    await connect(true)
    frame('message_create', message)
    await settle()
    expect(useChatStore().readStates[X]?.unread_count).toBe(1)
  })

  it('an answer from before the message, delivered after its event, keeps the message counted', async () => {
    holding = true
    server['/api/read-state'] = state(0, 2) // read before message 3 was stored, and held back
    await connect(true)
    frame('message_create', message)
    await settle()
    for (const h of held) h.release()
    await settle()
    expect(useChatStore().readStates[X]?.unread_count).toBe(1)
  })

  it('an answer from after the message, delivered before its event, counts it once', async () => {
    server['/api/read-state'] = state(1, 3) // the server read the state after the message was stored
    await connect(true)
    frame('message_create', message)
    await settle()
    expect(useChatStore().readStates[X]?.unread_count).toBe(1)
  })

  it('two answers on their way and a reconnect between them: the newest answer counts, with what came after it', async () => {
    holding = true
    server['/api/read-state'] = state(0, 2)
    await connect(true) // first answer, read before message 3
    Socket.instances.at(-1)?.onclose?.()
    await settle()
    server['/api/read-state'] = state(1, 3) // second answer, read after message 3 (whose event was lost)
    await connect(false)
    frame('message_create', { ...message, id: fixtureId(51), number: 4 })
    await settle()
    for (const h of held.reverse()) h.release() // the newer answer lands first, the older one last
    await settle()
    expect(useChatStore().readStates[X]?.unread_count).toBe(2)
  })
})
