import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import LinkPreviewCard from './LinkPreviewCard.vue'
import { fetchLinkPreview } from '../lib/linkPreview'
import type { LinkPreview } from '../types/domain'

vi.mock('../lib/linkPreview', () => ({ fetchLinkPreview: vi.fn() }))
enableAutoUnmount(afterEach)
beforeEach(() => vi.mocked(fetchLinkPreview).mockReset())

const full: LinkPreview = { url: 'https://www.example.org/article', title: 'Article title', description: 'Description', image: 'https://example.org/image.png?q=one two', site_name: 'Publisher' }

const youtube: LinkPreview = {
  url: 'https://www.youtube.com/watch?v=dQw4w9WgXcQ',
  title: 'Never Gonna Give You Up',
  site_name: 'YouTube',
  author: 'Rick Astley',
  author_url: 'https://www.youtube.com/@RickAstleyYT',
  image: 'https://i.ytimg.com/vi/dQw4w9WgXcQ/hqdefault.jpg',
  embed: { provider: 'youtube', url: 'https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ' }
}

const steam: LinkPreview = {
  url: 'https://store.steampowered.com/app/3572800/',
  title: 'Duo Quest',
  description: 'Your bond is your greatest weapon!',
  site_name: 'Steam',
  color: '#1b2838',
  image: 'https://shared.akamai.steamstatic.com/header.jpg',
  fields: [{ key: 'price', name: 'Price', value: '$14.99' }, { key: 'recommendations', name: 'Recommendations', value: '141' }, { name: 'Release', value: 'Sep 2026' }]
}

async function mounted(preview: LinkPreview | null, url = preview?.url ?? 'https://example.org/') {
  vi.mocked(fetchLinkPreview).mockResolvedValue(preview)
  const w = mount(LinkPreviewCard, { props: { url } })
  await flushPromises()
  return w
}

describe('LinkPreviewCard', () => {
  it('stays absent when a preview is unavailable', async () => {
    vi.mocked(fetchLinkPreview).mockResolvedValue(null)
    const w = mount(LinkPreviewCard, { props: { url: full.url } })
    expect(w.find('article').exists()).toBe(false)
    await flushPromises()
    expect(fetchLinkPreview).toHaveBeenCalledWith(full.url)
    expect(w.find('article').exists()).toBe(false)
  })

  it('renders fetched metadata safely and removes broken images', async () => {
    const w = await mounted(full, 'https://example.org/input')
    const title = w.get('[data-testid="link-preview-title"]')
    expect(title.attributes('href')).toBe(full.url)
    expect(title.attributes('rel')).toBe('noopener noreferrer')
    expect(title.attributes('target')).toBe('_blank')
    expect(title.text()).toBe('Article title')
    expect(w.get('[data-testid="link-preview-provider"]').text()).toBe('Publisher')
    expect(w.find('p').text()).toBe('Description')
    expect(w.find('img').attributes('src')).toBe(`/api/link-preview/image?url=${encodeURIComponent(full.image ?? '')}`)
    await w.find('img').trigger('error')
    expect(w.find('img').exists()).toBe(false)
    expect(w.text()).toContain('Article title')
  })

  it.each([
    { preview: { url: '', title: '' }, url: 'https://www.example.org/input', expected: 'example.org' },
    { preview: { url: 'https://www.example.net/page', title: '' }, url: 'https://example.org/input', expected: 'example.net' },
    { preview: { url: '', title: '' }, url: 'invalid url', expected: '' }
  ] satisfies { preview: LinkPreview; url: string; expected: string }[])('names the site from the link when the page gives none: $url', async ({ preview, url, expected }) => {
    const w = await mounted(preview, url)
    expect(w.get('[data-testid="link-preview-provider"]').text()).toBe(expected)
    expect(w.find('p').exists()).toBe(false)
    expect(w.find('img').exists()).toBe(false)
  })

  it('shows provider, channel, title and a thumbnail with a play button for a video', async () => {
    const w = await mounted(youtube)
    expect(w.get('[data-testid="link-preview-provider"]').text()).toBe('YouTube')
    const author = w.get('[data-testid="link-preview-author"] a')
    expect(author.text()).toBe('Rick Astley')
    expect(author.attributes('href')).toBe('https://www.youtube.com/@RickAstleyYT')
    expect(w.get('[data-testid="link-preview-title"]').attributes('href')).toBe(youtube.url)
    expect(w.get('[data-testid="link-preview-play"] img').attributes('src')).toContain('/api/link-preview/image?url=')
    expect(w.get('[data-testid="link-preview-play"]').attributes('aria-label')).toContain('Never Gonna Give You Up')
  })

  it('loads no third-party player before the click, and a sandboxed no-cookie one after', async () => {
    const w = await mounted(youtube)
    expect(w.find('iframe').exists()).toBe(false)
    expect(w.html()).not.toContain('youtube-nocookie')
    await w.get('[data-testid="link-preview-play"]').trigger('click')
    const frame = w.get('iframe')
    expect(frame.attributes('src')).toBe('https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ?autoplay=1')
    expect(frame.attributes('sandbox')).toBe('allow-scripts allow-same-origin allow-presentation allow-popups allow-popups-to-escape-sandbox')
    expect(frame.attributes('sandbox')).not.toContain('allow-top-navigation')
    expect(frame.attributes('referrerpolicy')).toBe('strict-origin-when-cross-origin')
    expect(frame.attributes('allow')).not.toMatch(/camera|microphone|geolocation/)
    expect(w.find('[data-testid="link-preview-play"]').exists()).toBe(false)
  })

  it('falls back to the plain link when the page refuses the player', async () => {
    const w = await mounted(youtube)
    await w.get('[data-testid="link-preview-play"]').trigger('click')
    expect(w.find('iframe').exists()).toBe(true)
    const violation = Object.assign(new Event('securitypolicyviolation'), { blockedURI: 'https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ?autoplay=1' })
    document.dispatchEvent(violation)
    await flushPromises()
    expect(w.find('iframe').exists()).toBe(false)
    expect(w.find('[data-testid="link-preview-play"]').exists()).toBe(false)
    expect(w.find('[data-testid="link-preview-blocked"]').exists()).toBe(true)
    expect(w.get('[data-testid="link-preview-media"]').attributes('href')).toBe(youtube.url)
  })

  it('ignores violations of other addresses', async () => {
    const w = await mounted(youtube)
    await w.get('[data-testid="link-preview-play"]').trigger('click')
    document.dispatchEvent(Object.assign(new Event('securitypolicyviolation'), { blockedURI: 'https://tracker.example/x.js' }))
    await flushPromises()
    expect(w.find('iframe').exists()).toBe(true)
    expect(w.find('[data-testid="link-preview-blocked"]').exists()).toBe(false)
  })

  it('never plays an address outside the allowlist', async () => {
    const w = await mounted({ ...youtube, embed: { provider: 'youtube', url: 'https://evil.example/embed/dQw4w9WgXcQ' } })
    expect(w.find('[data-testid="link-preview-play"]').exists()).toBe(false)
    expect(w.find('iframe').exists()).toBe(false)
    expect(w.get('[data-testid="link-preview-title"]').text()).toBe('Never Gonna Give You Up')
  })

  it('shows fields such as price and recommendations, and the page colour as an accent', async () => {
    const w = await mounted(steam)
    expect(w.get('[data-testid="link-preview-provider"]').text()).toBe('Steam')
    const fields = w.get('[data-testid="link-preview-fields"]')
    expect(fields.text()).toContain('$14.99')
    expect(fields.text()).toContain('141')
    expect(fields.text()).toContain('Release')
    expect(fields.text()).toContain('Sep 2026')
    expect(w.get('article').element.style.borderLeftColor).not.toBe('')
    expect(w.find('[data-testid="link-preview-media"] img').exists()).toBe(true)
  })

  it('ignores colours, links and authors that are not plain values', async () => {
    const w = await mounted({ ...full, color: 'url(x)', author: 'Eve', author_url: 'javascript:alert(1)', url: 'javascript:alert(2)' }, 'https://example.org/safe')
    expect(w.get('article').element.style.borderLeftColor).toBe('')
    expect(w.find('[data-testid="link-preview-author"] a').exists()).toBe(false)
    expect(w.get('[data-testid="link-preview-author"]').text()).toBe('Eve')
    expect(w.get('[data-testid="link-preview-title"]').attributes('href')).toBe('https://example.org/safe')
  })

  it('puts a small thumbnail beside the text for compact cards', async () => {
    const w = await mounted({ ...full, compact: true })
    expect(w.find('[data-testid="link-preview-media"]').exists()).toBe(false)
    expect(w.find('img').classes()).toContain('object-cover')
    expect(w.findAll('img')).toHaveLength(1)
  })
})
