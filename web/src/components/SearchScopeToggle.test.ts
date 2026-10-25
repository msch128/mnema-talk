import { afterEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import SearchScopeToggle from './SearchScopeToggle.vue'
import { i18nPlugin, setLocale } from '../i18n'

let wrapper: VueWrapper
afterEach(() => wrapper?.unmount())

function make(scope: 'channels' | 'dm' | undefined) {
  setLocale('en')
  wrapper = mount(SearchScopeToggle, { props: { scope }, global: { plugins: [i18nPlugin] } })
  return wrapper
}

describe('SearchScopeToggle', () => {
  it('marks the chosen scope as pressed', () => {
    make('dm')
    expect(wrapper.get('[data-scope="dm"]').attributes('aria-pressed')).toBe('true')
    expect(wrapper.get('[data-scope="channels"]').attributes('aria-pressed')).toBe('false')
  })

  it('reports the other scope when it is pressed, without taking focus', async () => {
    make('channels')
    const ev = new MouseEvent('mousedown', { bubbles: true, cancelable: true })
    wrapper.get('[data-scope="dm"]').element.dispatchEvent(ev)
    expect(ev.defaultPrevented).toBe(true)
    await wrapper.get('[data-scope="dm"]').trigger('click')
    await wrapper.get('[data-scope="channels"]').trigger('click')
    expect(wrapper.emitted('update:scope')).toEqual([['dm'], ['channels']])
  })

  it('shows neither as pressed without a scope', () => {
    make(undefined)
    expect(wrapper.findAll('[aria-pressed="true"]')).toHaveLength(0)
  })
})
