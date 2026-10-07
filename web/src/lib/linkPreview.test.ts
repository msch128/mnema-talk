import { describe, it, expect, vi, beforeEach } from 'vitest'

vi.mock('./api', () => ({ api: vi.fn() }))
import { api } from './api'
import { fetchLinkPreview, clearLinkPreviewCache } from './linkPreview'

beforeEach(() => {
  clearLinkPreviewCache()
  vi.mocked(api).mockReset()
})

describe('fetchLinkPreview', () => {
  it('requests each url once, however often it is asked', async () => {
    vi.mocked(api).mockResolvedValue({ title: 'Titel', url: 'https://a.de' })
    const [a, b] = await Promise.all([fetchLinkPreview('https://a.de'), fetchLinkPreview('https://a.de')])
    await fetchLinkPreview('https://a.de')
    expect(api).toHaveBeenCalledTimes(1)
    expect(a?.title).toBe('Titel')
    expect(b).toBe(a)
  })

  it('remembers "no preview" (204) and definitive errors', async () => {
    vi.mocked(api).mockResolvedValueOnce(null)
    expect(await fetchLinkPreview('https://none.de')).toBeNull()
    expect(await fetchLinkPreview('https://none.de')).toBeNull()
    expect(api).toHaveBeenCalledTimes(1)
  })

  it('retries after a rate limit', async () => {
    vi.mocked(api).mockRejectedValueOnce({ status: 429 })
    expect(await fetchLinkPreview('https://r.de')).toBeNull()
    vi.mocked(api).mockResolvedValueOnce({ title: 'ok' })
    expect((await fetchLinkPreview('https://r.de'))?.title).toBe('ok')
    expect(api).toHaveBeenCalledTimes(2)
  })
})

  it('remembers empty metadata but accepts a description without a title', async () => {
    vi.mocked(api).mockResolvedValueOnce({ title: '', description: '' })
    expect(await fetchLinkPreview('https://empty.example')).toBeNull()
    expect(await fetchLinkPreview('https://empty.example')).toBeNull()
    vi.mocked(api).mockResolvedValueOnce({ title: '', description: 'synthetic' })
    expect((await fetchLinkPreview('https://description.example'))?.description).toBe('synthetic')
    expect(api).toHaveBeenCalledTimes(2)
  })

  it.each([null, 'synthetic', { status: 'bad' }, { status: 0 }, { status: 500 }])('retries network/server failures: %j', async error => {
    vi.mocked(api).mockRejectedValueOnce(error)
    expect(await fetchLinkPreview('https://retry.example')).toBeNull()
    vi.mocked(api).mockResolvedValueOnce({ title: 'synthetic' })
    expect((await fetchLinkPreview('https://retry.example'))?.title).toBe('synthetic')
    expect(api).toHaveBeenCalledTimes(2)
  })

  it('remembers permanent failures and evicts the oldest URL when its bounded cache fills', async () => {
    vi.mocked(api).mockRejectedValueOnce({ status: 404 })
    expect(await fetchLinkPreview('https://permanent.example')).toBeNull()
    expect(await fetchLinkPreview('https://permanent.example')).toBeNull()
    expect(api).toHaveBeenCalledTimes(1)
    clearLinkPreviewCache()
    vi.mocked(api).mockClear().mockResolvedValue(null)
    for (let index = 0; index <= 300; index++) await fetchLinkPreview(`https://cache.example/${index}`)
    expect(api).toHaveBeenCalledTimes(301)
    await fetchLinkPreview('https://cache.example/1')
    expect(api).toHaveBeenCalledTimes(301)
    await fetchLinkPreview('https://cache.example/0')
    expect(api).toHaveBeenCalledTimes(302)
  })
