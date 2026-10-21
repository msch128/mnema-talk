import type { ApiOptions } from '../lib/api'

import { describe, it, expect, beforeEach, vi } from 'vitest'
import { useMentionsInbox } from './useMentionsInbox'
import { messageFixture, fixtureId } from '../test-fixtures.fixture'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args)
}))

const page = (ids: number[], has_more = false) => ({ messages: ids.map(n => messageFixture({ id: fixtureId(n) })), has_more })

beforeEach(() => {
  apiMock.mockReset()
  vi.spyOn(console, 'warn').mockImplementation(() => undefined)
})

describe('useMentionsInbox', () => {
  it('loads the newest page, then pages back with the oldest id', async () => {
    apiMock.mockResolvedValueOnce(page([5, 4], true)).mockResolvedValueOnce(page([3]))
    const inbox = useMentionsInbox()
    await inbox.load()
    expect(apiMock).toHaveBeenCalledWith('/api/mentions?limit=20', { decode: expect.any(Function) })
    expect(inbox.items.value.map(m => m.id)).toEqual([fixtureId(5), fixtureId(4)])
    expect(inbox.hasMore.value).toBe(true)
    await inbox.loadMore()
    expect(apiMock).toHaveBeenLastCalledWith(`/api/mentions?limit=20&before=${fixtureId(4)}`, { decode: expect.any(Function) })
    expect(inbox.items.value).toHaveLength(3)
    expect(inbox.hasMore.value).toBe(false)
    await inbox.loadMore()
    expect(apiMock).toHaveBeenCalledTimes(2)
  })

  it('reports a failed load and keeps the list when paging fails', async () => {
    apiMock.mockRejectedValueOnce(new Error('offline'))
    const inbox = useMentionsInbox()
    await inbox.load()
    expect(inbox.failed.value).toBe(true)
    expect(inbox.items.value).toEqual([])
    apiMock.mockResolvedValueOnce(page([2], true)).mockRejectedValueOnce(new Error('offline'))
    await inbox.load()
    expect(inbox.failed.value).toBe(false)
    await inbox.loadMore()
    expect(inbox.items.value).toHaveLength(1)
    expect(inbox.loadingMore.value).toBe(false)
  })

  it('ignores an answer that a newer load superseded', async () => {
    let release: (v: unknown) => void = () => undefined
    apiMock.mockImplementationOnce(() => new Promise(resolve => { release = resolve })).mockResolvedValueOnce(page([9]))
    const inbox = useMentionsInbox()
    const slow = inbox.load()
    await inbox.load()
    release(page([1]))
    await slow
    expect(inbox.items.value.map(m => m.id)).toEqual([fixtureId(9)])
  })

  it('drops a superseded failure and a superseded page', async () => {
    let fail: (e: unknown) => void = () => undefined
    let late: (v: unknown) => void = () => undefined
    apiMock.mockImplementationOnce(() => new Promise((_, reject) => { fail = reject })).mockResolvedValueOnce(page([8], true))
    const inbox = useMentionsInbox()
    const slow = inbox.load()
    await inbox.load()
    fail(new Error('late'))
    await slow
    expect(inbox.failed.value).toBe(false)
    apiMock.mockImplementationOnce(() => new Promise(resolve => { late = resolve }))
    const more = inbox.loadMore()
    await inbox.load().catch(() => undefined)
    late(page([1]))
    await more
    expect(inbox.items.value.map(m => m.id)).not.toContain(fixtureId(1))
  })
})
