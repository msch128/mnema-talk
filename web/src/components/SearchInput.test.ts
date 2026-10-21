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
})
