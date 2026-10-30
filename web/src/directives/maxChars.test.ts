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
})
