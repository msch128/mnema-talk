import type { VueWrapper } from '@vue/test-utils'
import type { ApiOptions } from '../lib/api'

import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import AdminMediaTab from './AdminMediaTab.vue'
import { setLocale } from '../i18n'
import { useToastStore } from '../stores/toast'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args),
  onUnauthorized: vi.fn(),
  ApiError: class ApiError extends Error {}
}))
const confirmMock = vi.fn()
vi.mock('../lib/confirm', () => ({ confirm: (...args: [path: string, options?: ApiOptions]) => confirmMock(...args) }))

const stats = { total_files: 3, total_size_bytes: 3 * 1024 * 1024, deleted_files: 1 }
const items = () => [
  { created_at: '2026-01-01T00:00:00Z', channel_id: null, uploader_id: '00000000-0000-4000-8000-000000000001',  id: "00000000-0000-4000-8000-0000000003ef", original_filename: 'cat.png', mime_type: 'image/png', size_bytes: 2048, uploader_name: 'Max', url: "/api/media/00000000-0000-4000-8000-0000000003ef", is_deleted: false },
  { created_at: '2026-01-01T00:00:00Z', channel_id: null, uploader_id: '00000000-0000-4000-8000-000000000001',  id: "00000000-0000-4000-8000-0000000003f0", original_filename: 'x.svg', mime_type: 'image/svg+xml', size_bytes: 10, uploader_name: 'Max', url: "/api/media/00000000-0000-4000-8000-0000000003f0", is_deleted: false },
  { created_at: '2026-01-01T00:00:00Z', channel_id: null, uploader_id: '00000000-0000-4000-8000-000000000001',  id: "00000000-0000-4000-8000-0000000003f2", original_filename: 'old.pdf', mime_type: 'application/pdf', size_bytes: 0, uploader_name: 'Eva', url: "/api/media/00000000-0000-4000-8000-0000000003f2", is_deleted: true }
]

// Route-based API fake; `fail` maps a URL prefix to an error.
function serve({ fail = {}, list = items(), st = stats }: { fail?: Record<string, string>; list?: ReturnType<typeof items> | null; st?: typeof stats | null } = {}) {
  apiMock.mockImplementation(url => {
    for (const [prefix, msg] of Object.entries(fail)) {
      if (url.startsWith(prefix)) return Promise.reject(new Error(msg))
    }
    if (url === '/api/admin/media/stats') return Promise.resolve(st)
    if (url.startsWith('/api/admin/media?')) return Promise.resolve(list)
    if (url.startsWith('/api/admin/media/prune')) return Promise.resolve({ pruned_count: 2, cutoff_days: 30 })
    return Promise.resolve(null)
  })
}

const tiles = (w: VueWrapper) => w.findAll('.grid > div')
const toasts = () => useToastStore().toasts

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
  confirmMock.mockReset()
})

describe('AdminMediaTab', () => {
  it('shows the storage totals and the media grid', async () => {
    serve()
    const w = mount(AdminMediaTab)
    await flushPromises()
    expect(w.text()).toContain('3 MB')
    expect(w.text()).toContain('3 files, 1 deleted')
    expect(tiles(w)).toHaveLength(3)
    // Only raster images get a preview; SVG never renders inline.
    expect(tiles(w)[0]!.find('img')!.attributes('src')).toBe("/api/media/00000000-0000-4000-8000-0000000003ef")
    expect(tiles(w)[1]!.find('img')!.exists()).toBe(false)
    expect(tiles(w)[0]!.text()).toContain('2 KB • Max')
    expect(tiles(w)[2]!.text()).toContain('(deleted)')
    expect(tiles(w)[2]!.text()).toContain('0 B')
    expect(tiles(w)[2]!.find('button')!.exists()).toBe(false)
  })

  it('keeps the other half when one request fails', async () => {
    serve({ fail: { '/api/admin/media/stats': 'stats down' } })
    const w = mount(AdminMediaTab)
    await flushPromises()
    expect(tiles(w)).toHaveLength(3)
    expect(w.text()).toContain('0 B')
    expect(toasts().map(t => t.text)).toEqual(['stats down'])

    serve({ fail: { '/api/admin/media?': 'list down' }, list: [] })
    await w.find('button[aria-label="Refresh"]')!.trigger('click')
    await flushPromises()
    expect(w.text()).toContain('3 files, 1 deleted')
    expect(tiles(w)).toHaveLength(3)
  })

  it('shows the empty state for null responses', async () => {
    serve({ list: null, st: null })
    const w = mount(AdminMediaTab)
    await flushPromises()
    expect(w.text()).toContain('No media yet.')
    expect(w.text()).toContain('0 files, 0 deleted')
  })

  it('prunes after confirmation and refreshes', async () => {
    serve()
    const w = mount(AdminMediaTab)
    await flushPromises()
    const prune = w.findAll('button').find(b => b.text() === 'Delete …')!
    expect(prune!.attributes('disabled')).toBeDefined()
    await w.find<HTMLInputElement>('input[type="number"]')!.setValue('30')
    expect(prune!.attributes('disabled')).toBeUndefined()

    confirmMock.mockResolvedValueOnce(false)
    await prune!.trigger('click')
    await flushPromises()
    expect(confirmMock.mock.calls[0]![0]!).toMatchObject({ danger: true })
    expect(apiMock).not.toHaveBeenCalledWith(expect.stringContaining('prune'), expect.anything())

    confirmMock.mockResolvedValueOnce(true)
    apiMock.mockClear()
    await prune!.trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith('/api/admin/media/prune?days=30', { decode: expect.any(Function),  method: 'POST' })
    expect(toasts().at(-1)!.text).toBe('2 files deleted (older than 30 days).')
    expect(w.find<HTMLInputElement>('input[type="number"]')!.element.value).toBe('')
    expect(apiMock).toHaveBeenCalledWith('/api/admin/media/stats', { decode: expect.any(Function) })
  })

  it('keeps the day count when pruning fails', async () => {
    serve({ fail: { '/api/admin/media/prune': 'storage unavailable' } })
    const w = mount(AdminMediaTab)
    await flushPromises()
    await w.find<HTMLInputElement>('input[type="number"]')!.setValue('7')
    confirmMock.mockResolvedValueOnce(true)
    await w.findAll('button').find(b => b.text() === 'Delete …')!.trigger('click')
    await flushPromises()
    expect(toasts().at(-1)!).toMatchObject({ type: 'error', text: 'storage unavailable' })
    expect(w.find<HTMLInputElement>('input[type="number"]')!.element.value).toBe('7')
  })

  it('deletes one file after confirmation', async () => {
    serve()
    const w = mount(AdminMediaTab)
    await flushPromises()
    const del = tiles(w)[0]!.find('button[aria-label="Delete file"]')!

    confirmMock.mockResolvedValueOnce(false)
    await del.trigger('click')
    await flushPromises()
    expect(confirmMock.mock.calls[0]![0]!.title).toContain('cat.png')
    expect(apiMock).not.toHaveBeenCalledWith("/api/admin/media/00000000-0000-4000-8000-0000000003ef", expect.anything())

    confirmMock.mockResolvedValueOnce(true)
    await del.trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith("/api/admin/media/00000000-0000-4000-8000-0000000003ef", { method: 'DELETE' })
    expect(toasts().at(-1)!.text).toBe('File deleted')
  })

  it('reports a failed delete without refreshing', async () => {
    serve({ fail: { "/api/admin/media/00000000-0000-4000-8000-0000000003ef": '' } })
    const w = mount(AdminMediaTab)
    await flushPromises()
    confirmMock.mockResolvedValueOnce(true)
    apiMock.mockClear()
    await tiles(w)[0]!.find('button[aria-label="Delete file"]')!.trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledTimes(1)
    expect(toasts().at(-1)!.type).toBe('error')
  })
})
