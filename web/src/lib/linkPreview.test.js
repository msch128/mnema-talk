import { describe, it, expect, vi, beforeEach } from 'vitest'

vi.mock('./api', () => ({ api: vi.fn() }))
import { api } from './api'
import { fetchLinkPreview, clearLinkPreviewCache } from './linkPreview'

beforeEach(() => {
  clearLinkPreviewCache()
  api.mockReset()
})

describe('fetchLinkPreview', () => {
  it('requests each url once, however often it is asked', async () => {
    api.mockResolvedValue({ title: 'Titel', url: 'https://a.de' })
    const [a, b] = await Promise.all([fetchLinkPreview('https://a.de'), fetchLinkPreview('https://a.de')])
    await fetchLinkPreview('https://a.de')
    expect(api).toHaveBeenCalledTimes(1)
    expect(a.title).toBe('Titel')
    expect(b).toBe(a)
  })

  it('remembers "no preview" (204) and definitive errors', async () => {
    api.mockResolvedValueOnce(null)
    expect(await fetchLinkPreview('https://none.de')).toBeNull()
    expect(await fetchLinkPreview('https://none.de')).toBeNull()
    expect(api).toHaveBeenCalledTimes(1)
  })

  it('retries after a rate limit', async () => {
    api.mockRejectedValueOnce({ status: 429 })
    expect(await fetchLinkPreview('https://r.de')).toBeNull()
    api.mockResolvedValueOnce({ title: 'ok' })
    expect((await fetchLinkPreview('https://r.de')).title).toBe('ok')
    expect(api).toHaveBeenCalledTimes(2)
  })
})
