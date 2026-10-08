


import { describe, it, expect } from 'vitest'
import { defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'
import MessageAttachments from './MessageAttachments.vue'

const image = { is_deleted: false, id: "00000000-0000-4000-8000-0000000003f6", url: "/m/00000000-0000-4000-8000-0000000003f6", mime_type: 'image/png', original_filename: 'shot.png', size_bytes: 1048576 }
const video = { is_deleted: false, id: "00000000-0000-4000-8000-0000000003f7", url: "/m/00000000-0000-4000-8000-0000000003f7", mime_type: 'video/mp4', original_filename: 'clip.mp4', size_bytes: 2097152 }
const file = { is_deleted: false, id: "00000000-0000-4000-8000-0000000003f8", url: "/m/00000000-0000-4000-8000-0000000003f8", mime_type: 'application/pdf', original_filename: 'doc.pdf', size_bytes: 512 }

describe('MessageAttachments', () => {
  it('accepts its default empty list and unknown MIME with zero size', () => {
    expect(mount(MessageAttachments).find('[data-attachment]').exists()).toBe(false)
    const w = mount(MessageAttachments, { props: { attachments: [{ ...file, mime_type: '', size_bytes: 0 }] } })
    expect(w.find('[data-attachment="file"]').exists()).toBe(true)
    expect(w.text()).toContain('0.00')
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
    expect(w.emitted('open-image')!).toEqual([["/m/00000000-0000-4000-8000-0000000003f6"]])
  })

  it.each(['chat', 'reply', 'root'] as const)('plays videos in the %s variant', variant => {
    const w = mount(MessageAttachments, { props: { attachments: [video], variant } })
    const el = w.find('video')!
    expect(el.exists()).toBe(true)
    expect(el.attributes('src')).toBe("/m/00000000-0000-4000-8000-0000000003f7")
    expect(el.attributes('controls')).toBeDefined()
  })

  it('links every file with its size', () => {
    const w = mount(MessageAttachments, { props: { attachments: [image, video, file] } })
    const links = w.findAll('a')
    expect(links.map(a => a.text())).toEqual(['shot.png', 'clip.mp4', 'doc.pdf'])
    expect(w.text()).toContain('2.00')
    expect(w.find('[data-attachment="file"] img, [data-attachment="file"] video')!.exists()).toBe(false)
  })
})
