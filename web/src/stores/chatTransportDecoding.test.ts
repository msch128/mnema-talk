import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useChatStore } from './chat'
import { categoryFixture, messageFixture, readStateFixture } from '../test-fixtures.fixture'
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
