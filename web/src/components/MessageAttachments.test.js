import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import MessageAttachments from './MessageAttachments.vue'

const image = { id: 'i', url: '/m/i', mime_type: 'image/png', original_filename: 'shot.png', size_bytes: 1048576 }
const video = { id: 'v', url: '/m/v', mime_type: 'video/mp4', original_filename: 'clip.mp4', size_bytes: 2097152 }
const file = { id: 'f', url: '/m/f', mime_type: 'application/pdf', original_filename: 'doc.pdf', size_bytes: 512 }

describe('MessageAttachments', () => {
  it('renders nothing without attachments', () => {
    const w = mount(MessageAttachments, { props: { attachments: [] } })
    expect(w.find('[data-attachment]').exists()).toBe(false)
  })

  it('opens images through a button (keyboard reachable)', async () => {
    const w = mount(MessageAttachments, { props: { attachments: [image] } })
    const btn = w.find('[data-attachment="image"] button')
    expect(btn.exists()).toBe(true)
    expect(btn.find('img').attributes('alt')).toBe('shot.png')
    await btn.trigger('click')
    expect(w.emitted('open-image')).toEqual([['/m/i']])
  })

  it.each(['chat', 'reply', 'root'])('plays videos in the %s variant', variant => {
    const w = mount(MessageAttachments, { props: { attachments: [video], variant } })
    const el = w.find('video')
    expect(el.exists()).toBe(true)
    expect(el.attributes('src')).toBe('/m/v')
    expect(el.attributes('controls')).toBeDefined()
  })

  it('links every file with its size', () => {
    const w = mount(MessageAttachments, { props: { attachments: [image, video, file] } })
    const links = w.findAll('a')
    expect(links.map(a => a.text())).toEqual(['shot.png', 'clip.mp4', 'doc.pdf'])
    expect(w.text()).toContain('2.00')
    expect(w.find('[data-attachment="file"] img, [data-attachment="file"] video').exists()).toBe(false)
  })
})
