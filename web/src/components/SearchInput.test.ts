import { afterEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import SearchInput from './SearchInput.vue'
import { i18nPlugin, setLocale } from '../i18n'
import { tooltip } from '../directives/tooltip'
import { requireValue } from '../test-fixtures.fixture'

const lookup = {
  members: [{ id: 'u1', username: 'ada', display_name: 'Ada Lovelace' }, { id: 'u2', username: 'adam', display_name: '' }],
  channels: [{ id: 'c1', name: 'general' }],
}

let wrapper: VueWrapper
afterEach(() => wrapper?.unmount())

function make(modelValue = '', extra: Record<string, unknown> = {}) {
  setLocale('en')
  wrapper = mount(SearchInput, {
    props: { modelValue, lookup, loading: false, unresolved: [], ...extra },
    global: { plugins: [i18nPlugin], directives: { tooltip } },
    attachTo: document.body,
  })
  return wrapper
}
const field = () => requireValue(wrapper.find<HTMLInputElement>('[data-search-input]').element)
async function type(value: string) {
  field().value = value
  await wrapper.find('[data-search-input]').trigger('input')
}
const last = () => requireValue(wrapper.emitted<[string]>('update:modelValue')?.at(-1))[0]

describe('SearchInput', () => {
  it('turns a finished filter into a chip and keeps the rest in the field', async () => {
    make()
    await type('hello has:image ')
    expect(last()).toBe('has:image hello ')
    await wrapper.setProps({ modelValue: last() })
    expect(wrapper.findAll('[data-search-chip]')).toHaveLength(1)
    expect(field().value).toBe('hello ')
    await wrapper.find('[data-search-chip] button').trigger('click')
    expect(last()).toBe('hello ')
  })

  it('follows a query set from outside and flags unknown names', async () => {
    make('')
    await wrapper.setProps({ modelValue: 'in:general text', unresolved: ['in'] })
    expect(field().value).toBe('text')
    expect(wrapper.find('[data-search-chip]').classes().join(' ')).toContain('danger')
  })

  it('offers suggestions, navigates them with the keyboard and picks one', async () => {
    make()
    await type('from:ad')
    const items = wrapper.findAll('[data-search-suggestions] [role="option"]')
    expect(items).toHaveLength(2)
    expect(field().getAttribute('aria-activedescendant')).toBe('search-suggestion-0')
    await wrapper.find('[data-search-input]').trigger('keydown', { key: 'ArrowDown' })
    expect(field().getAttribute('aria-activedescendant')).toBe('search-suggestion-1')
    await wrapper.find('[data-search-input]').trigger('keydown', { key: 'ArrowDown' })
    expect(field().getAttribute('aria-activedescendant')).toBe('search-suggestion-0')
    await wrapper.find('[data-search-input]').trigger('keydown', { key: 'ArrowUp' })
    expect(field().getAttribute('aria-activedescendant')).toBe('search-suggestion-1')
    await wrapper.find('[data-search-input]').trigger('keydown', { key: 'Enter' })
    expect(last()).toBe('from:adam ')
  })

  it('picks a suggestion with the mouse and ignores other keys while the list is open', async () => {
    make()
    await type('from:ad')
    await wrapper.find('[data-search-input]').trigger('keydown', { key: 'x' })
    await wrapper.find('[data-search-suggestions] [role="option"]').trigger('mousedown')
    expect(last()).toBe('from:ada ')
  })

  it('lets keys through when no suggestion list is open', async () => {
    make()
    await type('plain')
    const ev = new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true, cancelable: true })
    field().dispatchEvent(ev)
    expect(ev.defaultPrevented).toBe(false)
  })

  it('shortcut buttons append a filter prefix and close emits close', async () => {
    make()
    await type('text')
    await wrapper.findAll('[data-search-shortcut]')[2]!.trigger('click')
    expect(last()).toBe('text has:')
    await wrapper.find('[data-dialog-close]').trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)
  })
  it('removes the last chip with Backspace on an empty field', async () => {
    make('has:image from:ada ')
    expect(field().value).toBe('')
    await wrapper.find('[data-search-input]').trigger('keydown', { key: 'Backspace' })
    expect(last()).toBe('from:ada ')
  })

  it('keeps Backspace for the text while the field has some', async () => {
    make('has:image text')
    await wrapper.find('[data-search-input]').trigger('keydown', { key: 'Backspace' })
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('picks a suggestion with Tab and wraps the selection upwards and downwards', async () => {
    make()
    await type('in:g')
    await wrapper.find('[data-search-input]').trigger('keydown', { key: 'ArrowUp' })
    expect(field().getAttribute('aria-activedescendant')).toBe('search-suggestion-0')
    await wrapper.find('[data-search-input]').trigger('keydown', { key: 'ArrowDown' })
    expect(field().getAttribute('aria-activedescendant')).toBe('search-suggestion-0')
    await wrapper.find('[data-search-input]').trigger('keydown', { key: 'Tab' })
    expect(last()).toBe('in:general ')
  })

  it('shows the shortcut row and no filter row without v3', () => {
    make()
    expect(wrapper.findAll('[data-search-shortcut]').length).toBeGreaterThan(0)
    expect(wrapper.find('[data-search-filter-row]').exists()).toBe(false)
  })

  it('separates a shortcut from the text before it only when needed', async () => {
    make()
    await type('text ')
    await wrapper.findAll('[data-search-shortcut]')[0]!.trigger('click')
    expect(last()).toBe('text from:')
  })

  it('shows one filter row with v3 and adds filters from its popovers', async () => {
    make('', { v3Supported: true })
    expect(wrapper.find('[data-search-shortcut]').exists()).toBe(false)
    expect(wrapper.find('[data-search-filter-row]').exists()).toBe(true)

    await wrapper.find('[data-filter-button="has"]').trigger('click')
    await wrapper.find('[data-pop-value="image"]').trigger('click')
    expect(last()).toBe('has:image ')
    await wrapper.setProps({ modelValue: last() })
    expect(wrapper.find('[data-filter-button="has"]').attributes('data-filter-active')).toBe('true')
    // Choosing the active value again removes the filter.
    await wrapper.find('[data-filter-button="has"]').trigger('click')
    await wrapper.find('[data-pop-value="image"]').trigger('click')
    expect(last()).toBe('')
  })

  it('adds a finished token directly after existing text with a space', async () => {
    make('', { v3Supported: true })
    await type('hello')
    await wrapper.find('[data-filter-button="more"]').trigger('click')
    await wrapper.find('[data-more="mentions"]').trigger('click')
    await wrapper.find('[data-popover-input]').setValue('ada')
    await wrapper.find('[data-pop-item]').trigger('click')
    expect(last()).toBe('mentions:ada hello ')
  })

  it('puts away the popover first, then the suggestions, and reports when there was nothing', async () => {
    make('', { v3Supported: true })
    const api = wrapper.vm as unknown as { dismissLayer: () => boolean }
    expect(api.dismissLayer()).toBe(false)
    await wrapper.find('[data-filter-button="in"]').trigger('click')
    expect(wrapper.find('[data-search-popover]').exists()).toBe(true)
    expect(api.dismissLayer()).toBe(true)
    await wrapper.vm.$nextTick()
    expect(wrapper.find('[data-search-popover]').exists()).toBe(false)

    await type('from:a')
    expect(wrapper.find('[data-search-suggestions]').exists()).toBe(true)
    expect(api.dismissLayer()).toBe(true)
    await wrapper.vm.$nextTick()
    expect(wrapper.find('[data-search-suggestions]').exists()).toBe(false)
    expect(api.dismissLayer()).toBe(false)
  })

  it('hides the suggestions while a popover is open', async () => {
    make('', { v3Supported: true })
    await type('from:a')
    expect(wrapper.find('[data-search-suggestions]').exists()).toBe(true)
    await wrapper.find('[data-filter-button="date"]').trigger('click')
    expect(wrapper.find('[data-search-suggestions]').exists()).toBe(false)
  })

  it('shows a progress line instead of a spinner while searching, and the active result id otherwise', () => {
    make('', { activeResultId: 'search-result-3', loading: true })
    expect(field().getAttribute('aria-activedescendant')).toBe('search-result-3')
    expect(field().getAttribute('aria-controls')).toBe('search-results')
    expect(wrapper.find('[data-search-progress]').exists()).toBe(true)
    expect(wrapper.find('.animate-spin').exists()).toBe(false)
  })

  it('leaves the close button out of the tab order and passes the scope choice on', async () => {
    make('', { v3Supported: true, showScope: true, scope: 'channels' })
    expect(wrapper.find('[data-dialog-close]').attributes('tabindex')).toBe('-1')
    await wrapper.find('[data-scope="dm"]').trigger('click')
    expect(wrapper.emitted('update:scope')?.[0]).toEqual(['dm'])
  })

  it('exposes focus to the dialog', () => {
    make()
    ;(wrapper.vm as unknown as { focus: () => void }).focus()
    expect(document.activeElement).toBe(field())
  })
})
