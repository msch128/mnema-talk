import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { defineComponent, ref } from 'vue'
import { maxChars } from './maxChars'

function field(limit: number) {
  const value = ref('')
  const wrapper = mount(
    defineComponent({
      directives: { maxChars },
      setup: () => ({ value, limit }),
      template: '<input v-model="value" v-max-chars="limit" />',
    }),
    { attachTo: document.body },
  )
  const input = wrapper.find('input')
  const type = async (text: string) => {
    await input.setValue(text)
    return value.value
  }
  return { type, input, wrapper }
}

describe('v-max-chars', () => {
  it('lets 24 emoji through a limit of 24 (maxlength would stop at 12)', async () => {
    const { type } = field(24)
    expect(await type('😂'.repeat(24))).toBe('😂'.repeat(24))
  })

  it('cuts a longer paste to the limit at a character boundary', async () => {
    const { type } = field(24)
    expect(await type('😂'.repeat(30))).toBe('😂'.repeat(24))
  })

  it('does not split a ZWJ sequence', async () => {
    const { type } = field(5)
    expect(await type('ab👨‍👩‍👧c')).toBe('ab')
  })

  it('counts CJK characters once each', async () => {
    const { type } = field(4)
    expect(await type('你好世界你好')).toBe('你好世界')
  })

  it('leaves text being composed alone until it ends', async () => {
    const { input, wrapper } = field(2)
    await input.trigger('compositionstart')
    await input.setValue('こんにちは')
    expect((input.element as HTMLInputElement).value).toBe('こんにちは')
    await input.trigger('compositionend')
    expect((input.element as HTMLInputElement).value).toBe('こん')
    wrapper.unmount()
  })

  it('follows a changed limit and exposes it as data-max-chars', async () => {
    const limit = ref(3)
    const value = ref('')
    const wrapper = mount(
      defineComponent({
        directives: { maxChars },
        setup: () => ({ value, limit }),
        template: '<input v-model="value" v-max-chars="limit" />',
      }),
    )
    const input = wrapper.find('input')
    expect(input.attributes('data-max-chars')).toBe('3')
    limit.value = 5
    await wrapper.vm.$nextTick()
    expect(input.attributes('data-max-chars')).toBe('5')
    await input.setValue('abcdefg')
    expect(value.value).toBe('abcde')
  })

  it.each([0, -2, Number.NaN])('does not limit anything for the unusable limit %s', async (bad) => {
    const value = ref('')
    const wrapper = mount(
      defineComponent({
        directives: { maxChars },
        setup: () => ({ value, bad }),
        template: '<input v-model="value" v-max-chars="bad" />',
      }),
    )
    const input = wrapper.find('input')
    expect(input.attributes('data-max-chars')).toBeUndefined()
    await input.setValue('x'.repeat(500))
    expect(value.value).toHaveLength(500)
  })

  it('keeps the caret inside a text that was cut, in a textarea too', async () => {
    const value = ref('')
    const wrapper = mount(
      defineComponent({
        directives: { maxChars },
        setup: () => ({ value }),
        template: '<textarea v-model="value" v-max-chars="4" />',
      }),
      { attachTo: document.body },
    )
    const area = wrapper.find('textarea').element
    area.value = 'abcdefgh'
    area.setSelectionRange(8, 8)
    area.dispatchEvent(new Event('input', { bubbles: true }))
    expect(value.value).toBe('abcd')
    expect(area.selectionStart).toBe(4)
    wrapper.unmount()
  })

  it('stops enforcing once the field is removed', async () => {
    const value = ref('')
    const wrapper = mount(
      defineComponent({
        directives: { maxChars },
        setup: () => ({ value }),
        template: '<input v-model="value" v-max-chars="2" />',
      }),
    )
    const input = wrapper.find('input').element
    wrapper.unmount()
    input.value = 'abcdef'
    input.dispatchEvent(new Event('input', { bubbles: true }))
    expect(input.value).toBe('abcdef')
  })

  it('leaves a value inside the limit untouched', async () => {
    const { type } = field(5)
    expect(await type('abc')).toBe('abc')
  })
})
