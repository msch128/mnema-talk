import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useChatStore } from './chat'
import { useAuthStore } from './auth'
import { categoryFixture, channelFixture, fixtureId, messageFixture, readStateFixture, userFixture } from '../test-fixtures.fixture'
import { required } from '../store-test-support.fixture'
import { ContractError } from '../types/validation'

beforeEach(() => {
  setActivePinia(createPinia()); vi.useFakeTimers()
  vi.spyOn(window, 'addEventListener'); vi.spyOn(document, 'addEventListener')
})
afterEach(() => {
  useChatStore().closeWebSocket(); vi.clearAllTimers(); vi.useRealTimers()
  for (const [type, listener, options] of vi.mocked(window.addEventListener).mock.calls) window.removeEventListener(type, listener, options)
  for (const [type, listener, options] of vi.mocked(document.addEventListener).mock.calls) document.removeEventListener(type, listener, options)
  vi.restoreAllMocks(); vi.unstubAllGlobals()
})

describe('chat account and read-state boundaries', () => {
  it('does not reconnect or send pings when an unauthenticated disconnected tab becomes visible', () => {
    const chat = useChatStore(); useAuthStore().user = null
    vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
    const fetch = vi.fn<typeof globalThis.fetch>(); vi.stubGlobal('fetch', fetch)
    document.dispatchEvent(new Event('visibilitychange'))
    expect(chat.isConnected).toBe(false); expect(chat.reconnectAttempt).toBe(0); expect(fetch).not.toHaveBeenCalled()
  })

  it('updates a name without erasing the retained avatar when an optional avatar is absent', () => {
    const chat = useChatStore()
    const author = userFixture({ id: fixtureId(9), display_name: 'Before', avatar_url: '/media/avatar' })
    const renamed = userFixture({ id: author.id, display_name: 'After' })
    expect('avatar_url' in renamed).toBe(false)
    chat.members = [author]
    chat.messages = [messageFixture({ user_id: author.id, display_name: 'Before', avatar_url: '/media/avatar' })]
    chat.handleWSEvent({ type: 'user_update', payload: renamed })
    expect(required(chat.messages[0]).display_name).toBe('After')
    expect(required(chat.messages[0]).avatar_url).toBe('/media/avatar')
    expect(required(chat.members[0]).avatar_url).toBe('/media/avatar')
  })

  it('does not issue mark-read for the signed-in user own message in the visible newest window', () => {
    useAuthStore().user = userFixture()
    const chat = useChatStore(); chat.activeChannel = channelFixture()
    const fetch = vi.fn<typeof globalThis.fetch>(); vi.stubGlobal('fetch', fetch)
    chat.handleWSEvent({ type: 'message_create', payload: messageFixture() })
    expect(chat.messages).toHaveLength(1); expect(fetch).not.toHaveBeenCalled()
    expect(chat.readStates).toEqual({})
  })

  it('retains a valid zero unread count without inventing a notification or mark-read', async () => {
    const chat = useChatStore(); chat.activeChannel = null
    const state = readStateFixture({ unread_count: 0, mention_count: 0, last_read_at: null })
    vi.stubGlobal('fetch', vi.fn<typeof globalThis.fetch>().mockImplementation(async () => new Response(JSON.stringify([state]), { status: 200 })))
    expect(await chat.fetchReadState()).toEqual({ [state.channel_id]: state })
    expect(chat.readStates[state.channel_id]).toEqual(state)
  })
})


describe('chat transport decoder boundaries', () => {
  it('retains the existing hierarchy when the transport omits a required channel array', async () => {
    const chat = useChatStore(), category = categoryFixture()
    chat.categories = [category]
    const error = vi.spyOn(console, 'error').mockImplementation(() => {})
    vi.stubGlobal('fetch', vi.fn<typeof globalThis.fetch>().mockImplementation(async () => new Response(JSON.stringify({ categories: [{ ...category, channels: null }], uncategorized: [] }), { status: 200 })))
    await chat.fetchChannels()
    expect(chat.categories).toEqual([category])
    expect(error).toHaveBeenCalledWith('Failed to fetch channels:', expect.any(ContractError))
  })

  it.each([
    { root: null, replies: [] },
    { root: messageFixture(), replies: null }
  ])('retains the root preview when the transport violates the thread contract %#', async payload => {
    const chat = useChatStore(), root = messageFixture()
    const error = vi.spyOn(console, 'error').mockImplementation(() => {})
    vi.stubGlobal('fetch', vi.fn<typeof globalThis.fetch>().mockImplementation(async () => new Response(JSON.stringify(payload), { status: 200 })))
    await chat.openThread(root)
    expect(chat.activeThread).toEqual(root)
    expect(chat.threadReplies).toEqual([])
    expect(chat.isThreadLoading).toBe(false)
    expect(error).toHaveBeenCalledWith('Failed to load thread:', expect.any(ContractError))
  })

  it.each([null, [{ ...readStateFixture(), channel_id: '' }], [{ ...readStateFixture(), notify_level: '' }]])('retains read states when the transport violates the read-state contract %#', async payload => {
    const chat = useChatStore(), state = readStateFixture()
    chat.readStates = { [state.channel_id]: state }
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    vi.stubGlobal('fetch', vi.fn<typeof globalThis.fetch>().mockImplementation(async () => new Response(JSON.stringify(payload), { status: 200 })))
    expect(await chat.fetchReadState()).toEqual({})
    expect(chat.readStates).toEqual({ [state.channel_id]: state })
    expect(warn).toHaveBeenCalledWith('Failed to fetch read-state:', expect.any(ContractError))
  })
})
