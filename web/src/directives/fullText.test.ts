import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { defineComponent, ref } from 'vue'
import { fullText } from './fullText'

// jsdom does no layout, so each case states the sizes the browser would measure.
function sized(el: HTMLElement, scrollWidth: number, clientWidth: number) {
  Object.defineProperty(el, 'scrollWidth', { configurable: true, value: scrollWidth })
  Object.defineProperty(el, 'clientWidth', { configurable: true, value: clientWidth })
}

function row(name: string) {
  const text = ref(name)
  const wrapper = mount(
    defineComponent({
      directives: { fullText },
      setup: () => ({ text }),
      template: '<button type="button"><span v-full-text class="truncate">{{ text }}</span></button>',
    }),
    { attachTo: document.body },
  )
  return { wrapper, text, span: wrapper.get('span').element, button: wrapper.get('button') }
}

describe('v-full-text', () => {
  it('shows the whole name as a title when the pointer reaches a cut name', async () => {
    const { span, button, wrapper } = row('a-channel-with-a-very-long-name')
    sized(span, 300, 120)
    await button.trigger('pointerenter')
    expect(span.getAttribute('title')).toBe('a-channel-with-a-very-long-name')
    expect(span.hasAttribute('data-full-text')).toBe(true)
    wrapper.unmount()
  })

  it('gives a name that fits no title', async () => {
    const { span, button, wrapper } = row('general')
    sized(span, 60, 120)
    await button.trigger('pointerenter')
    expect(span.hasAttribute('title')).toBe(false)
    wrapper.unmount()
  })

  it('follows keyboard focus and a renamed text', async () => {
    const { span, button, text, wrapper } = row('first-very-long-name')
    sized(span, 300, 120)
    await button.trigger('focusin')
    expect(span.getAttribute('title')).toBe('first-very-long-name')
    text.value = 'renamed-and-still-too-long'
    await wrapper.vm.$nextTick()
    expect(span.getAttribute('title')).toBe('renamed-and-still-too-long')
    sized(span, 60, 120)
    await button.trigger('pointerenter')
    expect(span.hasAttribute('title')).toBe(false)
    wrapper.unmount()
  })
})
