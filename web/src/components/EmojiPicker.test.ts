import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { VueWrapper } from '@vue/test-utils'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { useEmojiStore } from '../stores/emoji'
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
beforeEach(() => { setActivePinia(createPinia()); state.fail = false; state.options.mockClear(); setLocale('en') })
afterEach(() => { wrapper?.unmount(); vi.restoreAllMocks(); document.body.innerHTML = ''; setLocale('de') })

describe('EmojiPicker and trigger', () => {
  it.each(['en', 'de'] as const)('loads local %s data, focuses search and emits only Unicode', async lang => {
    setLocale(lang)
    wrapper = mount(EmojiPicker, { attachTo: document.body })
    expect(wrapper.attributes('role')).toBe('dialog')
    expect(wrapper.attributes('aria-label')).toBeTruthy()
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
    expect(wrapper.emitted('close')).toEqual([['escape']])
    wrapper.unmount()
    expect(picker.isConnected).toBe(false)
  })

  it.each([[false, '<:blob:00000000-0000-4000-8000-000000000501>'], [true, ':blob:']])('lists custom emoji and picks them (shortcode %s)', async (shortcode, expected) => {
    useEmojiStore().items = [{ id: '00000000-0000-4000-8000-000000000501', name: 'blob', animated: false, url: '/api/emoji/x/image?v=1' }]
    wrapper = mount(EmojiPicker, { attachTo: document.body, props: { shortcode } })
    await vi.waitFor(() => expect(wrapper.element.querySelector('synthetic-emoji-picker')).not.toBeNull())
    const picker = requireValue(document.querySelector<HTMLElement>('synthetic-emoji-picker'))
    expect((picker as unknown as { customEmoji: unknown }).customEmoji).toEqual([{ name: 'blob', shortcodes: ['blob'], url: '/api/emoji/x/image?v=1' }])
    picker.dispatchEvent(new CustomEvent('emoji-click', { detail: { name: 'blob' } }))
    picker.dispatchEvent(new CustomEvent('emoji-click', { detail: { name: 'unknown' } }))
    expect(wrapper.emitted('pick')).toEqual([[expected]])
  })

  it.each([
    ['sticks out on the left', { left: -20, right: 320 }, '28px 0'],
    ['sticks out on the right', { left: 800, right: 1140 }, '-124px 0'],
    ['fits', { left: 100, right: 440 }, '']
  ])('slides back into the viewport when it %s', async (_name, rect, translate) => {
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
      return this.dataset['testid'] === 'emoji-picker' ? { ...rect, top: 0, bottom: 360, width: 340, height: 360, x: rect.left, y: 0, toJSON: () => ({}) } : new DOMRect()
    })
    vi.spyOn(HTMLElement.prototype, 'offsetWidth', 'get').mockReturnValue(340)
    wrapper = mount(EmojiPicker, { attachTo: document.body })
    // Placed at once, while the loading box is all there is on screen.
    expect((wrapper.element as HTMLElement).style.translate).toBe(translate)
    await vi.waitFor(() => expect(wrapper.element.querySelector('synthetic-emoji-picker')).not.toBeNull())
    expect((wrapper.element as HTMLElement).style.translate).toBe(translate)
  })

  it('is placed again when the window changes size', async () => {
    let left = 100
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
      return this.dataset['testid'] === 'emoji-picker' ? { left, right: left + 340, top: 0, bottom: 360, width: 340, height: 360, x: left, y: 0, toJSON: () => ({}) } : new DOMRect()
    })
    vi.spyOn(HTMLElement.prototype, 'offsetWidth', 'get').mockReturnValue(340)
    wrapper = mount(EmojiPicker, { attachTo: document.body })
    expect((wrapper.element as HTMLElement).style.translate).toBe('')
    left = -30
    window.dispatchEvent(new Event('resize'))
    expect((wrapper.element as HTMLElement).style.translate).toBe('38px 0')
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
    expect(document.activeElement).toBe(button.element)
    await button.trigger('click')
    ;(document.activeElement as HTMLElement | null)?.blur()
    wrapper.getComponent(EmojiPicker).vm.$emit('close', 'outside')
    await flushPromises()
    expect(button.attributes('aria-expanded')).toBe('false')
    expect(document.activeElement).not.toBe(button.element)
    await button.trigger('click')
    await button.trigger('click')
    expect(button.attributes('aria-expanded')).toBe('false')
    await wrapper.setProps({ disabled: true })
    await button.trigger('click')
    expect(button.attributes('aria-expanded')).toBe('false')
  })
})
