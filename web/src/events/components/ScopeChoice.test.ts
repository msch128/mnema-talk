import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import ScopeChoice from './ScopeChoice.vue'
import { setLocale } from '../../i18n'

let wrapper: VueWrapper
beforeEach(() => setLocale('en'))
afterEach(() => wrapper?.unmount())

describe('ScopeChoice', () => {
  it('offers this event or this and the following ones and reports the choice', async () => {
    wrapper = mount(ScopeChoice, { props: { modelValue: 'this', hint: 'Earlier events stay.' } })
    expect(wrapper.text()).toContain('Only this event')
    expect(wrapper.text()).toContain('This and all following events')
    expect(wrapper.text()).toContain('Earlier events stay.')
    expect((wrapper.get('[data-testid="scope-this"]').element as HTMLInputElement).checked).toBe(true)
    await wrapper.get('[data-testid="scope-following"]').setValue(true)
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual(['following'])
    await wrapper.get('[data-testid="scope-this"]').setValue(true)
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual(['this'])
  })

  it('shows no hint line without one', () => {
    wrapper = mount(ScopeChoice, { props: { modelValue: 'following' } })
    expect(wrapper.find('p').exists()).toBe(false)
  })
})
