import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useChatStore, TYPING_EXPIRY_MS } from './chat'
import { useAuthStore } from './auth'
import { channelFixture, messageFixture, userFixture, readStateFixture, fixtureId, FIXTURE_TIMESTAMP } from '../test-fixtures.fixture'
import { required } from '../store-test-support.fixture'
import type { ApiOptions } from '../lib/api'
import type { Message } from '../types/domain'
import { currentRoute } from '../lib/router'
import { IDLE_AFTER_MS } from '../lib/presence'

// Deferred transport responses exercise real store continuations and races.
const http = vi.hoisted(() => ({ handle: (url: string, _opts?: ApiOptions): unknown => url === '/api/channels' ? { categories: [], uncategorized: [] } : [] }))
vi.mock('../lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: vi.fn((url: string, options?: ApiOptions) => Promise.resolve(http.handle(url, options)))
}))
import { api } from '../lib/api'
import { notifyShowsText } from '../lib/notifyPreview'

// These tests look at the notification text, which is shown when the member's setting allows it.
notifyShowsText.value = true
function deferred<T>() {
  let resolve: (value: T) => void = () => { throw new Error('Uninitialized deferred') }
  let reject: (reason: unknown) => void = () => { throw new Error('Uninitialized deferred') }
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
const ch = channelFixture({ id: fixtureId(2) })
const me = userFixture({ id: fixtureId(1), username: 'me', presence: 'online' })
const other = userFixture({ id: fixtureId(5), username: 'other', display_name: 'Other' })
const message = (overrides: Partial<Message> = {}) => messageFixture({ user_id: other.id, ...overrides })
const tick = async () => { for (let i = 0; i < 10; i++) await Promise.resolve() }
function setup() {
  const chat = useChatStore()
  useAuthStore().user = { ...me }
  chat.activeChannel = { ...ch }
  chat.uncategorized = [ch]
  chat.members = [me, other]
  return chat
}
function hidden(value: boolean) { Object.defineProperty(document, 'hidden', { configurable: true, value }) }
class Socket {
  static latest: Socket | null = null
  onopen: (() => void) | null = null
  onclose: (() => void) | null = null
  onmessage: ((event: { data: string }) => void) | null = null
  send = vi.fn<(data: string) => void>()
  close = vi.fn()
  constructor(public url: string) { Socket.latest = this }
}
class BrowserNotification {
  static permission: NotificationPermission = 'granted'
  static requestPermission = vi.fn(async (): Promise<NotificationPermission> => 'granted')
  static items: BrowserNotification[] = []
  onclick: (() => void) | null = null
  close = vi.fn()
  constructor(public title: string, public options?: NotificationOptions) { BrowserNotification.items.push(this) }
}
beforeEach(() => {
  vi.useFakeTimers()
  vi.setSystemTime(new Date('2026-01-01T12:00:00Z'))
  setActivePinia(createPinia())
  http.handle = url => url === '/api/channels' ? { categories: [], uncategorized: [ch] } : []
  vi.mocked(api).mockClear()
  vi.stubGlobal('WebSocket', Socket)
  vi.stubGlobal('Notification', undefined)
  vi.spyOn(console, 'warn').mockImplementation(() => {})
  vi.spyOn(console, 'error').mockImplementation(() => {})
  hidden(false)
  BrowserNotification.items = []
  BrowserNotification.permission = 'granted'
  BrowserNotification.requestPermission.mockReset().mockResolvedValue('granted')
  Socket.latest = null
  // Store-owned page listeners last for the application session. Each test
  // removes only its listeners, preserving module-level return-to-tab hooks.
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
  hidden(false)
})


describe('optional values and defensive public actions', () => {
  it('keeps unknown profile preview fields empty and uses provided avatar/name/role', async () => {
    const chat = setup(), pending = deferred<unknown>()
    http.handle = () => pending.promise
    const unknown = fixtureId(90)
    const opening = chat.openUserProfile({ id: unknown })
    expect(chat.selectedUserProfile).toEqual({ id: unknown, username: '', display_name: '', avatar_url: '', role: 'user' })
    pending.resolve(userFixture({ id: unknown }))
    await opening
    const wait = deferred<unknown>()
    http.handle = () => wait.promise
    const second = chat.openUserProfile({ id: unknown, username: 'Preview', display_name: '', avatar_url: '/preview.svg', role: 'admin', created_at: FIXTURE_TIMESTAMP })
    expect(chat.selectedUserProfile?.display_name).toBe('Preview')
    expect(chat.selectedUserProfile?.avatar_url).toBe('/preview.svg')
    expect(chat.selectedUserProfile?.role).toBe('admin')
    expect(chat.selectedUserProfile?.created_at).toBe(FIXTURE_TIMESTAMP)
    wait.resolve(userFixture({ id: unknown }))
    await second
    await chat.openUserProfile({ id: '' })
  })
  it('recognizes replies to myself and legacy mentions only when signed in', () => {
    const chat = setup()
    expect(chat.messageMentionsMe(message({ reply_to: { id: fixtureId(9), user_id: me.id, deleted: false } }))).toBe(true)
    // The public action retains the old pre-mentions-array fallback; this is
    // intentionally an unknown legacy input, never asserted as a Message.
    expect(Reflect.apply(chat.messageMentionsMe, chat, [{ ...message(), mentions: undefined, content: '' }])).toBe(false)
    useAuthStore().user = null
    expect(chat.messageMentionsMe(message())).toBe(false)
  })
  it('uploads a bare file, sends root messages without reply fields and updates absent reactions safely', async () => {
    const chat = setup()
    http.handle = (_url, opts) => {
      if (opts?.form) expect([...opts.form.keys()]).toEqual(['file'])
      return message()
    }
    await chat.uploadMedia(new File(['x'], 'synthetic.txt'))
    await chat.sendMessage('root')
    expect(api).toHaveBeenLastCalledWith(expect.any(String), expect.objectContaining({ json: { content: 'root' } }))
    // ReactionResult permits omission of the reactions member.
    http.handle = () => ({})
    await chat.toggleReaction(message().id, '👍')
    expect(chat.messages[0]?.reactions).toEqual([])
    await chat.openThread('')
    await chat.openThread({ id: '' })
  })
  it('routes missing message inputs safely and leaves vanished navigation thread closed', async () => {
    const chat = setup()
    const startRoute = currentRoute.value
    chat.goToMessage(message({ id: '' }))
    chat.goToMessage(message({ channel_id: '' }))
    expect(currentRoute.value).toEqual(startRoute)
    const reply = message({ parent_id: fixtureId(10) })
    chat.goToMessage(reply)
    http.handle = () => Promise.reject({ status: 404 })
    expect(await chat.jumpToMessage(fixtureId(10))).toBe(false)
    expect(chat.activeThread).toBeNull()
    const wait = deferred<unknown>()
    http.handle = () => wait.promise
    const jump = chat.jumpToMessage(fixtureId(11))
    chat.activeChannel = channelFixture({ id: fixtureId(20) })
    wait.resolve([message({ id: fixtureId(11) })])
    expect(await jump).toBe(false)
    expect(chat.jumpTarget).toBeNull()
  })
  it('marks another channel unread without moving active divider, and initializes notification defaults', async () => {
    const chat = setup(), remote = fixtureId(99)
    http.handle = url => url === '/api/read-state' ? [readStateFixture({ channel_id: remote, unread_count: 2 })] : []
    await chat.markChannelUnread(remote, fixtureId(90))
    expect(chat.activeChannelLastReadAt).toBeNull()
    expect(chat.readStates[remote]?.unread_count).toBe(2)
    await chat.setNotificationLevel(fixtureId(98), 'all')
    expect(chat.readStates[fixtureId(98)]).toEqual({ channel_id: fixtureId(98), notify_level: 'all', unread_count: 0, mention_count: 0, last_read_at: null })
    chat.readStates = { [ch.id]: readStateFixture({ unread_count: 0, mention_count: 1 }) }
    window.dispatchEvent(new Event('focus'))
    expect(chat.readStates[ch.id]?.mention_count).toBe(0)
    http.handle = () => Promise.reject(new Error('offline'))
    await expect(chat.markChannelUnread(remote, fixtureId(90))).rejects.toThrow('offline')
  })
  it('discards buffered messages from another active channel and leaves live duplicates unchanged', async () => {
    const chat = setup()
    chat.hasMoreAfter = true
    chat.handleWSEvent({ type: 'message_create', payload: message() })
    const otherChannel = channelFixture({ id: fixtureId(8) })
    chat.activeChannel = otherChannel
    http.handle = () => []
    await chat.fetchMessages(otherChannel.id)
    expect(chat.messages).toEqual([])
    chat.activeChannel = ch
    const live = message()
    chat.handleWSEvent({ type: 'message_create', payload: live })
    chat.handleWSEvent({ type: 'message_create', payload: live })
    expect(chat.messages).toHaveLength(1)
    expect(chat.liveAppendSeq).toBe(1)
    chat.handleWSEvent({ type: 'message_create', payload: message({ id: fixtureId(9), parent_id: fixtureId(99) }) })
    expect(chat.threadReplies).toEqual([])
  })
  it('falls back to first voice channel when no text or route channel exists', async () => {
    const chat = setup(), voice = channelFixture({ id: fixtureId(30), type: 'voice' })
    chat.activeChannel = null
    http.handle = url => url === '/api/channels' ? { categories: [], uncategorized: [voice] } : []
    await chat.fetchChannels()
    await tick()
    expect(chat.activeChannel).toEqual(voice)
    expect(api).not.toHaveBeenCalledWith(`/api/channels/${voice.id}/read`, expect.anything())
  })
  it('handles empty sender/body notification, channel mute and mentions-only levels', () => {
    const chat = setup(), remote = fixtureId(20)
    vi.stubGlobal('Notification', BrowserNotification)
    hidden(true)
    chat.handleWSEvent({ type: 'message_create', payload: message({ channel_id: remote, content: '', display_name: '', username: '', attachments: [] }) })
    expect(BrowserNotification.items[0]?.title).toBe('')
    expect(BrowserNotification.items[0]?.options?.body).toBe('')
    chat.readStates = { [remote]: readStateFixture({ channel_id: remote, notify_level: 'mute' }) }
    chat.handleWSEvent({ type: 'message_create', payload: message({ channel_id: remote }) })
    chat.readStates = { [remote]: readStateFixture({ channel_id: remote, notify_level: 'mentions' }) }
    chat.handleWSEvent({ type: 'message_create', payload: message({ channel_id: remote }) })
    expect(BrowserNotification.items).toHaveLength(1)
  })
  it('keeps explicit typing labels and expires safely when its UI cache was cleared', () => {
    const chat = setup()
    // Optional labels were present in historical typing payloads. Reflect
    // keeps the unknown input at the defensive runtime boundary without a cast.
    Reflect.apply(chat.handleWSEvent, chat, [{ type: 'typing', payload: { channel_id: ch.id, user_id: other.id, username: 'wire', display_name: 'Wire Name' } }])
    expect(chat.typingByChannel[ch.id]?.[0]).toEqual({ user_id: other.id, username: 'wire', display_name: 'Wire Name' })
    chat.typingByChannel = {}
    vi.advanceTimersByTime(TYPING_EXPIRY_MS)
    expect(chat.typingByChannel[ch.id]).toEqual([])
  })
  it('labels a typer like the known member, never from the payload username alone', () => {
    const chat = setup()
    chat.members = [me, { ...other, display_name: '' }]
    Reflect.apply(chat.handleWSEvent, chat, [{ type: 'typing', payload: { channel_id: ch.id, user_id: other.id, username: 'wire' } }])
    expect(chat.typingByChannel[ch.id]?.[0]).toEqual({ user_id: other.id, username: 'wire', display_name: 'other' })
    chat.typingByChannel = {}
    chat.members = [me]
    Reflect.apply(chat.handleWSEvent, chat, [{ type: 'typing', payload: { channel_id: ch.id, user_id: other.id, username: 'wire' } }])
    expect(chat.typingByChannel[ch.id]?.[0]).toEqual({ user_id: other.id, username: 'wire', display_name: '' })
  })
  it('ignores malformed optional programmatic events before mutating state', () => {
    const chat = setup()
    // The wire decoder rejects these. Public actions also retain explicit
    // guards for old/programmatic callers, exercised with unknown values.
    const guarded = ['typing', 'read_state', 'message_create', 'user_update', 'message_update', 'message_delete', 'message_reaction', 'user_stats']
    for (const type of guarded) Reflect.apply(chat.handleWSEvent, chat, [{ type, payload: null }])
    Reflect.apply(chat.handleWSEvent, chat, [{ type: 'typing', payload: { channel_id: ch.id, user_id: '' } }])
    Reflect.apply(chat.handleWSEvent, chat, [{ type: 'read_state', payload: { channel_id: '' } }])
    Reflect.apply(chat.handleWSEvent, chat, [{ type: 'user_update', payload: { id: '' } }])
    chat.handleWSEvent({ type: 'pong', payload: { t: 0 } })
    expect(chat.messages).toEqual([])
    expect(chat.typingByChannel).toEqual({})
    expect(chat.readStates).toEqual({})
    expect(chat.members).toEqual([me, other])
  })
  it('keeps plain unread counts for unauthenticated messages without mentions', () => {
    const chat = setup()
    useAuthStore().user = null
    chat.activeChannel = null
    chat.handleWSEvent({ type: 'message_create', payload: message({ channel_id: ch.id, user_id: '' }) })
    expect(chat.readStates[ch.id]?.unread_count).toBe(1)
    expect(chat.readStates[ch.id]?.mention_count).toBe(0)
  })
})

describe('reconnect lifecycle variants', () => {
  it('reopens idle connection, resyncs open thread and stops retry after logout', async () => {
    const chat = setup()
    const root = message()
    chat.activeThread = root
    http.handle = url => url.includes('/thread') ? { root, replies: [] } : url === '/api/channels' ? { categories: [], uncategorized: [ch] } : []
    vi.advanceTimersByTime(IDLE_AFTER_MS)
    chat.initWebSocket()
    const socket = required(Socket.latest)
    socket.onopen?.()
    await tick()
    expect(socket.send).toHaveBeenCalledWith(expect.stringContaining('"idle":true'))
    expect(api).toHaveBeenCalledWith(`/api/messages/${root.id}/thread`, expect.anything())
    useAuthStore().user = null
    socket.onclose?.()
    expect(chat.reconnectAttempt).toBe(0)
    expect(chat.isConnected).toBe(false)
  })
  it('retries visible disconnected authenticated tab but stops if auth has expired', async () => {
    const chat = setup(), auth = useAuthStore()
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' })
    const checking = vi.spyOn(auth, 'checkAuth').mockResolvedValue(false)
    document.dispatchEvent(new Event('visibilitychange'))
    await tick()
    expect(checking).toHaveBeenCalled()
    expect(Socket.latest).toBeNull()
    expect(chat.isConnected).toBe(false)
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' })
    checking.mockClear()
    document.dispatchEvent(new Event('visibilitychange'))
    expect(checking).not.toHaveBeenCalled()
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' })
    chat.initWebSocket()
    chat.retryNow()
    expect(checking).not.toHaveBeenCalled()
  })
})

describe('session and optional signal boundaries', () => {
  it('leaves RTC signals harmless until handlers register, and ignores redundant socket initialization', () => {
    const chat = setup()
    expect(() => chat.handleWSEvent({ type: 'webrtc_offer', payload: { type: 'offer', sdp: 'synthetic' } })).not.toThrow()
    expect(() => chat.handleWSEvent({ type: 'webrtc_candidate', payload: { candidate: '' } })).not.toThrow()
    vi.spyOn(window.location, 'protocol', 'get').mockReturnValue('https:')
    chat.initWebSocket()
    const first = required(Socket.latest)
    expect(first.url.startsWith('wss://')).toBe(true)
    chat.initWebSocket()
    expect(Socket.latest).toBe(first)
    chat.closeWebSocket()
    useAuthStore().user = null
    chat.initWebSocket()
    expect(Socket.latest).toBe(first)
  })
  it('opens with no active history and marks an active channel unread without a stored read marker', async () => {
    const chat = setup()
    chat.activeChannel = null
    chat.initWebSocket()
    required(Socket.latest).onopen?.()
    await tick()
    chat.activeChannel = ch
    http.handle = () => []
    await chat.markChannelUnread(ch.id, fixtureId(20))
    expect(chat.activeChannelLastReadAt).toBeNull()
    expect(chat.readStates).toEqual({})
  })
})
