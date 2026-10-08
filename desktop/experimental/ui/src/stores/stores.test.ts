import { required } from '../store-test-support.fixture'
import { fixtureId } from '../test-fixtures.fixture'
const CHANNEL_ID = fixtureId(2)
import { channelFixture, messageFixture, userFixture } from '../test-fixtures.fixture'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useChatStore } from './chat'
import { useToastStore } from './toast'
import { useVoiceStore } from './voice'
import { useAuthStore } from './auth'
import { setLocale } from '../i18n'

beforeEach(() => {
  setLocale('de')
  setActivePinia(createPinia())
  vi.stubGlobal('fetch', vi.fn<typeof fetch>().mockImplementation(async () => new Response('[]', { status: 200 })))
})

afterEach(() => { vi.unstubAllGlobals(); localStorage.clear() })

describe('voice store', () => {
  it('starts without invented measurements', () => {
    const voice = useVoiceStore()
    expect(voice.ping).toBeNull()
    expect(voice.pingHistory).toEqual([])
    expect(voice.rtcStats).toBeNull()
    expect(voice.avgPing).toBeNull()
  })

  it('records real round trips only', () => {
    const voice = useVoiceStore()
    voice.recordPing(42.4)
    voice.recordPing(NaN)
    voice.recordPing(-5)
    expect(voice.pingHistory).toEqual([42])
    expect(voice.ping).toBe(42)
  })
})

describe('auth store', () => {
  it('never keeps a token in localStorage', async () => {
    localStorage.setItem('mnema_token', 'legacy')
    const auth = useAuthStore()
    vi.stubGlobal('fetch', vi.fn<typeof fetch>().mockImplementation(async () => new Response(JSON.stringify({ user: userFixture({ role: 'user' }) }), { status: 200 })))
    await auth.login('max', 'secret-secret')
    expect(localStorage.getItem('mnema_token')).toBeNull()
    expect(JSON.stringify(localStorage)).not.toContain('secret')
    expect(auth.isAuthenticated).toBe(true)
  })
})

describe('chat store events', () => {
  function setup() {
    const chat = useChatStore()
    chat.activeChannel = channelFixture({ id: CHANNEL_ID, type: 'text' })
    chat.messages = [messageFixture({ id: 'root', channel_id: CHANNEL_ID, reply_count: 0, reactions: [] })]
    return chat
  }

  it('counts a thread reply once even when it arrives twice', () => {
    const chat = setup()
    const reply = messageFixture({ id: 'r1', channel_id: CHANNEL_ID, parent_id: 'root' })
    chat.handleWSEvent({ type: 'message_create', payload: reply })
    chat.handleWSEvent({ type: 'message_create', payload: reply })
    expect(required(chat.messages[0]).reply_count).toBe(1)
  })

  it('ignores messages for other channels', () => {
    const chat = setup()
    chat.handleWSEvent({ type: 'message_create', payload: messageFixture({ id: 'x', channel_id: 'other' }) })
    expect(chat.messages).toHaveLength(1)
  })

  it('decrements the reply count when a reply is deleted', () => {
    const chat = setup()
    required(chat.messages[0]).reply_count = 2
    chat.handleWSEvent({ type: 'message_delete', payload: { id: 'r1', channel_id: CHANNEL_ID, parent_id: 'root' } })
    expect(required(chat.messages[0]).reply_count).toBe(1)
  })

  it('updates reply previews when the original is edited or deleted', () => {
    const chat = setup()
    chat.messages = [
      messageFixture({ id: 'o', channel_id: CHANNEL_ID, content: 'hi' }),
      messageFixture({ id: 'r', channel_id: CHANNEL_ID, reply_to_id: 'o', reply_to: { id: 'o', content: 'hi', deleted: false } })
    ]
    chat.handleWSEvent({ type: 'message_update', payload: messageFixture({ id: 'o', channel_id: CHANNEL_ID, content: 'x'.repeat(300) }) })
    expect(required(required(chat.messages[1]).reply_to).content).toHaveLength(200)
    chat.handleWSEvent({ type: 'message_delete', payload: { id: 'o', channel_id: CHANNEL_ID, parent_id: null } })
    expect(chat.messages).toHaveLength(1)
    expect(required(required(chat.messages[0]).reply_to).deleted).toBe(true)
  })

  it('applies reactions and presence updates', () => {
    const chat = setup()
    chat.handleWSEvent({ type: 'message_reaction', payload: { message_id: 'root', reactions: [{ emoji: '🔥', count: 1, users: ['u1'] }] } })
    expect(required(chat.messages[0]).reactions).toHaveLength(1)
    chat.handleWSEvent({ type: 'presence_update', payload: { user_id: 'u2', status: 'online' } })
    expect(chat.onlineUserIds.has('u2')).toBe(true)
    chat.handleWSEvent({ type: 'presence_update', payload: { user_id: 'u2', status: 'offline' } })
    expect(chat.onlineUserIds.has('u2')).toBe(false)
  })
})

describe('chat store history paging', () => {
  // Fake backend for GET /api/channels/{id}/messages with before/after/around.
  function serve(total: number) {
    const all = Array.from({ length: total }, (_, i) => messageFixture({ id: fixtureId(1000 + i), channel_id: CHANNEL_ID, content: String(i) }))
    const calls: string[] = []
    const respond = (body: unknown) => new Response(JSON.stringify(body), { status: 200 })
    vi.stubGlobal('fetch', vi.fn(async (url: string) => {
      // Opening a channel also marks it read; only history requests matter here.
      if (url.endsWith('/read')) return new Response(null, { status: 204 })
      calls.push(url)
      const u = new URL(url, 'http://x')
      const limit = Number(u.searchParams.get('limit'))
      const idx = (id: string | null) => all.findIndex(m => m.id === id)
      if (u.searchParams.has('before')) {
        const i = idx(u.searchParams.get('before'))
        return respond(all.slice(Math.max(0, i - limit), i))
      }
      if (u.searchParams.has('after')) {
        const i = idx(u.searchParams.get('after'))
        return respond(all.slice(i + 1, i + 1 + limit))
      }
      if (u.searchParams.has('around')) {
        const i = idx(u.searchParams.get('around'))
        if (i === -1) return new Response(JSON.stringify({ error: { code: 'NOT_FOUND' } }), { status: 404 })
        const older = all.slice(Math.max(0, i + 1 - (Math.floor(limit / 2) + 1)), i + 1)
        return respond(older.concat(all.slice(i + 1, i + 1 + limit - older.length)))
      }
      return respond(all.slice(Math.max(0, all.length - limit)))
    }))
    return { all, calls }
  }

  async function openChannel(total: number) {
    const srv = serve(total)
    const chat = useChatStore()
    await chat.selectChannel(channelFixture({ id: CHANNEL_ID, type: 'text' }))
    return { chat, srv }
  }

  it('loads the newest page, then older pages without duplicates', async () => {
    const { chat, srv } = await openChannel(500)
    expect(srv.calls[0]).toBe(`/api/channels/${CHANNEL_ID}/messages?limit=50`)
    expect(Number(required(chat.messages[0]).content)).toBe(450)
    expect(chat.hasMoreBefore).toBe(true)
    await chat.loadOlder()
    expect(srv.calls[1]).toContain(`before=${fixtureId(1450)}`)
    expect(chat.messages.map(m => Number(m.content))).toEqual(Array.from({ length: 100 }, (_, i) => 400 + i))
  })

  it('never runs two loads in the same direction at once', async () => {
    const { chat, srv } = await openChannel(500)
    await Promise.all([chat.loadOlder(), chat.loadOlder()])
    expect(srv.calls.filter(c => c.includes('before='))).toHaveLength(1)
  })

  it('caps the window, trims the bottom and holds back live messages while detached', async () => {
    const { chat } = await openChannel(1000)
    for (let i = 0; i < 6; i++) await chat.loadOlder()
    expect(chat.messages).toHaveLength(250)
    expect(chat.hasMoreAfter).toBe(true)
    chat.handleWSEvent({ type: 'message_create', payload: messageFixture({ id: 'live1', channel_id: CHANNEL_ID }) })
    expect(chat.messages.some(m => m.id === 'live1')).toBe(false)
    expect(chat.missedLiveCount).toBe(1)
    await chat.jumpToLatest()
    expect(chat.hasMoreAfter).toBe(false)
    expect(chat.missedLiveCount).toBe(0)
    // The held-back live message joins once the window reaches the present.
    expect(required(chat.messages.at(-1)).id).toBe('live1')
  })

  it('jumps around a message outside the window and reports missing ones', async () => {
    const { chat } = await openChannel(1000)
    expect(await chat.jumpToMessage(fixtureId(1100))).toBe(true)
    expect(chat.messages.some(m => m.id === fixtureId(1100))).toBe(true)
    expect(required(chat.jumpTarget).id).toBe(fixtureId(1100))
    expect(chat.hasMoreBefore && chat.hasMoreAfter).toBe(true)
    await chat.loadNewer()
    expect(Number(required(chat.messages.at(-1)).content)).toBe(174)

    expect(await chat.jumpToMessage('gone')).toBe(false)
    expect(required(useToastStore().toasts.at(-1)).text).toBe('Nachricht nicht gefunden')
  })

  it('ignores responses for a channel the user already left', async () => {
    serve(100)
    const chat = useChatStore()
    const first = chat.selectChannel(channelFixture({ id: CHANNEL_ID, type: 'text' }))
    chat.activeChannel = channelFixture({ id: 'ch2', type: 'text' })
    await first
    expect(chat.messages).toEqual([])
  })

  it('sends reply_to_id with messages and uploads', async () => {
    const chat = useChatStore()
    chat.activeChannel = channelFixture({ id: CHANNEL_ID, type: 'text' })
    const fetchMock = vi.fn<typeof fetch>().mockImplementation(async () => new Response(JSON.stringify(messageFixture({ channel_id: CHANNEL_ID })), { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)
    await chat.sendMessage('hey', null, 'orig')
    expect(JSON.parse(String(required(fetchMock.mock.calls[0])[1]?.body))).toEqual({ content: 'hey', reply_to_id: 'orig' })
    await chat.uploadMedia(new File(['x'], 'fixture.txt'), '', null, 'orig')
    const form = required(fetchMock.mock.calls[1])[1]?.body
    if (!(form instanceof FormData)) throw new Error('Expected multipart upload request')
    expect([...form.keys()]).toEqual(['reply_to_id', 'file'])
  })
})
