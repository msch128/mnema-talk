// Stale responses and partial updates in the chat store: a late load must
// never overwrite what the user opened since, and a partial user update must
// not blank fields it does not carry.
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'

// Every api() call waits until the test answers it.
const pending = vi.hoisted(() => [])
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal()),
  api: vi.fn((url, opts) => new Promise((resolve, reject) => {
    pending.push({ url, method: opts?.method || 'GET', resolve, reject })
  }))
}))

import { useChatStore } from './chat'
import { useAuthStore } from './auth'

function answer(match, body) {
  const i = pending.findIndex(p => p.url.includes(match))
  if (i < 0) throw new Error(`no pending request for ${match}`)
  const [req] = pending.splice(i, 1)
  req.resolve(body)
}

// Lets awaited api() results run their continuations.
const flush = () => new Promise(r => setTimeout(r, 0))

function setup() {
  const chat = useChatStore()
  useAuthStore().user = { id: 'me', username: 'max', role: 'user' }
  chat.categories = [{
    id: 'c',
    channels: [
      { id: 'ch1', name: 'allgemein', type: 'text' },
      { id: 'ch2', name: 'musik', type: 'text' },
      { id: 'v1', name: 'talk', type: 'voice' }
    ]
  }]
  chat.activeChannel = { id: 'ch1', name: 'allgemein', type: 'text' }
  chat.messages = []
  // A connected socket that records what the store sends.
  const sent = []
  let socket = null
  vi.stubGlobal('WebSocket', class {
    constructor() { socket = this }
    send(data) { sent.push(JSON.parse(data)) }
    close() {}
  })
  chat.initWebSocket()
  socket.onopen()
  return { chat, sent }
}

beforeEach(() => {
  pending.length = 0
  setActivePinia(createPinia())
})

afterEach(() => {
  useChatStore().closeWebSocket()
  vi.unstubAllGlobals()
})


describe('openThread', () => {
  it('drops a thread load that another thread superseded', async () => {
    const { chat } = setup()
    const first = chat.openThread('a')
    const second = chat.openThread('b')
    answer('/messages/b/thread', { root: { id: 'b', content: 'B' }, replies: [{ id: 'rb' }] })
    await second
    answer('/messages/a/thread', { root: { id: 'a', content: 'A' }, replies: [{ id: 'ra' }] })
    await first
    expect(chat.activeThread.id).toBe('b')
    expect(chat.threadReplies.map(r => r.id)).toEqual(['rb'])
    expect(chat.isThreadLoading).toBe(false)
  })

  it('does not reopen a thread that was closed while it loaded', async () => {
    const { chat } = setup()
    const open = chat.openThread('a')
    chat.closeThread()
    answer('/messages/a/thread', { root: { id: 'a' }, replies: [] })
    await open
    expect(chat.activeThread).toBeNull()
  })
})

describe('openUserProfile', () => {
  it('ignores a profile that loads after another one was opened', async () => {
    const { chat } = setup()
    chat.openUserProfile({ id: 'u1', username: 'anna' })
    chat.openUserProfile({ id: 'u2', username: 'ben' })
    answer('/users/u1', { id: 'u1', username: 'anna', bio: 'A' })
    await flush()
    expect(chat.selectedUserProfile.id).toBe('u2')
    expect(chat.selectedUserProfile.bio).toBeUndefined()
  })

  it('stays closed when closed while loading', async () => {
    const { chat } = setup()
    chat.openUserProfile({ id: 'u1', username: 'anna' })
    chat.closeUserProfile()
    answer('/users/u1', { id: 'u1', username: 'anna' })
    await flush()
    expect(chat.selectedUserProfile).toBeNull()
  })
})

describe('user updates', () => {
  it('a partial update keeps names and avatars', () => {
    const { chat } = setup()
    chat.messages = [{ id: 'm1', user_id: 'u1', display_name: 'Anna', avatar_url: '/a.png', reply_to: { user_id: 'u1', display_name: 'Anna', avatar_url: '/a.png' } }]
    chat.handleWSEvent({ type: 'user_update', payload: { id: 'u1', disabled: true } })
    expect(chat.messages[0].display_name).toBe('Anna')
    expect(chat.messages[0].avatar_url).toBe('/a.png')
    expect(chat.messages[0].reply_to.display_name).toBe('Anna')
  })

  it('updates the open thread root too', () => {
    const { chat } = setup()
    chat.activeThread = { id: 'root', user_id: 'u1', display_name: 'Anna', avatar_url: '/a.png' }
    chat.handleWSEvent({ type: 'user_update', payload: { id: 'u1', display_name: 'Annie', avatar_url: '/b.png' } })
    expect(chat.activeThread.display_name).toBe('Annie')
    expect(chat.activeThread.avatar_url).toBe('/b.png')
  })
})

describe('channel list refresh', () => {
  it('shows a renamed active channel under its new name', async () => {
    const { chat } = setup()
    const fetching = chat.fetchChannels()
    answer('/api/channels', {
      categories: [{ id: 'c', channels: [{ id: 'ch1', name: 'neu', type: 'text', topic: 'T' }] }],
      uncategorized: []
    })
    await fetching
    expect(chat.activeChannel.name).toBe('neu')
    expect(chat.activeChannel.topic).toBe('T')
  })

  it('keeps the newest list when overlapping refetches answer out of order', async () => {
    const { chat } = setup()
    const older = chat.fetchChannels()
    const newer = chat.fetchChannels()
    const list = name => ({ categories: [{ id: 'c', channels: [{ id: 'ch1', name, type: 'text' }] }], uncategorized: [] })
    // Requests are answered first-in-first-out by answer(); swap them.
    const [first, second] = pending.splice(0, 2)
    second.resolve(list('neu'))
    await newer
    first.resolve(list('alt'))
    await older
    expect(chat.categories[0].channels[0].name).toBe('neu')
  })

  it('a superseded refetch resolves only once the newest list is in', async () => {
    // The sidebar awaits its own refetch after saving a new order and then
    // drops the order it kept on screen; a channels_changed refetch started
    // meanwhile must not leave the older list showing in between.
    const { chat } = setup()
    const list = name => ({ categories: [{ id: 'c', channels: [{ id: 'ch1', name, type: 'text' }] }], uncategorized: [] })
    let ownDone = false
    const own = chat.fetchChannels().then(() => { ownDone = true })
    chat.fetchChannels()
    const [first, second] = pending.splice(0, 2)
    first.resolve(list('alt'))
    await flush()
    expect(ownDone).toBe(false)
    second.resolve(list('neu'))
    await own
    expect(chat.categories[0].channels[0].name).toBe('neu')
  })

  it('duplicates a channel and refetches the list', async () => {
    const { chat } = setup()
    const dup = chat.duplicateChannel('ch2')
    expect(pending[0]).toMatchObject({ url: '/api/admin/channels/ch2/duplicate', method: 'POST' })
    answer('/duplicate', { id: 'ch3', name: 'musik', type: 'text' })
    await flush()
    answer('/api/channels', { categories: [{ id: 'c', channels: [{ id: 'ch3', name: 'musik', type: 'text' }] }], uncategorized: [] })
    await expect(dup).resolves.toEqual({ id: 'ch3', name: 'musik', type: 'text' })
    expect(chat.allChannels.map(c => c.id)).toEqual(['ch3'])
  })
})

describe('typing notices', () => {
  it('are throttled per channel', () => {
    const { chat, sent } = setup()
    chat.sendTyping('ch1')
    chat.sendTyping('ch1')
    chat.sendTyping('ch2')
    expect(sent.filter(e => e.type === 'typing').map(e => e.payload.channel_id)).toEqual(['ch1', 'ch2'])
  })

  it('are never sent for voice channels', () => {
    const { chat, sent } = setup()
    chat.sendTyping('v1')
    expect(sent.filter(e => e.type === 'typing')).toHaveLength(0)
  })
})

describe('missed live messages', () => {
  it('my own messages do not count as missed', () => {
    const { chat } = setup()
    chat.hasMoreAfter = true
    chat.handleWSEvent({ type: 'message_create', payload: { id: 'x1', channel_id: 'ch1', user_id: 'me' } })
    chat.handleWSEvent({ type: 'message_create', payload: { id: 'x2', channel_id: 'ch1', user_id: 'u1' } })
    expect(chat.missedLiveCount).toBe(1)
  })
})

describe('isVoiceChannel', () => {
  it('knows voice channels by id', () => {
    const { chat } = setup()
    expect(chat.isVoiceChannel('v1')).toBe(true)
    expect(chat.isVoiceChannel('ch1')).toBe(false)
    expect(chat.isVoiceChannel('nope')).toBe(false)
  })
})
