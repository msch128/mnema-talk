import { required } from '../store-test-support.fixture'
import { categoryFixture, channelFixture, messageFixture, readStateFixture, userFixture, voiceUserFixture, FIXTURE_TIMESTAMP } from '../test-fixtures.fixture'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useChatStore, TYPING_EXPIRY_MS, MARK_READ_INTERVAL_MS } from './chat'
import { useVoiceStore } from './voice'
import { useAuthStore } from './auth'
import { currentRoute } from '../lib/router'

let calls: { url: string; method: string }[]
function stubFetch() {
  calls = []
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    calls.push({ url, method: init?.method || 'GET' })
    // Socket-open resync uses decoded GET snapshots; acknowledge mutation
    // calls with 204 and return valid empty collections for those snapshots.
    if (url === '/api/channels') return Response.json({ categories: [], uncategorized: [] })
    if (url === '/api/members' || url === '/api/read-state' || url.includes('/messages?')) return Response.json([])
    return new Response(null, { status: 204 })
  }))
}
const reads = () => calls.filter(c => c.url.endsWith('/read') && c.method === 'POST')

function setHidden(hidden: boolean) {
  Object.defineProperty(document, 'hidden', { configurable: true, get: () => hidden })
}

function setup() {
  const chat = useChatStore()
  const auth = useAuthStore()
  auth.user = userFixture({ id: 'me', username: 'max', role: 'user' })
  chat.categories = [categoryFixture({ id: 'c', channels: [channelFixture({ id: 'ch1', name: 'allgemein', type: 'text' }), channelFixture({ id: 'ch2', name: 'musik', type: 'text' })] })]
  chat.activeChannel = channelFixture({ id: 'ch1', type: 'text' })
  chat.messages = []
  chat.members = [userFixture({ id: 'u2', username: 'anna', display_name: 'Anna' }), userFixture({ id: 'u3', username: 'ben' })]
  return chat
}

const msg = (over = {}) => (messageFixture({ id: 'm' + Math.random(), channel_id: 'ch1', user_id: 'u2', content: 'hi', created_at: new Date().toISOString(), ...over }))

beforeEach(() => {
  vi.useFakeTimers()
  setActivePinia(createPinia())
  stubFetch()
  setHidden(false)
})

afterEach(() => {
  useChatStore().closeWebSocket()
  vi.clearAllTimers()
  vi.useRealTimers()
  vi.unstubAllGlobals()
  setHidden(false)
})

describe('marking the open channel read', () => {
  it('marks read while messages arrive, at most once per interval', () => {
    const chat = setup()
    chat.handleWSEvent({ type: 'message_create', payload: msg() })
    chat.handleWSEvent({ type: 'message_create', payload: msg() })
    chat.handleWSEvent({ type: 'message_create', payload: msg() })
    expect(reads()).toHaveLength(1)
    vi.advanceTimersByTime(MARK_READ_INTERVAL_MS)
    expect(reads()).toHaveLength(2)
    // The open channel never accumulates an unread badge.
    expect(chat.readStates.ch1?.unread_count || 0).toBe(0)
  })

  it('does not mark read while the tab is hidden, but counts unread', () => {
    const chat = setup()
    setHidden(true)
    chat.handleWSEvent({ type: 'message_create', payload: msg() })
    chat.handleWSEvent({ type: 'message_create', payload: msg() })
    expect(reads()).toHaveLength(0)
    expect(required(chat.readStates.ch1).unread_count).toBe(2)
  })

  it('marks read when the tab becomes visible again', () => {
    const chat = setup()
    setHidden(true)
    chat.handleWSEvent({ type: 'message_create', payload: msg() })
    setHidden(false)
    document.dispatchEvent(new Event('visibilitychange'))
    expect(reads()).toHaveLength(1)
    expect(required(chat.readStates.ch1).unread_count).toBe(0)
  })

  it('leaves other channels unread', () => {
    const chat = setup()
    chat.handleWSEvent({ type: 'message_create', payload: msg({ channel_id: 'ch2', content: 'hey @max', mentions: ['me'] }) })
    expect(reads()).toHaveLength(0)
    expect(required(chat.readStates.ch2).unread_count).toBe(1)
    expect(required(chat.readStates.ch2).mention_count).toBe(1)
  })

  it('does not mark read while viewing older history', () => {
    const chat = setup()
    chat.hasMoreAfter = true
    chat.handleWSEvent({ type: 'message_create', payload: msg() })
    expect(reads()).toHaveLength(0)
    expect(required(chat.readStates.ch1).unread_count).toBe(1)
  })

  it('does not treat a username with regexp characters as a pattern', () => {
    const chat = setup()
    useAuthStore().user = userFixture({ id: 'me', username: 'a.b(', role: 'user' })
    // Deliberately bypass the wire decoder for the retained legacy mention
    // fallback: normal current envelopes always carry a mentions array.
    expect(() => Reflect.apply(chat.handleWSEvent, chat, [{ type: 'message_create', payload: { ...msg({ channel_id: 'ch2', content: '@a.b( hi' }), mentions: undefined } }])).not.toThrow()
    expect(required(chat.readStates.ch2).mention_count).toBe(1)
    Reflect.apply(chat.handleWSEvent, chat, [{ type: 'message_create', payload: { ...msg({ channel_id: 'ch2', content: '@axb( hi' }), mentions: undefined } }])
    expect(required(chat.readStates.ch2).mention_count).toBe(1)
  })

  it('keeps a channel unread after "mark unread" until it is marked read', async () => {
    const chat = setup()
    const channel = channelFixture()
    chat.activeChannel = channel
    calls.length = 0
    vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
      calls.push({ url, method: init?.method || 'GET' })
      if (url === '/api/read-state') {
        return new Response(JSON.stringify([readStateFixture({ channel_id: channel.id, unread_count: 3, mention_count: 0, last_read_at: '2026-01-01T00:00:00Z' })]), { status: 200 })
      }
      return new Response(null, { status: 204 })
    }))
    await chat.markChannelUnread(channel.id, 'm1')
    expect(reads()).toHaveLength(0)
    expect(required(chat.readStates[channel.id]).unread_count).toBe(3)
    expect(chat.activeChannelLastReadAt).toBe('2026-01-01T00:00:00Z')
    await chat.markChannelRead(channel.id)
    expect(required(chat.readStates[channel.id]).unread_count).toBe(0)
  })
})

describe('typing indicator', () => {
  it('shows a typer and expires after the timeout', () => {
    const chat = setup()
    chat.handleWSEvent({ type: 'typing', payload: { channel_id: 'ch1', user_id: 'u2' } })
    expect(required(chat.typingByChannel.ch1).map(u => u.display_name)).toEqual(['Anna'])
    vi.advanceTimersByTime(TYPING_EXPIRY_MS - 1)
    expect(chat.typingByChannel.ch1).toHaveLength(1)
    vi.advanceTimersByTime(1)
    expect(chat.typingByChannel.ch1).toHaveLength(0)
  })

  it('refreshes the timer on repeated notices without duplicating the typer', () => {
    const chat = setup()
    chat.handleWSEvent({ type: 'typing', payload: { channel_id: 'ch1', user_id: 'u2' } })
    vi.advanceTimersByTime(3000)
    chat.handleWSEvent({ type: 'typing', payload: { channel_id: 'ch1', user_id: 'u2' } })
    vi.advanceTimersByTime(3000)
    expect(chat.typingByChannel.ch1).toHaveLength(1)
  })

  it('clears when that user\'s message arrives and ignores yourself', () => {
    const chat = setup()
    chat.handleWSEvent({ type: 'typing', payload: { channel_id: 'ch1', user_id: 'u2' } })
    chat.handleWSEvent({ type: 'typing', payload: { channel_id: 'ch1', user_id: 'me' } })
    expect(chat.typingByChannel.ch1).toHaveLength(1)
    chat.handleWSEvent({ type: 'message_create', payload: msg({ user_id: 'u2' }) })
    expect(chat.typingByChannel.ch1).toHaveLength(0)
  })

  it('sends typing at most every 3 seconds', () => {
    const send = vi.fn()
    class FakeSocket { send = send; onopen: (() => void) | null = null; constructor() { this.send = send; setTimeout(() => this.onopen?.(), 0) } close() {} }
    vi.stubGlobal('WebSocket', FakeSocket)
    const chat = setup()
    chat.initWebSocket()
    vi.advanceTimersByTime(1)
    send.mockClear() // the heartbeat ping sent on open
    chat.sendTyping('ch1')
    chat.sendTyping('ch1')
    expect(send).toHaveBeenCalledTimes(1)
    expect(JSON.parse(required(send.mock.calls[0])[0])).toEqual({ type: 'typing', payload: { channel_id: 'ch1' } })
    vi.advanceTimersByTime(3000)
    send.mockClear()
    chat.sendTyping('ch1')
    expect(send).toHaveBeenCalledTimes(1)
    chat.closeWebSocket()
  })
})

describe('desktop notifications', () => {
  let made: NotificationFixture[]
  class NotificationFixture {
    static permission: NotificationPermission = 'default'
    static requestPermission = vi.fn(async (): Promise<NotificationPermission> => { NotificationFixture.permission = 'granted'; return 'granted' })
    close = vi.fn()
    onclick: (() => void) | null = null
    constructor(public title: string, public opts: NotificationOptions) { made.push(this) }
  }
  function stubNotification(permission: NotificationPermission) {
    made = []
    NotificationFixture.permission = permission
    NotificationFixture.requestPermission.mockClear()
    vi.stubGlobal('Notification', NotificationFixture)
    return NotificationFixture
  }

  it('notifies for other channels and uses the svg icon', () => {
    stubNotification('granted')
    const chat = setup()
    chat.handleWSEvent({ type: 'message_create', payload: msg({ channel_id: 'ch2', display_name: 'Anna', content: 'hallo' }) })
    expect(made).toHaveLength(1)
    expect(required(made[0]).opts.icon).toBe('/favicon.svg')
    expect(required(made[0]).title).toContain('#musik')
  })

  it('stays quiet for the open channel in a visible tab, speaks up when hidden', () => {
    stubNotification('granted')
    const chat = setup()
    chat.handleWSEvent({ type: 'message_create', payload: msg() })
    expect(made).toHaveLength(0)
    setHidden(true)
    chat.handleWSEvent({ type: 'message_create', payload: msg() })
    expect(made).toHaveLength(1)
  })

  it('respects the per-channel level and the permission', async () => {
    const N = stubNotification('default')
    const chat = setup()
    const other = () => chat.handleWSEvent({ type: 'message_create', payload: msg({ channel_id: 'ch2' }) })
    const mention = () => chat.handleWSEvent({ type: 'message_create', payload: msg({ channel_id: 'ch2', content: '@max', mentions: ['me'] }) })
    other()
    expect(made).toHaveLength(0)
    expect(await chat.requestNotificationPermission()).toBe('granted')
    expect(chat.notificationPermission).toBe('granted')

    await chat.setNotificationLevel('ch2', 'mentions')
    expect(chat.notificationLevel('ch2')).toBe('mentions')
    other()
    expect(made).toHaveLength(0)
    mention()
    expect(made).toHaveLength(1)

    await chat.setNotificationLevel('ch2', 'mute')
    mention()
    expect(made).toHaveLength(1)
    expect(N.requestPermission).toHaveBeenCalledTimes(1)
    expect(calls.some(c => c.url === '/api/channels/ch2/notifications' && c.method === 'PUT')).toBe(true)
  })

  it('clicking a notification focuses the window and navigates to the message', () => {
    stubNotification('granted')
    const chat = setup()
    const focus = vi.spyOn(window, 'focus').mockImplementation(() => {})
    chat.handleWSEvent({ type: 'message_create', payload: msg({ id: 'mx', channel_id: 'ch2' }) })
    required(made[0]).onclick?.()
    expect(focus).toHaveBeenCalled()
    expect(currentRoute.value).toMatchObject({ view: 'chat', channelId: 'ch2', messageId: 'mx' })
  })

  it('defaults the level to all', () => {
    expect(setup().notificationLevel('whatever')).toBe('all')
  })

  it('stays silent while the user is on do not disturb or focus', () => {
    stubNotification('granted')
    const chat = setup()
    const auth = useAuthStore()
    for (const presence of ['dnd', 'focus'] as const) {
      auth.user = { ...required(auth.user), presence }
      chat.handleWSEvent({ type: 'message_create', payload: msg({ channel_id: 'ch2', mentions: ['me'] }) })
    }
    expect(made).toHaveLength(0)
    auth.user = { ...required(auth.user), presence: 'away' }
    chat.handleWSEvent({ type: 'message_create', payload: msg({ channel_id: 'ch2' }) })
    expect(made).toHaveLength(1)
  })

  it('counts @all/@here mentions from the server list', () => {
    const chat = setup()
    chat.handleWSEvent({ type: 'message_create', payload: msg({ channel_id: 'ch2', content: '@here', mentions: ['me', 'u3'] }) })
    chat.handleWSEvent({ type: 'message_create', payload: msg({ channel_id: 'ch2', content: '@max', mentions: [] }) })
    expect(required(chat.readStates.ch2).mention_count).toBe(1)
    expect(required(chat.readStates.ch2).unread_count).toBe(2)
  })
})

describe('presence', () => {
  it('tracks live statuses from snapshot and updates', () => {
    const chat = setup()
    chat.handleWSEvent({ type: 'presence_snapshot', payload: { u2: 'dnd', u3: 'online' } })
    expect(chat.presenceOf('u2')).toBe('dnd')
    expect(chat.onlineUserIds.has('u3')).toBe(true)
    chat.handleWSEvent({ type: 'presence_update', payload: { user_id: 'u3', status: 'away' } })
    expect(chat.presenceOf('u3')).toBe('away')
    chat.handleWSEvent({ type: 'presence_update', payload: { user_id: 'u3', status: 'offline' } })
    expect(chat.presenceOf('u3')).toBe('offline')
    expect(chat.onlineUserIds.has('u3')).toBe(false)
  })

  it('a status update from someone else never touches my chosen presence', () => {
    const chat = setup()
    const auth = useAuthStore()
    auth.user = { ...required(auth.user), presence: 'focus' }
    chat.handleWSEvent({ type: 'user_update', payload: userFixture({ id: 'me', status_text: 'cleared by admin' }) })
    expect(auth.user?.presence).toBe('focus')
    expect(auth.user?.status_text).toBe('cleared by admin')
  })
})

describe('profile changes', () => {
  it('a new avatar shows on messages, the open thread and in Talk', () => {
    const chat = setup()
    const voice = useVoiceStore()
    chat.messages = [messageFixture({ id: 'm1', user_id: 'u2', avatar_url: '/api/media/old', display_name: 'Old' })]
    chat.activeThread = messageFixture({ id: 't1', user_id: 'u2', avatar_url: '/api/media/old', display_name: 'Old' })
    voice.handleVoiceStateUpdate({ action: 'join', channel_id: 'v1', started_at: FIXTURE_TIMESTAMP, user: voiceUserFixture({ id: 'u2', avatar_url: '/api/media/old', display_name: 'Old', muted: true }) })
    chat.handleWSEvent({ type: 'user_update', payload: userFixture({ id: 'u2', avatar_url: '/api/media/new', display_name: 'New' }) })
    expect(chat.messages[0]).toMatchObject({ avatar_url: '/api/media/new', display_name: 'New' })
    expect(chat.activeThread).toMatchObject({ avatar_url: '/api/media/new', display_name: 'New' })
    expect(required(voice.channelUsers.v1).u2).toMatchObject({ avatar_url: '/api/media/new', display_name: 'New', muted: true })
  })
})

describe('goToMessage', () => {
  it('routes thread replies to their root message', () => {
    const chat = setup()
    chat.goToMessage(messageFixture({ id: 'reply', channel_id: 'ch2', parent_id: 'root' }))
    expect(currentRoute.value).toMatchObject({ channelId: 'ch2', messageId: 'root' })
    chat.goToMessage(messageFixture({ id: 'plain', channel_id: 'ch1' }))
    expect(currentRoute.value).toMatchObject({ channelId: 'ch1', messageId: 'plain' })
  })
})
