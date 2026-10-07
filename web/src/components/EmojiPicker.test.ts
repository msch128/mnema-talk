import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { VueWrapper } from '@vue/test-utils'
import { flushPromises, mount } from '@vue/test-utils'
import { setLocale } from '../i18n'
import EmojiPicker from './EmojiPicker.vue'
import EmojiButton from './EmojiButton.vue'
import { requireValue } from '../test-fixtures.fixture'

const state = vi.hoisted(() => ({ fail: false, options: vi.fn() }))
vi.mock('emoji-picker-element/picker.js', () => {
  class FakePicker extends HTMLElement {
    constructor(options: { locale: string; dataSource: string }) {
      super()
      state.options(options)
      if (state.fail) throw new Error('synthetic import failure')
      this.attachShadow({ mode: 'open' }).appendChild(document.createElement('input'))
    }
  }
  customElements.define('synthetic-emoji-picker', FakePicker)
  return { default: FakePicker }
})
let wrapper: VueWrapper
beforeEach(() => { state.fail = false; state.options.mockClear(); setLocale('en') })
afterEach(() => { wrapper?.unmount(); vi.restoreAllMocks(); document.body.innerHTML = ''; setLocale('de') })

describe('EmojiPicker and trigger', () => {
  it.each(['en', 'de'] as const)('loads local %s data, focuses search and emits only Unicode', async lang => {
    setLocale(lang)
    wrapper = mount(EmojiPicker, { attachTo: document.body })
    expect(wrapper.text()).toContain(lang === 'en' ? 'Loading' : 'geladen')
    await vi.waitFor(() => expect(wrapper.element.querySelector('synthetic-emoji-picker')).not.toBeNull())
    const picker = requireValue(document.querySelector<HTMLElement>('synthetic-emoji-picker'))
    expect(state.options).toHaveBeenCalledWith(expect.objectContaining({ locale: lang }))
    expect(picker.classList.contains('dark')).toBe(true)
    picker.dispatchEvent(new CustomEvent('emoji-click', { detail: {} }))
    picker.dispatchEvent(new CustomEvent('emoji-click'))
    expect(wrapper.emitted('pick')).toBeUndefined()
    picker.dispatchEvent(new CustomEvent('emoji-click', { detail: { unicode: '😀' } }))
    expect(wrapper.emitted('pick')).toEqual([['😀']])
    await vi.waitFor(() => expect(picker.shadowRoot?.activeElement?.tagName).toBe('INPUT'))
    picker.dispatchEvent(new Event('pointerdown', { bubbles: true }))
    expect(wrapper.emitted('close')).toBeUndefined()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(wrapper.emitted('close')).toEqual([[]])
    wrapper.unmount()
    expect(picker.isConnected).toBe(false)
  })

  it('reports a load failure by closing the picker', async () => {
    state.fail = true
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    wrapper = mount(EmojiPicker)
    await flushPromises()
    await vi.waitFor(() => expect(warn).toHaveBeenCalledWith('Emoji picker failed to load:', expect.any(Error)))
    expect(wrapper.emitted('close')).toEqual([[]])
  })

  it('does not append a picker after its host was unmounted', async () => {
    wrapper = mount(EmojiPicker)
    wrapper.unmount()
    await flushPromises()
    expect(state.options).not.toHaveBeenCalled()
  })

  it('opens, closes and aligns the trigger and forwards a selected emoji', async () => {
    wrapper = mount(EmojiButton, { props: { align: 'left' }, attachTo: document.body })
    const button = wrapper.get('button')
    expect(button.attributes('aria-expanded')).toBe('false')
    await button.trigger('click')
    await flushPromises()
    expect(button.attributes('aria-expanded')).toBe('true')
    expect(wrapper.getComponent(EmojiPicker).classes()).toContain('left-0')
    wrapper.getComponent(EmojiPicker).vm.$emit('pick', '🎉')
    await flushPromises()
    expect(wrapper.emitted('pick')).toEqual([['🎉']])
    expect(wrapper.findComponent(EmojiPicker).exists()).toBe(false)
    await wrapper.setProps({ align: 'right' })
    await button.trigger('click')
    await flushPromises()
    expect(wrapper.getComponent(EmojiPicker).classes()).toContain('right-0')
    wrapper.getComponent(EmojiPicker).vm.$emit('close')
    await flushPromises()
    expect(button.attributes('aria-expanded')).toBe('false')
    await button.trigger('click')
    await button.trigger('click')
    expect(button.attributes('aria-expanded')).toBe('false')
    await wrapper.setProps({ disabled: true })
    await button.trigger('click')
    expect(button.attributes('aria-expanded')).toBe('false')
  })
})
