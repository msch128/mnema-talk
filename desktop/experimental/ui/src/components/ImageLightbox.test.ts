import { afterEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import ImageLightbox from './ImageLightbox.vue'
let wrapper: ReturnType<typeof mount<typeof ImageLightbox>>
afterEach(() => { wrapper?.unmount(); document.body.innerHTML = '' })
describe('ImageLightbox', () => {
  it('renders the image, keeps image clicks open, and closes on each dismissal path', async () => {
    wrapper = mount(ImageLightbox, { props: { src: '/api/media/synthetic.png' }, attachTo: document.body })
    await nextTick(); await nextTick()
    expect(wrapper.get('img').attributes('src')).toBe('/api/media/synthetic.png')
    expect(document.activeElement).toBe(wrapper.get('button').element)
    await wrapper.get('img').trigger('click')
    expect(wrapper.emitted('close')).toBeUndefined()
    await wrapper.get('button').trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)
    await wrapper.get('[role="dialog"]').trigger('click')
    await wrapper.get('button').trigger('keydown', { key: 'Escape' })
    expect(wrapper.emitted('close')).toHaveLength(3)
  })
})
