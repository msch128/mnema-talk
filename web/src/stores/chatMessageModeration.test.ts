import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import type { ApiOptions } from '../lib/api'
import { useChatStore } from './chat'
import { messageFixture } from '../test-fixtures.fixture'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args)
}))

beforeEach(() => {
  setActivePinia(createPinia())
  apiMock.mockReset()
  vi.spyOn(window, 'addEventListener'); vi.spyOn(document, 'addEventListener')
})
afterEach(() => {
  useChatStore().closeWebSocket()
  for (const [type, listener, options] of vi.mocked(window.addEventListener).mock.calls) window.removeEventListener(type, listener, options)
  for (const [type, listener, options] of vi.mocked(document.addEventListener).mock.calls) document.removeEventListener(type, listener, options)
  vi.restoreAllMocks()
})

describe('message moderation in the chat store', () => {
  it('pins and unpins through the API and updates the message in place', async () => {
    const chat = useChatStore()
    chat.messages = [messageFixture({ id: 'm1', channel_id: 'c1' })]
    apiMock.mockResolvedValueOnce(messageFixture({ id: 'm1', channel_id: 'c1', is_pinned: true }))
    await chat.setPinned('c1', 'm1', true)
    expect(apiMock).toHaveBeenLastCalledWith('/api/channels/c1/messages/m1/pin', { method: 'PUT', decode: expect.any(Function) })
    expect(chat.messages[0]?.is_pinned).toBe(true)
    apiMock.mockResolvedValueOnce(messageFixture({ id: 'm1', channel_id: 'c1', is_pinned: false }))
    await chat.setPinned('c1', 'm1', false)
    expect(apiMock).toHaveBeenLastCalledWith('/api/channels/c1/messages/m1/pin', { method: 'DELETE', decode: expect.any(Function) })
    expect(chat.messages[0]?.is_pinned).toBe(false)
  })

  it('sends the reason with a deletion only when there is one', async () => {
    const chat = useChatStore()
    chat.messages = [messageFixture({ id: 'm1', channel_id: 'c1' }), messageFixture({ id: 'm2', channel_id: 'c1' })]
    apiMock.mockResolvedValue(undefined)
    await chat.deleteMessage('c1', 'm1', 'insult')
    expect(apiMock).toHaveBeenLastCalledWith('/api/channels/c1/messages/m1', { method: 'DELETE', json: { reason: 'insult' } })
    await chat.deleteMessage('c1', 'm2')
    expect(apiMock).toHaveBeenLastCalledWith('/api/channels/c1/messages/m2', { method: 'DELETE' })
    expect(chat.messages).toHaveLength(0)
  })
})
