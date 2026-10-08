import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useChatStore, TYPING_EXPIRY_MS } from './chat'
import { useAuthStore } from './auth'
import { useVoiceStore } from './voice'
import { useAppVersionStore } from './appVersion'
import { useToastStore } from './toast'
import { categoryFixture, channelFixture, messageFixture, userFixture, readStateFixture, fixtureId, FIXTURE_TIMESTAMP } from '../test-fixtures.fixture'
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

describe('chat actions and loaded copies', () => {
  it('sends trimmed replies, counts HTTP/WS echoes once and uploads ordered thread fields', async () => {
    const chat = setup(), root = message({ id: fixtureId(10) })
    chat.messages = [root]
    chat.activeThread = { ...root }
    const reply = message({ id: fixtureId(11), parent_id: root.id })
    http.handle = () => reply
    expect(await chat.sendThreadReply('  reply  ', fixtureId(12))).toEqual(reply)
    expect(api).toHaveBeenLastCalledWith(`/api/channels/${ch.id}/messages`, expect.objectContaining({ json: { content: 'reply', parent_id: root.id, reply_to_id: fixtureId(12) } }))
    chat.handleWSEvent({ type: 'message_create', payload: reply })
    expect(chat.messages[0]?.reply_count).toBe(1)
    expect(chat.activeThread?.reply_count).toBe(1)
    expect(chat.threadReplies).toHaveLength(1)
    const uploaded = message({ id: fixtureId(13), parent_id: root.id })
    http.handle = (_url, opts) => {
      expect(opts?.form && [...opts.form.keys()]).toEqual(['content', 'parent_id', 'reply_to_id', 'file'])
      return uploaded
    }
    const file = new File(['image'], 'synthetic.png', { type: 'image/png' })
    expect(await chat.uploadThreadMedia(file, 'caption', fixtureId(12))).toEqual(uploaded)
    expect(chat.threadReplies.map(m => m.id)).toEqual([reply.id, uploaded.id])
    chat.closeThread()
    expect(await chat.sendThreadReply('no thread')).toBeNull()
    expect(await chat.uploadThreadMedia(file)).toBeNull()
    expect(await chat.sendMessage(' ')).toBeNull()
    chat.activeChannel = null
    expect(await chat.sendMessage('text')).toBeNull()
    expect(await chat.uploadMedia(file)).toBeNull()
  })
  it('edits, reacts to and deletes all loaded copies and reply previews', async () => {
    const chat = setup(), root = message()
    const preview = message({ id: fixtureId(8), reply_to_id: root.id, reply_to: { id: root.id, user_id: other.id, content: root.content, deleted: false } })
    chat.messages = [root, preview]
    chat.threadReplies = [{ ...root }]
    chat.activeThread = { ...root }
    const edited = { ...root, content: 'edited', is_edited: true }
    http.handle = () => edited
    expect(await chat.editMessage(ch.id, root.id, ' edited ')).toEqual(edited)
    expect(chat.threadReplies[0]?.content).toBe('edited')
    expect(chat.activeThread?.content).toBe('edited')
    expect(chat.messages[1]?.reply_to?.content).toBe('edited')
    expect(await chat.editMessage(ch.id, root.id, ' ')).toBeNull()
    const reactions = [{ emoji: '👍', count: 1, user_ids: [me.id] }]
    http.handle = () => ({ reactions })
    expect(await chat.toggleReaction(root.id, '👍')).toEqual(reactions)
    expect(chat.activeThread?.reactions).toEqual(reactions)
    chat.handleWSEvent({ type: 'message_reaction', payload: { message_id: root.id, reactions: [] } })
    expect(chat.threadReplies[0]?.reactions).toEqual([])
    await chat.deleteMessage(ch.id, root.id)
    expect(chat.messages.map(m => m.id)).toEqual([preview.id])
    expect(chat.messages[0]?.reply_to?.deleted).toBe(true)
    expect(chat.threadReplies).toEqual([])
    expect(chat.activeThread).toBeNull()
    await chat.deleteMessage(ch.id, 'absent')
  })
  it('propagates user updates to account, members, previews, voice and profile', async () => {
    const chat = setup()
    chat.messages = [message({ user_id: me.id, reply_to: { id: fixtureId(8), user_id: me.id, deleted: false } })]
    chat.threadReplies = [message({ id: fixtureId(9), user_id: me.id })]
    chat.activeThread = message({ id: fixtureId(10), user_id: me.id })
    chat.selectedUserProfile = { ...me }
    const updated = { ...me, display_name: 'Renamed', avatar_url: '/synthetic.svg', status_text: 'Busy' }
    const voiceUpdate = vi.spyOn(useVoiceStore(), 'updateUser')
    http.handle = () => updated
    expect(await chat.setStatusText(me.id, 'Busy')).toEqual(updated)
    expect(api).toHaveBeenLastCalledWith('/api/users/me/status', expect.objectContaining({ json: { status_text: 'Busy' } }))
    expect(chat.messages[0]?.reply_to?.display_name).toBe('Renamed')
    expect(chat.threadReplies[0]?.avatar_url).toBe('/synthetic.svg')
    expect(chat.activeThread?.display_name).toBe('Renamed')
    expect(chat.members[0]?.display_name).toBe('Renamed')
    expect(chat.selectedUserProfile?.display_name).toBe('Renamed')
    expect(useAuthStore().user?.display_name).toBe('Renamed')
    expect(voiceUpdate).toHaveBeenCalledWith(updated)
    http.handle = () => other
    await chat.setStatusText(other.id, 'Admin status')
    expect(api).toHaveBeenLastCalledWith(`/api/admin/users/${other.id}/status`, expect.objectContaining({ method: 'PUT' }))
  })
  it('keeps profile preview after failed loading and applies successful full profile', async () => {
    const chat = setup()
    http.handle = () => Promise.reject(new Error('offline'))
    await chat.openUserProfile({ id: other.id })
    expect(chat.selectedUserProfile?.username).toBe('other')
    expect(chat.selectedUserProfile?.role).toBe('user')
    expect(console.warn).toHaveBeenCalled()
    http.handle = () => ({ ...other, bio: 'Full profile' })
    await chat.openUserProfile({ id: other.id, user_id: other.id, username: 'preview' })
    expect(chat.selectedUserProfile?.bio).toBe('Full profile')
    chat.closeUserProfile()
    chat.insertMention('other')
    chat.insertMention('')
    expect(chat.pendingMention).toBe('other')
  })
  it('preserves initial root on thread failure and ignores stale failures', async () => {
    const chat = setup(), wait = deferred<unknown>()
    http.handle = () => wait.promise
    const opening = chat.openThread(message())
    chat.closeThread()
    wait.reject(new Error('gone'))
    await opening
    expect(console.error).not.toHaveBeenCalled()
    http.handle = () => Promise.reject(new Error('offline'))
    await chat.openThread(message())
    expect(chat.activeThread?.id).toBe(message().id)
    expect(chat.isThreadLoading).toBe(false)
    expect(console.error).toHaveBeenCalled()
  })
})

describe('channel management', () => {
  it('creates, edits, duplicates and deletes categories/channels and refreshes hierarchy', async () => {
    const chat = setup(), category = categoryFixture()
    http.handle = url => url === '/api/channels' ? { categories: [categoryFixture({ channels: [ch] })], uncategorized: [] } : url.includes('categories') ? category : url.includes('channels') && !url.includes('messages') && !url.endsWith('/read') ? ch : []
    expect(await chat.createChannel({ name: 'new' })).toEqual(ch)
    expect(api).toHaveBeenCalledWith('/api/admin/channels', expect.objectContaining({ json: { category_id: null, name: 'new', type: 'text', topic: '', sort_order: 0 } }))
    await chat.createChannel({ categoryId: category.id, name: 'voice', type: 'voice', topic: 'Topic', sortOrder: 2 })
    expect(await chat.duplicateChannel(ch.id)).toEqual(ch)
    expect(await chat.updateChannel(ch.id, { name: 'rename', topic: 'updated' })).toEqual(ch)
    expect(await chat.createCategory('new category', 2)).toEqual(category)
    expect(await chat.createCategory('default category')).toEqual(category)
    expect(await chat.updateCategory(category.id, { name: 'renamed' })).toEqual(category)
    await chat.deleteCategory(category.id)
    await chat.deleteChannel(ch.id)
    expect(chat.activeChannel?.id).toBe(ch.id)
    await chat.deleteChannel(fixtureId(99))
    expect(chat.allChannels).toHaveLength(1)
  })
  it('clears window when the last channel disappears and reports fetch failures', async () => {
    const chat = setup()
    chat.messages = [message()]
    http.handle = () => ({ categories: [], uncategorized: [] })
    await chat.fetchChannels()
    expect(chat.activeChannel).toBeNull()
    expect(chat.messages).toEqual([])
    http.handle = () => Promise.reject(new Error('offline'))
    await chat.fetchChannels()
    await chat.fetchMembers()
    expect(console.error).toHaveBeenCalledTimes(2)
    expect(await chat.selectChannel(null)).toBeUndefined()
  })
  it('derives online/offline members and exact channel type from snapshot', () => {
    const chat = setup()
    chat.handleWSEvent({ type: 'presence_snapshot', payload: [me.id] })
    expect(chat.onlineMembers.map(m => m.id)).toEqual([me.id])
    expect(chat.offlineMembers.map(m => m.id)).toEqual([other.id])
    expect(chat.presenceOf(null)).toBe('offline')
    expect(chat.presenceOf(other.id)).toBe('offline')
    expect(chat.presenceOf(me.id)).toBe('online')
    chat.uncategorized.push(channelFixture({ id: fixtureId(8), type: 'voice' }))
    expect(chat.isVoiceChannel(fixtureId(8))).toBe(true)
    expect(chat.isVoiceChannel(ch.id)).toBe(false)
  })
})

describe('history and races', () => {
  it('pages both directions and buffers incoming live messages until reaching present', async () => {
    const chat = setup()
    chat.messages = [message({ id: fixtureId(10) })]
    chat.hasMoreBefore = true
    http.handle = () => [message({ id: fixtureId(9) })]
    expect(await chat.loadOlder()).toBe(true)
    expect(chat.messages.map(m => m.id)).toEqual([fixtureId(9), fixtureId(10)])
    expect(chat.hasMoreBefore).toBe(false)
    chat.hasMoreAfter = true
    const live = message({ id: fixtureId(12) })
    chat.handleWSEvent({ type: 'message_create', payload: live })
    chat.handleWSEvent({ type: 'message_create', payload: live })
    expect(chat.missedLiveCount).toBe(1)
    expect(chat.messages).toHaveLength(2)
    http.handle = () => [message({ id: fixtureId(11) })]
    expect(await chat.loadNewer()).toBe(true)
    expect(chat.messages.map(m => m.id)).toEqual([fixtureId(9), fixtureId(10), fixtureId(11), fixtureId(12)])
    expect(chat.missedLiveCount).toBe(0)
    expect(chat.hasMoreAfter).toBe(false)
  })
  it.each(['loadOlder', 'loadNewer'] as const)('%s blocks duplicate paging and discards superseded pages', async method => {
    const chat = setup(), wait = deferred<unknown>()
    chat.messages = [message()]
    chat.hasMoreBefore = true
    chat.hasMoreAfter = true
    http.handle = () => wait.promise
    const loading = chat[method]()
    expect(await chat[method]()).toBe(false)
    http.handle = () => []
    await chat.fetchMessages(ch.id)
    wait.resolve([message({ id: fixtureId(8) })])
    expect(await loading).toBe(false)
    expect(chat.messages).toEqual([])
    expect(chat.isLoadingBefore).toBe(false)
    expect(chat.isLoadingAfter).toBe(false)
  })
  it.each(['loadOlder', 'loadNewer'] as const)('%s handles empty anchors and reports failure without losing window', async method => {
    const chat = setup()
    chat.hasMoreBefore = true
    chat.hasMoreAfter = true
    expect(await chat[method]()).toBe(false)
    chat.messages = [message()]
    http.handle = () => Promise.reject(new Error('offline'))
    expect(await chat[method]()).toBe(false)
    expect(chat.messages).toHaveLength(1)
    expect(console.error).toHaveBeenCalled()
    chat.isLoadingWindow = true
    expect(await chat[method]()).toBe(false)
    chat.activeChannel = null
    expect(await chat[method]()).toBe(false)
  })
  it('distinguishes missing anchor/network failure, drops stale failure and jumps locally', async () => {
    const chat = setup(), toast = vi.spyOn(useToastStore(), 'info')
    http.handle = () => Promise.reject({ status: 404 })
    expect(await chat.jumpToMessage(fixtureId(9))).toBe(false)
    const notFound = required(toast.mock.calls[0])[0]
    http.handle = () => Promise.reject(new Error('offline'))
    expect(await chat.jumpToMessage(fixtureId(9))).toBe(false)
    expect(required(toast.mock.calls[1])[0]).not.toBe(notFound)
    const wait = deferred<unknown>()
    http.handle = () => wait.promise
    const jump = chat.jumpToMessage(fixtureId(9))
    http.handle = () => [message()]
    await chat.fetchMessages(ch.id)
    wait.reject(new Error('stale'))
    expect(await jump).toBe(false)
    expect(toast).toHaveBeenCalledTimes(2)
    expect(await chat.jumpToMessage(message().id)).toBe(true)
    expect(chat.jumpTarget?.id).toBe(message().id)
    expect(await chat.jumpToMessage('')).toBe(false)
    chat.activeChannel = null
    expect(await chat.jumpToMessage(message().id)).toBe(false)
    expect(await chat.jumpToLatest()).toBe(false)
  })
  it('keeps failed old latest responses from clearing newer successful window', async () => {
    const chat = setup(), wait = deferred<unknown>()
    http.handle = () => wait.promise
    const old = chat.fetchMessages(ch.id)
    http.handle = () => [message()]
    await chat.fetchMessages(ch.id)
    wait.reject(new Error('stale'))
    expect(await old).toBe(false)
    expect(chat.messages).toHaveLength(1)
    http.handle = () => Promise.reject(new Error('offline'))
    expect(await chat.jumpToLatest()).toBe(false)
    expect(chat.messages).toEqual([])
  })
  it('routes reply to root then opens thread after its anchor loads, including voice routes', async () => {
    const chat = setup(), root = message({ id: fixtureId(10) })
    chat.messages = [root]
    chat.goToMessage(message({ parent_id: root.id }))
    expect(currentRoute.value).toEqual(expect.objectContaining({ channelId: ch.id, messageId: root.id }))
    http.handle = () => ({ root, replies: [message({ parent_id: root.id })] })
    expect(await chat.jumpToMessage(root.id)).toBe(true)
    await tick()
    expect(chat.activeThread?.id).toBe(root.id)
    chat.uncategorized = [channelFixture({ ...ch, type: 'voice' })]
    chat.goToMessage(root)
    expect(window.location.pathname).toContain(`/v/${ch.id}`)
  })
  it('caps pending live messages, excludes own news and removes buffered deletions', async () => {
    const chat = setup()
    chat.hasMoreAfter = true
    for (let i = 20; i < 225; i++) chat.handleWSEvent({ type: 'message_create', payload: message({ id: fixtureId(i) }) })
    const own = message({ id: fixtureId(226), user_id: me.id })
    chat.handleWSEvent({ type: 'message_create', payload: own })
    expect(chat.missedLiveCount).toBe(205)
    chat.handleWSEvent({ type: 'message_delete', payload: { id: own.id, channel_id: ch.id, parent_id: null } })
    http.handle = () => []
    expect(await chat.jumpToLatest()).toBe(true)
    expect(chat.messages).toHaveLength(199)
    expect(chat.messages[0]?.id).toBe(fixtureId(26))
    expect(chat.messages.some(m => m.id === own.id)).toBe(false)
  })
})

describe('read state, presence and typing', () => {
  it('fetches read state and tolerates failed optimistic writes and empty action identifiers', async () => {
    const chat = setup()
    hidden(true)
    http.handle = () => [readStateFixture({ channel_id: ch.id, unread_count: 3, mention_count: 1 })]
    expect(await chat.fetchReadState()).toEqual({ [ch.id]: expect.objectContaining({ unread_count: 3 }) })
    await chat.setNotificationLevel(ch.id, 'mentions')
    expect(chat.notificationLevel(ch.id)).toBe('mentions')
    expect(chat.notificationLevel('absent')).toBe('all')
    http.handle = () => Promise.reject(new Error('offline'))
    expect(await chat.fetchReadState()).toEqual({})
    await chat.markChannelRead(ch.id)
    expect(chat.readStates[ch.id]?.unread_count).toBe(0)
    await chat.setNotificationLevel(ch.id, 'mute')
    expect(chat.readStates[ch.id]?.notify_level).toBe('mute')
    await chat.markChannelRead('')
    await chat.markChannelUnread('', 'id')
    await chat.markChannelUnread(ch.id, '')
    await chat.setNotificationLevel('', 'all')
    expect(console.warn).toHaveBeenCalledTimes(3)
  })
  it('clears auto-read suppression when mark-unread fails and when switching channels', async () => {
    const chat = setup()
    http.handle = () => Promise.reject(new Error('offline'))
    await expect(chat.markChannelUnread(ch.id, message().id)).rejects.toThrow('offline')
    http.handle = () => []
    chat.handleWSEvent({ type: 'message_create', payload: message() })
    expect(chat.readStates[ch.id]?.unread_count).toBe(0)
    const next = channelFixture({ id: fixtureId(20) })
    await chat.selectChannel(next)
    expect(chat.activeChannel?.id).toBe(next.id)
  })
  it('voice reading catches up only while panel is open', () => {
    const chat = setup()
    chat.activeChannel = channelFixture({ ...ch, type: 'voice' })
    chat.readStates = { [ch.id]: readStateFixture({ channel_id: ch.id, unread_count: 3 }) }
    chat.setVoiceChatReading(false)
    expect(chat.readStates[ch.id]?.unread_count).toBe(3)
    chat.setVoiceChatReading(true)
    expect(chat.readStates[ch.id]?.unread_count).toBe(0)
    chat.activeChannel = null
    window.dispatchEvent(new Event('focus'))
  })
  it('optimistically changes presence, handles idle online and rolls account back on failure', async () => {
    const chat = setup(), wait = deferred<unknown>()
    chat.presenceById = { [me.id]: 'online' }
    http.handle = () => wait.promise
    const setting = chat.setMyPresence('dnd')
    expect(useAuthStore().user?.presence).toBe('dnd')
    expect(chat.presenceOf(me.id)).toBe('dnd')
    wait.resolve(userFixture({ ...me, presence: 'dnd' }))
    await setting
    http.handle = () => Promise.reject(new Error('offline'))
    await expect(chat.setMyPresence('focus')).rejects.toThrow('offline')
    expect(useAuthStore().user?.presence).toBe('dnd')
    http.handle = () => ({ ...me, presence: 'online' })
    vi.advanceTimersByTime(IDLE_AFTER_MS)
    await chat.setMyPresence('online')
    expect(chat.presenceOf(me.id)).toBe('away')
    chat.presenceById = {}
    await chat.setMyPresence('away')
    expect(chat.presenceOf(me.id)).toBe('offline')
    useAuthStore().user = null
    await chat.setMyPresence('online')
  })
  it('resets idle presence on input and throttles high frequency activity', () => {
    const chat = setup()
    chat.initWebSocket()
    chat.isConnected = true
    const socket = required(Socket.latest)
    window.dispatchEvent(new Event('pointermove'))
    window.dispatchEvent(new Event('pointermove'))
    vi.advanceTimersByTime(IDLE_AFTER_MS)
    expect(socket.send).toHaveBeenCalledWith(expect.stringContaining('"idle":true'))
    window.dispatchEvent(new Event('keydown'))
    expect(socket.send).toHaveBeenLastCalledWith(expect.stringContaining('"idle":false'))
  })
  it('derives unknown typing labels, expires timers, clears on messages and ignores own notices', () => {
    const chat = setup(), unknown = fixtureId(99)
    chat.handleWSEvent({ type: 'typing', payload: { channel_id: ch.id, user_id: unknown } })
    expect(chat.typingByChannel[ch.id]?.[0]).toEqual({ user_id: unknown, username: '', display_name: '' })
    chat.handleWSEvent({ type: 'typing', payload: { channel_id: ch.id, user_id: other.id } })
    chat.handleWSEvent({ type: 'typing', payload: { channel_id: ch.id, user_id: me.id } })
    chat.handleWSEvent({ type: 'message_create', payload: message() })
    expect(chat.typingByChannel[ch.id]).toHaveLength(1)
    vi.advanceTimersByTime(TYPING_EXPIRY_MS)
    expect(chat.typingByChannel[ch.id]).toEqual([])
    chat.activeChannel = null
    chat.sendTyping()
  })
})

describe('browser notifications', () => {
  it('requests permission and uses current permission after rejected browser request', async () => {
    const chat = setup()
    expect(await chat.requestNotificationPermission()).toBe('unsupported')
    vi.stubGlobal('Notification', BrowserNotification)
    expect(await chat.requestNotificationPermission()).toBe('granted')
    BrowserNotification.permission = 'denied'
    BrowserNotification.requestPermission.mockRejectedValueOnce(new Error('blocked'))
    expect(await chat.requestNotificationPermission()).toBe('denied')
    expect(chat.notificationPermission).toBe('denied')
  })
  it('notifies off-channel mentions, focuses/routes on click, and silences focus/DND', () => {
    const chat = setup()
    vi.stubGlobal('Notification', BrowserNotification)
    const remote = channelFixture({ id: fixtureId(20), name: 'remote' })
    chat.uncategorized.push(remote)
    const msg = message({ channel_id: remote.id, display_name: 'Other', mentions: [me.id] })
    chat.handleWSEvent({ type: 'message_create', payload: msg })
    const notification = required(BrowserNotification.items[0])
    expect(notification.title).toBe('Other (#remote)')
    expect(notification.options?.body).toBe(msg.content)
    const focus = vi.spyOn(window, 'focus').mockImplementation(() => {})
    notification.onclick?.()
    expect(focus).toHaveBeenCalled()
    expect(notification.close).toHaveBeenCalled()
    expect(currentRoute.value).toEqual(expect.objectContaining({ channelId: remote.id, messageId: msg.id }))
    useAuthStore().user = { ...me, presence: 'focus' }
    chat.handleWSEvent({ type: 'message_create', payload: message({ id: fixtureId(31), channel_id: remote.id }) })
    expect(BrowserNotification.items).toHaveLength(1)
  })
  it('uses attachment/sender fallbacks, skips ungranted and reports constructor errors', () => {
    const chat = setup()
    vi.stubGlobal('Notification', BrowserNotification)
    hidden(true)
    const msg = message({ channel_id: fixtureId(20), display_name: '', username: 'fallback', content: '', attachments: [{ id: fixtureId(9), is_deleted: false, url: '/synthetic.png', mime_type: 'image/png', original_filename: 'synthetic.png', size_bytes: 1 }] })
    chat.handleWSEvent({ type: 'message_create', payload: msg })
    expect(BrowserNotification.items[0]?.title).toBe('fallback')
    expect(BrowserNotification.items[0]?.options?.body).toBeTruthy()
    BrowserNotification.permission = 'denied'
    chat.handleWSEvent({ type: 'message_create', payload: msg })
    expect(BrowserNotification.items).toHaveLength(1)
    vi.stubGlobal('Notification', class { static permission = 'granted'; constructor() { throw new Error('disabled') } })
    chat.handleWSEvent({ type: 'message_create', payload: msg })
    expect(console.warn).toHaveBeenCalled()
  })
})

describe('event routing and socket boundary', () => {
  it('forwards RTC/voice events and invokes kick handler or local disconnect', () => {
    const chat = setup(), voice = useVoiceStore()
    const offer = vi.fn(), candidate = vi.fn(), kicked = vi.fn()
    const snapshot = vi.spyOn(voice, 'setVoiceSnapshot'), state = vi.spyOn(voice, 'handleVoiceStateUpdate'), rooms = vi.spyOn(voice, 'setVoiceRooms')
    const media = vi.spyOn(voice, 'handleMediaState'), viewers = vi.spyOn(voice, 'handleScreenViewers'), speaking = vi.spyOn(voice, 'handleSpeakingEvent'), mute = vi.spyOn(voice, 'handleMuteState'), disconnect = vi.spyOn(voice, 'disconnect')
    chat.setWebRTCHandlers({ onOffer: offer, onCandidate: candidate, onKicked: kicked })
    chat.handleWSEvent({ type: 'webrtc_offer', payload: { type: 'offer', sdp: 'synthetic' } })
    chat.handleWSEvent({ type: 'webrtc_candidate', payload: { candidate: '' } })
    chat.handleWSEvent({ type: 'voice_snapshot', payload: {} })
    chat.handleWSEvent({ type: 'voice_state_update', payload: { action: 'leave', channel_id: ch.id, user_id: other.id } })
    chat.handleWSEvent({ type: 'voice_rooms', payload: { started: {}, now: FIXTURE_TIMESTAMP } })
    chat.handleWSEvent({ type: 'webrtc_media_state', payload: { channel_id: ch.id, user_id: other.id, screen: false, camera: false } })
    chat.handleWSEvent({ type: 'screen_viewers', payload: { channel_id: ch.id, user_id: other.id, viewers: [] } })
    chat.handleWSEvent({ type: 'voice_speaking', payload: { channel_id: ch.id, user_id: other.id, active: false } })
    chat.handleWSEvent({ type: 'voice_mute_state', payload: { channel_id: ch.id, user_id: other.id, muted: true, deafened: false } })
    chat.handleWSEvent({ type: 'voice_kicked', payload: { channel_id: ch.id } })
    for (const spy of [offer, candidate, kicked, snapshot, state, rooms, media, viewers, speaking, mute]) expect(spy).toHaveBeenCalledOnce()
    chat.setWebRTCHandlers({ onOffer: offer, onCandidate: candidate })
    chat.handleWSEvent({ type: 'voice_kicked', payload: { channel_id: ch.id } })
    expect(disconnect).not.toHaveBeenCalled()
    voice.currentChannelId = ch.id
    chat.handleWSEvent({ type: 'voice_kicked', payload: { channel_id: ch.id } })
    expect(disconnect).toHaveBeenCalledOnce()
  })
  it('updates stats/read states, refreshes snapshots and updates/deletes all message copies', async () => {
    const chat = setup(), root = message({ id: fixtureId(10), reply_count: 1 }), reply = message({ id: fixtureId(11), parent_id: fixtureId(10) })
    chat.messages = [root]
    chat.activeThread = { ...root }
    chat.threadReplies = [reply]
    chat.handleWSEvent({ type: 'user_stats', payload: { user_id: other.id, voice_seconds: 50 } })
    chat.handleWSEvent({ type: 'user_stats', payload: { user_id: fixtureId(99), voice_seconds: 50 } })
    expect(chat.members[1]?.voice_seconds).toBe(50)
    chat.handleWSEvent({ type: 'read_state', payload: { channel_id: ch.id, notify_level: 'mute' } })
    expect(chat.notificationLevel(ch.id)).toBe('mute')
    chat.handleWSEvent({ type: 'read_state', payload: { channel_id: ch.id, last_read_at: FIXTURE_TIMESTAMP, unread_count: 2, mention_count: 1 } })
    expect(chat.readStates[ch.id]?.unread_count).toBe(2)
    chat.handleWSEvent({ type: 'read_state', payload: { channel_id: ch.id, last_read_at: FIXTURE_TIMESTAMP, refresh: true } })
    chat.handleWSEvent({ type: 'member_joined', payload: other })
    chat.handleWSEvent({ type: 'channels_changed', payload: null })
    await tick()
    expect(api).toHaveBeenCalledWith('/api/read-state', expect.anything())
    expect(api).toHaveBeenCalledWith('/api/members', expect.anything())
    chat.handleWSEvent({ type: 'message_update', payload: { ...reply, content: 'new' } })
    expect(chat.threadReplies[0]?.content).toBe('new')
    chat.handleWSEvent({ type: 'message_delete', payload: { id: reply.id, channel_id: ch.id, parent_id: root.id } })
    expect(chat.messages[0]?.reply_count).toBe(0)
    expect(chat.activeThread?.reply_count).toBe(0)
    chat.handleWSEvent({ type: 'message_delete', payload: { id: root.id, channel_id: ch.id, parent_id: null } })
    expect(chat.activeThread).toBeNull()
    vi.advanceTimersByTime(3000)
    await tick()
    expect(api).toHaveBeenCalledWith('/api/members', expect.anything())
  })
  it('records RTT, server versions and update state', () => {
    const chat = setup(), ping = vi.spyOn(useVoiceStore(), 'recordPing'), version = vi.spyOn(useAppVersionStore(), 'setServerVersion')
    const follow = vi.spyOn(useAppVersionStore(), 'followAdminUpdates').mockImplementation(() => {}), updating = vi.spyOn(useAppVersionStore(), 'setUpdating')
    chat.handleWSEvent({ type: 'pong', payload: { t: Date.now() - 80 } })
    chat.handleWSEvent({ type: 'server_info', payload: { version: '0.4.4' } })
    chat.handleWSEvent({ type: 'system_update', payload: { version: '0.5.0' } })
    expect(ping).toHaveBeenCalledWith(80)
    expect(version).toHaveBeenCalledWith('0.4.4')
    expect(required(follow.mock.calls[0])[0]()).toBe(false)
    expect(updating).toHaveBeenCalledWith('0.5.0')
  })
  it('decodes valid events, ignores unknown names and reports malformed JSON/envelopes', async () => {
    const chat = setup()
    chat.initWebSocket()
    const socket = required(Socket.latest)
    socket.onopen?.()
    await tick()
    socket.onmessage?.({ data: JSON.stringify({ type: 'presence_snapshot', payload: [me.id] }) })
    expect(chat.presenceOf(me.id)).toBe('online')
    socket.onmessage?.({ data: JSON.stringify({ type: 'future_event', payload: {} }) })
    socket.onmessage?.({ data: '{' })
    socket.onmessage?.({ data: JSON.stringify({ type: 'presence_update', payload: {} }) })
    expect(console.error).toHaveBeenCalledTimes(2)
    expect(chat.isConnected).toBe(true)
    document.dispatchEvent(new Event('visibilitychange'))
    expect(socket.send).toHaveBeenLastCalledWith(expect.stringContaining('"ping"'))
    chat.retryNow()
    expect(Socket.latest).toBe(socket)
    chat.closeWebSocket()
    expect(socket.close).toHaveBeenCalledOnce()
  })
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
