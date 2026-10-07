import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import LinkPreviewCard from './LinkPreviewCard.vue'
import { fetchLinkPreview } from '../lib/linkPreview'
import type { LinkPreview } from '../types/domain'

vi.mock('../lib/linkPreview', () => ({ fetchLinkPreview: vi.fn() }))
enableAutoUnmount(afterEach)
beforeEach(() => vi.mocked(fetchLinkPreview).mockReset())

const full: LinkPreview = { url: 'https://www.example.org/article', title: 'Article title', description: 'Description', image: 'https://example.org/image.png?q=one two', site_name: 'Publisher' }

describe('LinkPreviewCard', () => {
  it('stays absent when a preview is unavailable', async () => {
    vi.mocked(fetchLinkPreview).mockResolvedValue(null)
    const w = mount(LinkPreviewCard, { props: { url: full.url } })
    expect(w.find('a').exists()).toBe(false)
    await flushPromises()
    expect(fetchLinkPreview).toHaveBeenCalledWith(full.url)
    expect(w.find('a').exists()).toBe(false)
  })

  it('renders fetched metadata safely and removes broken images', async () => {
    vi.mocked(fetchLinkPreview).mockResolvedValue(full)
    const w = mount(LinkPreviewCard, { props: { url: 'https://example.org/input' } })
    await flushPromises()
    expect(w.attributes('href')).toBe(full.url)
    expect(w.attributes('rel')).toBe('noopener noreferrer')
    expect(w.attributes('target')).toBe('_blank')
    expect(w.text()).toContain('Publisher')
    expect(w.text()).toContain('Article title')
    expect(w.find('p').text()).toBe('Description')
    expect(w.find('img').attributes('src')).toBe(`/api/link-preview/image?url=${encodeURIComponent(full.image ?? '')}`)
    expect(w.find('img').attributes('alt')).toBe('Article title')
    await w.find('img').trigger('error')
    expect(w.find('img').exists()).toBe(false)
    expect(w.text()).toContain('Article title')
  })

  it.each([
    { preview: { url: '', title: '' }, url: 'https://www.example.org/input', expected: 'example.org' },
    { preview: { url: 'https://www.example.net/page', title: '' }, url: 'https://example.org/input', expected: 'example.net' },
    { preview: { url: '', title: '' }, url: 'invalid url', expected: '' },
    { preview: { url: '', title: '', image: 'https://example.org/pic' }, url: 'https://example.org/', expected: 'example.org' }
  ] satisfies { preview: LinkPreview; url: string; expected: string }[])('handles missing metadata for $url', async ({ preview, url, expected }) => {
    vi.mocked(fetchLinkPreview).mockResolvedValue(preview)
    const w = mount(LinkPreviewCard, { props: { url } })
    await flushPromises()
    expect(w.attributes('href')).toBe(preview.url || url)
    expect(w.find('span').text()).toBe(expected)
    expect(w.find('p').exists()).toBe(false)
    if (preview.image) expect(w.find('img').attributes('alt')).toBe('')
    else expect(w.find('img').exists()).toBe(false)
  })
})
