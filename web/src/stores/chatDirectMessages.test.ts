import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useChatStore } from './chat'
import { useAuthStore } from './auth'
import { useBlocksStore } from './blocks'
import { useServerFeaturesStore } from './serverFeatures'
import { channelFixture, categoryFixture, fixtureId, messageFixture, userFixture } from '../test-fixtures.fixture'
import { dmFixture, dmMemberFixture } from '../dm.fixture'
import type { ApiOptions } from '../lib/api'
import { api } from '../lib/api'
import * as router from '../lib/router'

const http = vi.hoisted(() => ({ handle: (_url: string, _opts?: ApiOptions): unknown => [] }))
vi.mock('../lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: vi.fn((url: string, options?: ApiOptions) => Promise.resolve(http.handle(url, options)))
}))

const me = fixtureId(1)
const ada = dmMemberFixture(2, { display_name: 'Ada' })
const bo = dmMemberFixture(3, { display_name: 'Bo' })
const pair = dmFixture({ id: fixtureId(100), members: [dmMemberFixture(1), ada] })
const group = dmFixture({ id: fixtureId(101), group: true, members: [dmMemberFixture(1), ada, bo] })

function setup(features: string[] = ['dm']) {
  const chat = useChatStore()
  useAuthStore().user = userFixture({ id: me })
  useServerFeaturesStore().list = features
  return chat
}

function calls(method: string) {
  return vi.mocked(api).mock.calls.filter(([, o]) => (o?.method ?? 'GET') === method)
}

beforeEach(() => {
  vi.useFakeTimers()
  setActivePinia(createPinia())
  // A created conversation is read again: the route answers with the one asked for.
  http.handle = url => [pair, group].find(d => url === `/api/dms/${d.id}`) ?? []
  vi.mocked(api).mockClear()
  vi.spyOn(console, 'warn').mockImplementation(() => {})
})
afterEach(() => {
  useChatStore().resetCommunityState()
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe('the conversation list', () => {
  it('asks the server for nothing when it has no direct messages', async () => {
    const chat = setup([])
    await chat.fetchDms()
    expect(api).not.toHaveBeenCalled()
    expect(chat.dms).toEqual([])
  })

  it('lists the newest conversation first with its unread count, and no mentions of the inbox', async () => {
    const chat = setup()
    const quiet = dmFixture({ id: fixtureId(102), members: [dmMemberFixture(1), bo], last_message_at: '2026-01-02T00:00:00Z' })
    const busy = dmFixture({ id: fixtureId(103), members: [dmMemberFixture(1), dmMemberFixture(4)], last_message_at: '2026-01-03T00:00:00Z', unread_count: 3 })
    http.handle = () => [quiet, busy]
    await chat.fetchDms()
    expect(chat.dms.map(d => d.id)).toEqual([busy.id, quiet.id])
    expect(chat.dmsLoaded).toBe(true)
    expect(chat.readStates[busy.id]).toMatchObject({ unread_count: 3, mention_count: 0, notify_level: 'all' })
  })

  it('keeps the counts of conversations when the channels read states are read again', async () => {
    const chat = setup()
    http.handle = () => [dmFixture({ id: pair.id, members: pair.members, unread_count: 2 })]
    await chat.fetchDms()
    http.handle = () => []
    await chat.fetchReadState()
    expect(chat.readStates[pair.id]?.unread_count).toBe(2)
  })

  it('reads the list again for a message of a conversation it does not know yet', async () => {
    const chat = setup()
    http.handle = () => [pair]
    chat.handleWSEvent({ type: 'message_create', payload: messageFixture({ id: fixtureId(7), channel_id: pair.id, user_id: ada.id }) })
    await vi.advanceTimersByTimeAsync(0)
    expect(chat.dms.map(d => d.id)).toEqual([pair.id])
  })
})

describe('events', () => {
  it('adds a created conversation once and moves it up when a message arrives', () => {
    const chat = setup()
    chat.handleWSEvent({ type: 'dm_created', payload: pair })
    chat.handleWSEvent({ type: 'dm_created', payload: group })
    chat.handleWSEvent({ type: 'dm_created', payload: group })
    expect(chat.dms).toHaveLength(2)
    chat.handleWSEvent({ type: 'message_create', payload: messageFixture({ id: fixtureId(8), channel_id: pair.id, user_id: ada.id, created_at: '2030-01-01T00:00:00Z' }) })
    expect(chat.dms[0]?.id).toBe(pair.id)
    expect(chat.readStates[pair.id]).toMatchObject({ unread_count: 1, mention_count: 0 })
  })

  it('reads a created conversation again, as the event carries the starter\'s notify level and send right', async () => {
    const chat = setup()
    http.handle = () => ({ ...pair, notify_level: 'all', can_send: false })
    chat.handleWSEvent({ type: 'dm_created', payload: { ...pair, notify_level: 'mute', can_send: true } })
    await vi.advanceTimersByTimeAsync(0)
    expect(calls('GET').map(([url]) => url)).toContain(`/api/dms/${pair.id}`)
    expect(chat.dms[0]).toMatchObject({ id: pair.id, notify_level: 'all', can_send: false })
  })

  it('does not count a message of the conversation that is open and being read', async () => {
    const chat = setup()
    chat.handleWSEvent({ type: 'dm_created', payload: pair })
    await chat.openDm(pair.id)
    chat.handleWSEvent({ type: 'message_create', payload: messageFixture({ id: fixtureId(9), channel_id: pair.id, user_id: ada.id }) })
    expect(chat.readStates[pair.id]?.unread_count ?? 0).toBe(0)
  })

  it('adds and removes group members, renaming the open conversation', async () => {
    const chat = setup()
    chat.handleWSEvent({ type: 'dm_created', payload: group })
    await chat.openDm(group.id)
    expect(chat.activeChannel?.name).toBe('Ada, Bo')
    const cy = dmMemberFixture(5, { display_name: 'Cy' })
    chat.handleWSEvent({ type: 'dm_member_added', payload: { dm_id: group.id, member: cy } })
    chat.handleWSEvent({ type: 'dm_member_added', payload: { dm_id: group.id, member: cy } })
    expect(chat.dmById(group.id)?.members).toHaveLength(4)
    expect(chat.activeChannel?.name).toBe('Ada, Bo, Cy')
    chat.handleWSEvent({ type: 'dm_member_removed', payload: { dm_id: group.id, member: bo } })
    expect(chat.activeChannel?.name).toBe('Ada, Cy')
  })

  it('closes a conversation the caller left on another device', async () => {
    const chat = setup()
    const navigate = vi.spyOn(router, 'navigate')
    chat.handleWSEvent({ type: 'dm_created', payload: group })
    await chat.openDm(group.id)
    chat.handleWSEvent({ type: 'dm_member_removed', payload: { dm_id: group.id, member: dmMemberFixture(1) } })
    expect(chat.dms).toEqual([])
    expect(chat.activeChannel).toBeNull()
    expect(chat.readStates[group.id]).toBeUndefined()
    expect(navigate).toHaveBeenCalledWith('/', { replace: true })
  })

  it('reads the conversations again when permissions change, so can_send follows the community switch', async () => {
    const chat = setup()
    chat.handleWSEvent({ type: 'dm_created', payload: pair })
    expect(chat.dmById(pair.id)?.can_send).toBe(true)
    http.handle = url => (url === '/api/dms' ? [{ ...pair, can_send: false }] : [])
    chat.handleWSEvent({ type: 'permissions_changed', payload: null })
    await vi.advanceTimersByTimeAsync(0)
    expect(chat.dmById(pair.id)?.can_send).toBe(false)
  })

  it('refreshes the block list for block_updated', async () => {
    setup(['blocks'])
    http.handle = () => []
    useChatStore().handleWSEvent({ type: 'block_updated', payload: { user_id: ada.id, blocked: true } })
    await vi.advanceTimersByTimeAsync(0)
    expect(vi.mocked(api).mock.calls.map(([u]) => u)).toContain('/api/me/blocks')
  })
})

describe('mark everything read', () => {
  it('reads the conversations too, but not for one category', async () => {
    const chat = setup()
    http.handle = url => (url === '/api/dms' ? [{ ...pair, unread_count: 4 }] : [])
    await chat.fetchDms()
    await chat.markAllRead(fixtureId(10))
    expect(chat.readStates[pair.id]?.unread_count).toBe(4)
    await chat.markAllRead()
    expect(chat.readStates[pair.id]?.unread_count).toBe(0)
    expect(calls('POST').some(([u]) => u === `/api/channels/${pair.id}/read`)).toBe(true)
  })
})

describe('a blocked sender', () => {
  it('adds no unread, mention or notification, in a group conversation or a channel', () => {
    const chat = setup(['dm', 'blocks'])
    useBlocksStore().blocked = [{ user_id: ada.id, username: 'member2', display_name: 'Ada', blocked_at: '2026-01-01T00:00:00Z' }]
    const notify = vi.fn()
    vi.stubGlobal('Notification', Object.assign(vi.fn(notify), { permission: 'granted' }))
    chat.handleWSEvent({ type: 'dm_created', payload: group })
    const channelId = fixtureId(2)
    for (const id of [group.id, channelId]) {
      chat.handleWSEvent({ type: 'message_create', payload: messageFixture({ id: fixtureId(70), channel_id: id, user_id: ada.id, content: '@member1 hi' }) })
      expect(chat.readStates[id]?.unread_count ?? 0).toBe(0)
      expect(chat.readStates[id]?.mention_count ?? 0).toBe(0)
    }
    chat.handleWSEvent({ type: 'message_create', payload: messageFixture({ id: fixtureId(71), channel_id: group.id, user_id: bo.id }) })
    expect(chat.readStates[group.id]?.unread_count).toBe(1)
    expect(notify).not.toHaveBeenCalledWith(expect.stringContaining('Ada'), expect.anything())
    vi.unstubAllGlobals()
  })
})

describe('starting, adding and leaving', () => {
  it('starts a conversation with the chosen members and opens it under its title', async () => {
    const chat = setup()
    http.handle = (_url, options) => (options?.method === 'POST' ? group : [])
    const dm = await chat.startDm([ada.id, bo.id])
    expect(calls('POST')[0]?.[1]?.json).toEqual({ user_ids: [ada.id, bo.id] })
    await chat.openDm(dm.id)
    expect(chat.activeChannel).toMatchObject({ id: group.id, name: 'Ada, Bo', type: 'text' })
    expect(chat.isDm(group.id)).toBe(true)
    expect(chat.isDm(fixtureId(555))).toBe(false)
  })

  it('keeps an open conversation when the channel list is read again', async () => {
    const chat = setup()
    chat.handleWSEvent({ type: 'dm_created', payload: pair })
    await chat.openDm(pair.id)
    http.handle = () => ({ categories: [categoryFixture({ channels: [channelFixture({ id: fixtureId(2) })] })], uncategorized: [] })
    await chat.fetchChannels()
    expect(chat.activeChannel?.id).toBe(pair.id)
  })

  it('adds a member through the members route and leaves with @me', async () => {
    const chat = setup()
    chat.handleWSEvent({ type: 'dm_created', payload: group })
    const cy = dmMemberFixture(5)
    http.handle = (_url, options) => (options?.method === 'POST' ? { ...group, members: [...group.members, cy] } : undefined)
    await chat.addDmMember(group.id, cy.id)
    expect(calls('POST')[0]).toEqual([`/api/dms/${group.id}/members`, expect.objectContaining({ json: { user_id: cy.id } })])
    expect(chat.dmById(group.id)?.members).toHaveLength(4)
    await chat.leaveDm(group.id)
    expect(calls('DELETE')[0]?.[0]).toBe(`/api/dms/${group.id}/members/@me`)
    expect(chat.dms).toEqual([])
  })

  it('treats a conversation as a channel for the chat area but not for the channel list', async () => {
    const chat = setup()
    chat.handleWSEvent({ type: 'dm_created', payload: pair })
    expect(chat.dmChannel(pair.id)?.name).toBe('Ada')
    expect(chat.allChannels.some(c => c.id === pair.id)).toBe(false)
  })
})

describe('the socket', () => {
  function captureSockets() {
    const urls: string[] = []
    vi.stubGlobal('WebSocket', class {
      onopen = null
      onclose = null
      onmessage = null
      readyState = 0
      constructor(url: string) { urls.push(url) }
      send() {}
      close() {}
    })
    return urls
  }
  afterEach(() => { vi.unstubAllGlobals() })

  it('reads the conversations only once the socket is open, so a conversation created before that is not missed', async () => {
    const sockets: { onopen: (() => void) | null }[] = []
    vi.stubGlobal('WebSocket', class {
      onopen: (() => void) | null = null
      onclose = null
      onmessage = null
      readyState = 0
      constructor() { sockets.push(this) }
      send() {}
      close() {}
    })
    const chat = setup(['dm'])
    let created = false
    http.handle = url => (url === '/api/dms' ? (created ? [pair] : []) : [])
    chat.initWebSocket()
    // Somebody starts a conversation after sign-in, before the socket is up: no list was read yet.
    created = true
    expect(vi.mocked(api).mock.calls.some(([u]) => u === '/api/dms')).toBe(false)
    const waiting = chat.waitForDms(5000)
    sockets[0]?.onopen?.()
    await waiting
    expect(chat.dms.map(d => d.id)).toEqual([pair.id])
    expect(vi.mocked(api).mock.calls.filter(([u]) => u === '/api/dms')).toHaveLength(1)
    chat.closeWebSocket()
  })

  it('does not wait for the conversations on a server without them, and gives up when the socket never opens', async () => {
    const chat = setup([])
    await chat.waitForDms(5000)
    useServerFeaturesStore().list = ['dm']
    const waiting = chat.waitForDms(3000)
    await vi.advanceTimersByTimeAsync(3000)
    await waiting
    expect(chat.dmsLoaded).toBe(false)
  })

  it('asks for conversation events only when the server has them', () => {
    const urls = captureSockets()
    const chat = setup(['dm'])
    chat.initWebSocket()
    chat.closeWebSocket()
    useServerFeaturesStore().list = []
    chat.initWebSocket()
    chat.closeWebSocket()
    expect(urls.map(u => u.replace(/^wss?:\/\/[^/]+/, ''))).toEqual(['/api/ws?dm=1', '/api/ws'])
  })
})
