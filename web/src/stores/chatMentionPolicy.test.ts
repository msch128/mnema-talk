// One rule, two implementations: who a message mentions. The server resolves
// it under the sender's mention policy (internal/chat/mentions: a webhook with
// user mentions off, a bot's allowed_mentions, the recipient cap) and sends the
// result as `mentions`. In a muted channel the client asked its own regular
// expression on the raw text instead (messageMentionsMeDirectly), so a message
// the server decided notifies nobody still broke the mute (finding H-L3-05 of
// the 2026-11 hunt). The live badge must be the one a reload brings back.
import { messageFixture, readStateFixture, userFixture, categoryFixture, channelFixture } from '../test-fixtures.fixture'
import { required } from '../store-test-support.fixture'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useChatStore } from './chat'
import { useAuthStore } from './auth'

// A channel id the read-state decoder accepts (a UUID).
const BUILDS = '00000000-0000-4000-8000-000000000002'
let made: unknown[]
class NotificationFixture {
  static permission: NotificationPermission = 'granted'
  static requestPermission = vi.fn(async (): Promise<NotificationPermission> => 'granted')
  close = vi.fn()
  onclick: (() => void) | null = null
  constructor() { made.push(this) }
}

function setup() {
  const chat = useChatStore()
  useAuthStore().user = userFixture({ id: 'me', username: 'max', role: 'user' })
  chat.categories = [categoryFixture({ id: 'c', channels: [channelFixture({ id: 'ch1', name: 'lobby', type: 'text' }), channelFixture({ id: BUILDS, name: 'builds', type: 'text' })] })]
  chat.activeChannel = channelFixture({ id: 'ch1', type: 'text' })
  chat.messages = []
  chat.readStates = { ...chat.readStates, [BUILDS]: readStateFixture({ channel_id: BUILDS, notify_level: 'mute', unread_count: 0, mention_count: 0 }) }
  return chat
}

// What the server sends for "@max" from a webhook whose user mentions are off
// (internal/webhook/execute_integration_test.go: "allow_user_mentions off"):
// the text names max, the resolved list is empty.
const unresolved = (content: string) => messageFixture({ id: 'm' + Math.random(), channel_id: BUILDS, user_id: 'hook', content, mentions: [], created_at: new Date().toISOString() })

beforeEach(() => {
  vi.useFakeTimers()
  setActivePinia(createPinia())
  vi.stubGlobal('fetch', vi.fn(async () => new Response(null, { status: 204 })))
  made = []
  vi.stubGlobal('Notification', NotificationFixture)
})

afterEach(() => {
  useChatStore().closeWebSocket()
  vi.clearAllTimers()
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('a muted channel follows the mentions the server resolved', () => {
  it('a name the server resolved does break the mute (control)', () => {
    const chat = setup()
    chat.handleWSEvent({ type: 'message_create', payload: { ...unresolved('hey @max'), mentions: ['me'] } })
    expect(required(chat.readStates[BUILDS]).mention_count).toBe(1)
    expect(made).toHaveLength(1)
  })

  it('a name the server did not resolve adds no mention', () => {
    const chat = setup()
    chat.handleWSEvent({ type: 'message_create', payload: unresolved('build failed, @max') })
    expect(required(chat.readStates[BUILDS]).mention_count).toBe(0)
  })

  it('a name the server did not resolve raises no desktop notification', () => {
    setup().handleWSEvent({ type: 'message_create', payload: unresolved('build failed, @max') })
    expect(made).toHaveLength(0)
  })

  it('the live badge is the one a reload brings back', async () => {
    const chat = setup()
    chat.handleWSEvent({ type: 'message_create', payload: unresolved('build failed, @max') })
    chat.handleWSEvent({ type: 'message_create', payload: { ...unresolved('hey @max'), mentions: ['me'] } })
    const live = required(chat.readStates[BUILDS]).mention_count
    // GET /api/read-state counts a muted channel from message_mentions
    // (internal/chat/readstate): only the resolved mention has a row.
    vi.stubGlobal('fetch', vi.fn(async () => Response.json([{ channel_id: BUILDS, unread_count: 2, mention_count: 1, last_read_at: null, notify_level: 'mute' }])))
    await chat.fetchReadState()
    expect(required(chat.readStates[BUILDS]).unread_count).toBe(2)
    expect(required(chat.readStates[BUILDS]).mention_count).toBe(live)
  })

  it('@all alone does not break the mute even though it puts me on the list', () => {
    const chat = setup()
    chat.handleWSEvent({ type: 'message_create', payload: { ...unresolved('@all deploy at 5'), mentions: ['me'] } })
    expect(required(chat.readStates[BUILDS]).mention_count).toBe(0)
    expect(made).toHaveLength(0)
  })

  it('a reply to me breaks the mute whatever the list says', () => {
    const chat = setup()
    chat.handleWSEvent({ type: 'message_create', payload: { ...unresolved('agreed'), reply_to: { id: 'r', user_id: 'me', username: 'max', display_name: 'Max', content: 'x' } } })
    expect(required(chat.readStates[BUILDS]).mention_count).toBe(1)
  })
})
