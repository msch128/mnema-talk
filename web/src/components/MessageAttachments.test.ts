


import { describe, it, expect, beforeEach, afterEach } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'
import MessageAttachments from './MessageAttachments.vue'
import { closeViewer, viewerState } from '../composables/useMediaViewer'
import { messageFixture } from '../test-fixtures.fixture'

const image = { is_deleted: false, id: "00000000-0000-4000-8000-0000000003f6", url: "/m/00000000-0000-4000-8000-0000000003f6", mime_type: 'image/png', original_filename: 'shot.png', size_bytes: 1048576 }
const video = { is_deleted: false, id: "00000000-0000-4000-8000-0000000003f7", url: "/m/00000000-0000-4000-8000-0000000003f7", mime_type: 'video/mp4', original_filename: 'clip.mp4', size_bytes: 2097152 }
const file = { is_deleted: false, id: "00000000-0000-4000-8000-0000000003f8", url: "/m/00000000-0000-4000-8000-0000000003f8", mime_type: 'application/pdf', original_filename: 'doc.pdf', size_bytes: 512 }

const images = (n: number) => Array.from({ length: n }, (_, i) => ({ ...image, id: `00000000-0000-4000-8000-0000000004${String(i).padStart(2, '0')}`, url: `/m/img${i}`, original_filename: `img${i}.png` }))

beforeEach(() => setActivePinia(createPinia()))
afterEach(closeViewer)

describe('MessageAttachments', () => {
  it('accepts its default empty list and unknown MIME with zero size', () => {
    expect(mount(MessageAttachments).find('[data-attachment]').exists()).toBe(false)
    const w = mount(MessageAttachments, { props: { attachments: [{ ...file, mime_type: '', size_bytes: 0 }] } })
    expect(w.find('[data-attachment="file"]').exists()).toBe(true)
    expect(w.text()).toContain('0 B')
  })

  it('uses the chat layout when an untyped parent supplies an unknown variant', () => {
    // Dynamic component boundaries can still originate in external JS. Keep
    // the fixture contract typed and model only the malformed layout option.
    const runtimeProps: Record<string, unknown> = { attachments: [image], variant: 'future-layout' }
    const parent = defineComponent({ render: () => h(MessageAttachments, runtimeProps) })
    const w = mount(parent)
    expect(w.find('img').classes()).toContain('max-h-80')
    expect(w.find('[data-attachment="image"]').classes()).toContain('max-w-md')
  })

  it('renders nothing without attachments', () => {
    const w = mount(MessageAttachments, { props: { attachments: [] } })
    expect(w.find('[data-attachment]')!.exists()).toBe(false)
  })

  it('opens images through a button (keyboard reachable)', async () => {
    const w = mount(MessageAttachments, { props: { attachments: [image] } })
    const btn = w.find('[data-attachment="image"] button')!
    expect(btn.exists()).toBe(true)
    expect(btn.find('img')!.attributes('alt')).toBe('shot.png')
    await btn.trigger('click')
    expect(viewerState.open).toBe(true)
    expect(viewerState.items).toEqual([{ src: image.url, name: 'shot.png', sizeBytes: 1048576, mimeType: 'image/png' }])
    expect(viewerState.message).toBeNull()
  })

  it.each(['chat', 'reply', 'root'] as const)('plays videos in the %s variant', variant => {
    const w = mount(MessageAttachments, { props: { attachments: [video], variant } })
    const el = w.find('video')!
    expect(el.exists()).toBe(true)
    expect(el.attributes('src')).toBe("/m/00000000-0000-4000-8000-0000000003f7")
    expect(el.attributes('controls')).toBeDefined()
  })

  it('shows a chat image on its own, without the card or file row', () => {
    const w = mount(MessageAttachments, { props: { attachments: [image] } })
    const el = w.find('[data-attachment="image"]')
    expect(el.classes()).not.toContain('border')
    expect(el.find('a').exists()).toBe(false)
    expect(el.find('img').attributes('title')).toBe('shot.png')
    expect(el.find('img').classes()).toEqual(expect.arrayContaining(['h-auto', 'w-auto', 'min-h-24', 'object-cover']))
  })

  it.each(['reply', 'root'] as const)('keeps the image card and file row in the %s variant', variant => {
    const w = mount(MessageAttachments, { props: { attachments: [image], variant } })
    expect(w.find('[data-attachment="image"] a').text()).toBe('shot.png')
  })

  it('links every non-image file with its size', () => {
    const w = mount(MessageAttachments, { props: { attachments: [image, video, file] } })
    const links = w.findAll('a')
    expect(links.map(a => a.text())).toEqual(['clip.mp4', 'doc.pdf'])
    expect(w.text()).toContain('2 MB')
    expect(w.find('[data-attachment="file"] img, [data-attachment="file"] video')!.exists()).toBe(false)
  })

  it.each([
    [2, [2]],
    [3, [2]],
    [4, [2, 2]],
    [5, [2, 3]],
    [7, [2, 2, 3]],
    [10, [2, 2, 3, 3]]
  ])('lays out %i images in rows of %j cells', (n, rows) => {
    const w = mount(MessageAttachments, { props: { attachments: images(n) } })
    expect(w.find('[data-gallery]').attributes('data-gallery')).toBe(String(n))
    expect(w.findAll('[data-gallery-row]').map(r => r.element.children.length)).toEqual(rows)
    expect(w.findAll('[data-gallery-cell]')).toHaveLength(n)
    expect(w.find('[data-gallery]').classes()).toContain('max-w-md')
    for (const img of w.findAll('[data-gallery] img')) {
      expect(img.attributes('loading')).toBe('lazy')
      expect(img.classes()).toContain('object-cover')
    }
    for (const row of w.findAll('[data-gallery-row]')) expect(row.classes().some(c => c.startsWith('aspect-'))).toBe(true)
  })

  it('stacks two images beside one large image for three', () => {
    const w = mount(MessageAttachments, { props: { attachments: images(3) } })
    expect(w.findAll('[data-stack] [data-gallery-cell]')).toHaveLength(2)
  })

  it('keeps a single image out of the mosaic', () => {
    const w = mount(MessageAttachments, { props: { attachments: images(1) } })
    expect(w.find('[data-gallery]').exists()).toBe(false)
    expect(w.find('[data-attachment="image"] img').exists()).toBe(true)
  })

  it('narrows the mosaic in replies and fills the root card', () => {
    expect(mount(MessageAttachments, { props: { attachments: images(2), variant: 'reply' } }).find('[data-gallery]').classes()).toContain('max-w-xs')
    expect(mount(MessageAttachments, { props: { attachments: images(2), variant: 'root' } }).find('[data-gallery]').classes()).not.toContain('max-w-md')
  })

  it('shows images first, then videos inline, then files, in server order', () => {
    const w = mount(MessageAttachments, { props: { attachments: [file, video, ...images(2)] } })
    const order = w.findAll('[data-gallery], [data-attachment]').map(e => e.attributes('data-gallery') ? 'gallery' : e.attributes('data-attachment'))
    expect(order).toEqual(['gallery', 'video', 'file'])
    expect(w.find('video').attributes('controls')).toBeDefined()
  })

  it('opens the viewer with every image of the message at the clicked index, with message details', async () => {
    const msg = messageFixture({ id: 'm1', channel_id: 'c1', display_name: 'Ada', avatar_url: '/a.png', created_at: '2026-01-01T10:00:00Z', attachments: images(4) })
    const w = mount(MessageAttachments, { props: { attachments: [video, ...msg.attachments], message: msg } })
    await w.findAll('[data-gallery-cell]')[2]!.trigger('click')
    expect(viewerState.items.map(i => i.src)).toEqual(['/m/img0', '/m/img1', '/m/img2', '/m/img3'])
    expect(viewerState.index).toBe(2)
    expect(viewerState.message).toMatchObject({ id: 'm1', channelId: 'c1', senderName: 'Ada', senderAvatarUrl: '/a.png', sentAt: '2026-01-01T10:00:00Z' })
  })

  it('shows a calm expand chip that is not part of the accessible name', () => {
    const w = mount(MessageAttachments, { props: { attachments: images(2) } })
    const chip = w.find('[data-gallery-cell] span')
    expect(chip.attributes('aria-hidden')).toBe('true')
    expect(chip.classes()).toEqual(expect.arrayContaining(['opacity-0', 'group-hover:opacity-100', 'group-focus-visible:opacity-100']))
  })
})
