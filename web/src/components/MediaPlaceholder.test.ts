// The stand-ins for a picture that cannot be shown: deleted media in the
// message, the avatar, the banner, the emoji, and a failure that may pass.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { setLocale } from '../i18n'
import { resetMediaStatus } from '../lib/mediaStatus'
import AttachmentImage from './AttachmentImage.vue'
import EmojiGlyph from './EmojiGlyph.vue'
import MediaImage from './MediaImage.vue'
import MessageAttachments from './MessageAttachments.vue'
import UserAvatar from './UserAvatar.vue'

vi.mock('../stores/emoji', () => ({
  prefersReducedMotion: () => false,
  useEmojiStore: () => ({ byId: new Map([['11111111-1111-4111-8111-111111111111', { id: '11111111-1111-4111-8111-111111111111', name: 'wave', animated: false, url: '/api/emoji/wave.png' }]]) })
}))

const id = '00000000-0000-4000-8000-0000000003f6'
const image = { is_deleted: false, id, url: `/api/media/${id}`, mime_type: 'image/png', original_filename: 'shot.png', size_bytes: 1024 }

function serverAnswers(status: number) {
  const fetchMock = vi.fn(() => Promise.resolve(new Response('', { status })))
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

beforeEach(() => {
  setLocale('en')
  resetMediaStatus()
})
afterEach(() => vi.unstubAllGlobals())

describe('a deleted attachment', () => {
  it('shows the placeholder at once when the message says the file is deleted, without asking the server', () => {
    const fetchMock = serverAnswers(410)
    const w = mount(MessageAttachments, { props: { attachments: [{ ...image, is_deleted: true }] } })
    expect(w.find('img').exists()).toBe(false)
    expect(w.find('button').exists()).toBe(false)
    expect(w.get('[data-media-placeholder="gone"]').text()).toBe('This image was deleted')
    expect(w.get('[data-media-placeholder="gone"]').attributes('role')).toBe('img')
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('shows a deleted image inside the mosaic as a compact box in its cell', () => {
    const second = { ...image, id: '00000000-0000-4000-8000-0000000003f9', url: '/api/media/second' }
    const w = mount(MessageAttachments, { props: { attachments: [{ ...image, is_deleted: true }, second] } })
    expect(w.findAll('[data-gallery-cell]')).toHaveLength(1)
    expect(w.get('[data-media-placeholder="gone"]').attributes('title')).toBe('This image was deleted')
  })

  it.each([['chat', 'h-40'], ['reply', 'h-24'], ['root', 'h-28']] as const)('keeps a size of its own in the %s layout', (variant, height) => {
    const w = mount(MessageAttachments, { props: { variant, attachments: [{ ...image, is_deleted: true }] } })
    expect(w.get('[data-media-placeholder]').classes()).toContain(height)
  })

  it('replaces an image the server no longer has (410) after the failed load', async () => {
    const fetchMock = serverAnswers(410)
    const w = mount(AttachmentImage, { props: { attachment: image } })
    await w.get('img').trigger('error')
    await flushPromises()
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(w.find('img').exists()).toBe(false)
    expect(w.get('[data-media-placeholder="gone"]').text()).toBe('This image was deleted')
  })

  it('offers a retry instead of "deleted" when the server only failed, and loads the image again', async () => {
    serverAnswers(503)
    const w = mount(AttachmentImage, { props: { attachment: image } })
    await w.get('img').trigger('error')
    await flushPromises()
    expect(w.find('[data-media-placeholder="gone"]').exists()).toBe(false)
    const retry = w.get('[data-media-placeholder="failed"]')
    expect(retry.text()).toContain('The image could not be loaded')
    expect(retry.text()).toContain('Try again')
    await retry.trigger('click')
    expect(w.get('img').attributes('src')).toBe(image.url)
    expect(w.find('[data-media-placeholder]').exists()).toBe(false)
  })

  it('does not open anything from the placeholder and leaves the file row alone', async () => {
    const w = mount(MessageAttachments, { props: { variant: 'reply', attachments: [{ ...image, is_deleted: true }] } })
    expect(w.emitted('open-image')).toBeUndefined()
    expect(w.text()).toContain('shot.png')
  })
})

describe('an avatar whose picture is gone', () => {
  it('falls back to the initial', async () => {
    serverAnswers(404)
    const w = mount(UserAvatar, { props: { user: { id: 'u1', username: 'fox', display_name: 'Fox', avatar_url: '/api/media/gone' } as never } })
    expect(w.find('img').exists()).toBe(true)
    await w.get('img').trigger('error')
    await flushPromises()
    expect(w.find('img').exists()).toBe(false)
    expect(w.text()).toBe('F')
  })

  it('knows a gone picture the next time, without a second request', async () => {
    const fetchMock = serverAnswers(410)
    const user = { id: 'u1', username: 'fox', display_name: 'Fox', avatar_url: '/api/media/gone' } as never
    const first = mount(UserAvatar, { props: { user } })
    await first.get('img').trigger('error')
    await flushPromises()
    const second = mount(UserAvatar, { props: { user } })
    expect(second.find('img').exists()).toBe(false)
    expect(second.text()).toBe('F')
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('shows the picture again when the member picks a new one', async () => {
    serverAnswers(410)
    const w = mount(UserAvatar, { props: { user: { id: 'u1', username: 'fox', display_name: 'Fox', avatar_url: '/api/media/gone' } as never } })
    await w.get('img').trigger('error')
    await flushPromises()
    await w.setProps({ user: { id: 'u1', username: 'fox', display_name: 'Fox', avatar_url: '/api/media/new' } as never })
    expect(w.get('img').attributes('src')).toBe('/api/media/new')
  })
})

describe('a custom emoji whose picture is gone', () => {
  it('is written as :name:', async () => {
    serverAnswers(410)
    const w = mount(EmojiGlyph, { props: { value: '<:wave:11111111-1111-4111-8111-111111111111>' } })
    await w.get('img').trigger('error')
    await flushPromises()
    expect(w.find('img').exists()).toBe(false)
    expect(w.text()).toBe(':wave:')
  })
})

describe('a custom emoji that failed to load', () => {
  it('has no "deleted" title and loads again on click', async () => {
    serverAnswers(503)
    const w = mount(EmojiGlyph, { props: { value: '<:wave:11111111-1111-4111-8111-111111111111>' } })
    await w.get('img').trigger('error')
    await flushPromises()
    const retry = w.get('button')
    expect(retry.text()).toBe(':wave:')
    expect(retry.attributes('title')).toBe('Try again')
    await retry.trigger('click')
    expect(w.find('img').exists()).toBe(true)
  })

  it('titles a deleted one', async () => {
    serverAnswers(410)
    const w = mount(EmojiGlyph, { props: { value: '<:wave:11111111-1111-4111-8111-111111111111>' } })
    await w.get('img').trigger('error')
    await flushPromises()
    expect(w.get('span').attributes('title')).toBe('This image was deleted')
    expect(w.find('button').exists()).toBe(false)
  })
})

describe('MediaImage', () => {
  it('lets the caller stand in for a picture that is gone, such as the default banner', async () => {
    serverAnswers(410)
    const w = mount(MediaImage, {
      props: { src: '/api/media/banner' },
      attrs: { alt: '', 'data-testid': 'banner' },
      slots: { fallback: '<div data-testid="default-banner"></div>' }
    })
    expect(w.get('img').attributes('data-testid')).toBe('banner')
    await w.get('img').trigger('error')
    await flushPromises()
    expect(w.find('img').exists()).toBe(false)
    expect(w.find('[data-testid="default-banner"]').exists()).toBe(true)
  })

  it('draws the compact box with its words as a tooltip and name', () => {
    const w = mount(MediaImage, { props: { src: '/x', deleted: true, compact: true } })
    const box = w.get('[data-media-placeholder="gone"]')
    expect(box.text()).toBe('')
    expect(box.attributes('title')).toBe('This image was deleted')
    expect(box.attributes('aria-label')).toBe('This image was deleted')
  })
})
