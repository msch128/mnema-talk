import { afterEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import SearchFilterRow from './SearchFilterRow.vue'
import { isoDay } from '../lib/searchQuery'
import { i18nPlugin, setLocale } from '../i18n'

const lookup = {
  members: [
    { id: 'u1', username: 'ada', display_name: 'Ada Lovelace' },
    { id: 'u2', username: 'bob', display_name: '' },
  ],
  channels: [{ id: 'c1', name: 'general' }, { id: 'c2', name: 'lounge' }],
}

let wrapper: VueWrapper
afterEach(() => wrapper?.unmount())

function make(extra: Record<string, unknown> = {}) {
  setLocale('en')
  wrapper = mount(SearchFilterRow, {
    props: { lookup, active: {}, ...extra },
    global: { plugins: [i18nPlugin] },
    attachTo: document.body,
  })
  return wrapper
}
const button = (kind: string) => wrapper.get(`[data-filter-button="${kind}"]`)
const picks = () => wrapper.emitted<[string]>('pick')?.map(e => e[0])
const popover = () => wrapper.find('[data-search-popover]')

describe('SearchFilterRow', () => {
  it('is one line of buttons that open a popover each, one at a time', async () => {
    make()
    expect(wrapper.findAll('[data-filter-button]').map(b => b.attributes('data-filter-button'))).toEqual(['from', 'in', 'has', 'date', 'more'])
    expect(popover().exists()).toBe(false)
    await button('from').trigger('click')
    expect(popover().attributes('data-popover-kind')).toBe('from')
    expect(button('from').attributes('aria-expanded')).toBe('true')
    await button('in').trigger('click')
    expect(popover().attributes('data-popover-kind')).toBe('in')
    await button('in').trigger('click')
    expect(popover().exists()).toBe(false)
  })

  it('does not take the focus away from the field when a button is pressed', () => {
    make()
    const ev = new MouseEvent('mousedown', { bubbles: true, cancelable: true })
    button('has').element.dispatchEvent(ev)
    expect(ev.defaultPrevented).toBe(true)
  })

  it('picks a member by typing and Enter, and a channel by click', async () => {
    make()
    await button('from').trigger('click')
    await wrapper.get('[data-popover-input]').setValue('love')
    expect(wrapper.findAll('[data-pop-item]').map(i => i.attributes('data-pop-value'))).toEqual(['ada'])
    await wrapper.get('[data-popover-input]').trigger('keydown', { key: 'Enter' })
    expect(picks()).toEqual(['from:ada'])
    expect(popover().exists()).toBe(false)

    await button('in').trigger('click')
    await wrapper.get('[data-pop-value="lounge"]').trigger('click')
    expect(picks()).toEqual(['from:ada', 'in:lounge'])
  })

  it('says when nothing matches', async () => {
    make()
    await button('in').trigger('click')
    await wrapper.get('[data-popover-input]').setValue('zzz')
    expect(popover().text()).toContain('No matches')
  })

  it('offers the kinds of has: and author:, and clears a chosen one when it is chosen again', async () => {
    make({ active: { has: 'image' } })
    expect(button('has').attributes('data-filter-active')).toBe('true')
    await button('has').trigger('click')
    expect(wrapper.findAll('[data-pop-item]')).toHaveLength(7)
    await wrapper.get('[data-pop-value="file"]').trigger('click')
    expect(picks()).toEqual(['has:file'])
    await button('has').trigger('click')
    await wrapper.get('[data-pop-value="image"]').trigger('click')
    expect(wrapper.emitted<[string]>('clear')?.[0]).toEqual(['has'])
  })

  it('picks a day: today, yesterday or a date, for during, before or after', async () => {
    make()
    await button('date').trigger('click')
    await wrapper.get('[data-date-today]').trigger('click')
    await button('date').trigger('click')
    await wrapper.get('[data-date-kind="before"]').trigger('click')
    expect(wrapper.get('[data-date-kind="before"]').attributes('aria-checked')).toBe('true')
    await wrapper.get('[data-date-yesterday]').trigger('click')
    await button('date').trigger('click')
    await wrapper.get('[data-date-kind="after"]').trigger('click')
    await wrapper.get('[data-date-input]').setValue('2026-01-02')
    expect(picks()).toEqual([`during:${isoDay(0)}`, `before:${isoDay(-1)}`, 'after:2026-01-02'])
  })

  it('leads to the rarer filters through "more"', async () => {
    make({ active: { is: 'pinned' } })
    expect(button('more').attributes('data-filter-active')).toBe('true')
    await button('more').trigger('click')
    await wrapper.get('[data-more="filename"]').trigger('click')
    expect(popover().attributes('data-popover-kind')).toBe('filename')
    expect(button('more').attributes('aria-expanded')).toBe('true')
    await wrapper.get('[data-popover-input]').setValue('plan v2.pdf')
    await wrapper.get('button.btn-primary').trigger('click')
    expect(picks()).toEqual(['filename:"plan v2.pdf"'])

    await button('more').trigger('click')
    await wrapper.get('[data-more="author"]').trigger('click')
    await wrapper.get('[data-pop-value="bot"]').trigger('click')
    await button('more').trigger('click')
    await wrapper.get('[data-more="pinned"]').trigger('click')
    expect(wrapper.emitted<[string]>('clear')?.[0]).toEqual(['is'])
    expect(picks()).toEqual(['filename:"plan v2.pdf"', 'author:bot'])
  })

  it('moves between its buttons with the arrow keys and keeps one tab stop', async () => {
    make()
    expect(wrapper.findAll('[data-filter-button]').map(b => b.attributes('tabindex'))).toEqual(['0', '-1', '-1', '-1', '-1'])
    ;(button('from').element as HTMLElement).focus()
    await button('from').trigger('keydown', { key: 'ArrowRight' })
    expect(document.activeElement).toBe(button('in').element)
    await wrapper.get('[role="toolbar"]').trigger('keydown', { key: 'ArrowLeft' })
    expect(document.activeElement).toBe(button('from').element)
    await wrapper.get('[role="toolbar"]').trigger('keydown', { key: 'ArrowLeft' })
    expect(document.activeElement).toBe(button('more').element)
    expect(button('more').attributes('tabindex')).toBe('0')
  })

  it('walks through the field and the choices of a popover with Up and Down', async () => {
    make()
    await button('in').trigger('click')
    await wrapper.vm.$nextTick(); await wrapper.vm.$nextTick()
    const input = wrapper.get('[data-popover-input]').element
    expect(document.activeElement).toBe(input)
    await wrapper.get('[data-search-popover]').trigger('keydown', { key: 'ArrowDown' })
    expect(document.activeElement).toBe(wrapper.get('[data-pop-value="general"]').element)
    await wrapper.get('[data-search-popover]').trigger('keydown', { key: 'ArrowUp' })
    expect(document.activeElement).toBe(input)
  })

  it('closes a popover on a press elsewhere, and reports it for Escape', async () => {
    make()
    const api = wrapper.vm as unknown as { close: (refocus?: boolean) => boolean; isOpen: () => boolean }
    expect(api.close()).toBe(false)
    await button('from').trigger('click')
    expect(api.isOpen()).toBe(true)
    document.body.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true }))
    await wrapper.vm.$nextTick()
    expect(popover().exists()).toBe(false)
    expect(wrapper.emitted('open')?.map(e => e[0])).toEqual([true, false])

    await button('from').trigger('click')
    expect(api.close(true)).toBe(true)
    await wrapper.vm.$nextTick(); await wrapper.vm.$nextTick()
    expect(document.activeElement).toBe(button('from').element)
  })

  it('offers the choice between channels and direct messages when asked', async () => {
    make({ showScope: true, scope: 'channels' })
    expect(wrapper.get('[data-scope="channels"]').attributes('aria-pressed')).toBe('true')
    await wrapper.get('[data-scope="dm"]').trigger('click')
    expect(wrapper.emitted('update:scope')?.[0]).toEqual(['dm'])
    wrapper.unmount()
    make()
    expect(wrapper.find('[data-testid="search-scope"]').exists()).toBe(false)
  })
})

describe('SearchFilterRow date popover', () => {
  const dateInput = () => wrapper.get<HTMLInputElement>('[data-date-input]')

  it('waits for a real year before picking a typed day', async () => {
    make()
    await button('date').trigger('click')
    // A date field reports 0002-10-28 after the first year digit.
    await dateInput().setValue('0002-10-28')
    expect(picks()).toBeUndefined()
    await dateInput().setValue('2026-10-28')
    expect(picks()).toEqual(['during:2026-10-28'])
  })

  it('walks the list from the other controls: Up enters at the bottom, Down at the top', async () => {
    make()
    await button('date').trigger('click')
    const items = wrapper.findAll<HTMLElement>('[data-pop-item]')
    const kind = wrapper.get<HTMLElement>('[data-date-kind]')
    kind.element.focus()
    await kind.trigger('keydown', { key: 'ArrowUp' })
    expect(document.activeElement).toBe(items.at(-1)?.element)
    kind.element.focus()
    await kind.trigger('keydown', { key: 'ArrowDown' })
    expect(document.activeElement).toBe(items[0]?.element)
  })
})
