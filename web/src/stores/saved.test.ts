import type { ApiOptions } from '../lib/api'

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises } from '@vue/test-utils'
import { useSavedStore } from './saved'
import { fixtureId, FIXTURE_TIMESTAMP } from '../test-fixtures.fixture'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args)
}))

const entry = (id: number, note = '') => ({ message_id: fixtureId(id), note, saved_at: FIXTURE_TIMESTAMP, available: false })
const page = (items: unknown[], more = false) => ({ items, has_more: more })

beforeEach(() => {
  setActivePinia(createPinia())
  apiMock.mockReset()
  vi.spyOn(console, 'warn').mockImplementation(() => undefined)
})

describe('saved store', () => {
  it('knows which messages are saved from what it loaded and changed', async () => {
    apiMock.mockResolvedValueOnce(page([entry(1)]))
    const saved = useSavedStore()
    expect(saved.isSaved(fixtureId(1))).toBe(false)
    await saved.load()
    expect(saved.isSaved(fixtureId(1))).toBe(true)
    apiMock.mockResolvedValueOnce(entry(2))
    await saved.save(fixtureId(2))
    expect(apiMock).toHaveBeenLastCalledWith(`/api/me/saved/${fixtureId(2)}`, expect.not.objectContaining({ json: expect.anything() }))
    expect(saved.items.map(e => e.message_id)).toEqual([fixtureId(2), fixtureId(1)])
    // Saving again with a note replaces the entry in place.
    apiMock.mockResolvedValueOnce(entry(1, 'n'))
    await saved.save(fixtureId(1), 'n')
    expect(apiMock).toHaveBeenLastCalledWith(`/api/me/saved/${fixtureId(1)}`, expect.objectContaining({ json: { note: 'n' } }))
    expect(saved.items.find(e => e.message_id === fixtureId(1))?.note).toBe('n')
    expect(saved.items).toHaveLength(2)
  })

  it('ignores an answer that arrives after a newer load', async () => {
    let first: (value: unknown) => void = () => undefined
    apiMock.mockReturnValueOnce(new Promise(resolve => { first = resolve }))
    const saved = useSavedStore()
    const stale = saved.load()
    apiMock.mockResolvedValueOnce(page([entry(2)]))
    await saved.load()
    first(page([entry(1)]))
    await stale
    expect(saved.items.map(e => e.message_id)).toEqual([fixtureId(2)])
    expect(saved.loading).toBe(false)
  })

  it('keeps the list and the paging when a later page fails, and does nothing without more pages', async () => {
    const saved = useSavedStore()
    await saved.loadMore()
    expect(apiMock).not.toHaveBeenCalled()
    apiMock.mockResolvedValueOnce(page([entry(1)], true))
    await saved.load()
    apiMock.mockRejectedValueOnce(new Error('offline'))
    await saved.loadMore()
    expect(saved.items).toHaveLength(1)
    expect(saved.hasMore).toBe(true)
    expect(saved.loadingMore).toBe(false)
  })

  it('marks a failed load and empties the list', async () => {
    const saved = useSavedStore()
    apiMock.mockResolvedValueOnce(page([entry(1)]))
    await saved.load()
    apiMock.mockRejectedValueOnce(new Error('offline'))
    await saved.load()
    expect(saved.failed).toBe(true)
    expect(saved.items).toEqual([])
  })

  it('follows another session: a save reloads, a removal drops the entry', async () => {
    const saved = useSavedStore()
    apiMock.mockResolvedValueOnce(page([entry(1)]))
    saved.onUpdated({ message_id: fixtureId(1), saved: true })
    await flushPromises()
    expect(saved.isSaved(fixtureId(1))).toBe(true)
    expect(saved.items).toHaveLength(1)
    saved.onUpdated({ message_id: fixtureId(1), saved: false })
    expect(saved.items).toHaveLength(0)
    expect(saved.isSaved(fixtureId(1))).toBe(false)
  })
})
