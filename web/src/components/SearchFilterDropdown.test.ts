import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import SearchFilterDropdown from './SearchFilterDropdown.vue'
import { i18nPlugin } from '../i18n'

const lookup = {
  members: [
    { id: 'u1', username: 'ada', display_name: 'Ada Lovelace' },
    { id: 'u2', username: 'bob', display_name: '' },
  ],
  channels: [
    { id: 'c1', name: 'general' },
    { id: 'c2', name: 'random' },
  ],
}

const mountOpts = {
  global: { plugins: [i18nPlugin] },
}

describe('SearchFilterDropdown', () => {
  it('does not render when open is false', () => {
    const w = mount(SearchFilterDropdown, {
      ...mountOpts,
      props: { open: false, lookup },
    })
    expect(w.find('[data-search-filter-dropdown]').exists()).toBe(false)
  })

  it('renders all filter suggestion sections and chips when open', () => {
    const w = mount(SearchFilterDropdown, {
      ...mountOpts,
      props: { open: true, lookup },
    })
    expect(w.find('[data-search-filter-dropdown]').exists()).toBe(true)
    expect(w.find('[data-filter-chip="from"]').exists()).toBe(true)
    expect(w.find('[data-filter-chip="in"]').exists()).toBe(true)
    expect(w.find('[data-filter-chip="mentions"]').exists()).toBe(true)
    expect(w.find('[data-filter-chip="filename"]').exists()).toBe(true)
    expect(w.find('[data-filter-has="file"]').exists()).toBe(true)
    expect(w.find('[data-filter-author="bot"]').exists()).toBe(true)
    expect(w.find('[data-date-input]').exists()).toBe(true)
    expect(w.find('[data-date-today]').exists()).toBe(true)
  })

  it('emits select with token when basic chips are clicked', async () => {
    const w = mount(SearchFilterDropdown, {
      ...mountOpts,
      props: { open: true, lookup },
    })

    await w.find('[data-filter-chip="from"]').trigger('click')
    expect(w.emitted('select')?.[0]).toEqual(['from:'])

    await w.find('[data-filter-chip="in"]').trigger('click')
    expect(w.emitted('select')?.[1]).toEqual(['in:'])

    await w.find('[data-filter-chip="mentions"]').trigger('click')
    expect(w.emitted('select')?.[2]).toEqual(['mentions:'])

    await w.find('[data-filter-chip="filename"]').trigger('click')
    expect(w.emitted('select')?.[3]).toEqual(['filename:'])
  })

  it('emits select when has: and author: options are clicked', async () => {
    const w = mount(SearchFilterDropdown, {
      ...mountOpts,
      props: { open: true, lookup },
    })

    await w.find('[data-filter-has="file"]').trigger('click')
    expect(w.emitted('select')?.[0]).toEqual(['has:file'])

    await w.find('[data-filter-has="embed"]').trigger('click')
    expect(w.emitted('select')?.[1]).toEqual(['has:embed'])

    await w.find('[data-filter-author="bot"]').trigger('click')
    expect(w.emitted('select')?.[2]).toEqual(['author:bot'])

    await w.find('[data-filter-author="webhook"]').trigger('click')
    expect(w.emitted('select')?.[3]).toEqual(['author:webhook'])
  })

  it('emits select with date on today and custom date pick', async () => {
    const w = mount(SearchFilterDropdown, {
      ...mountOpts,
      props: { open: true, lookup },
    })

    await w.find('[data-date-today]').trigger('click')
    const todayCall = w.emitted('select')?.[0]?.[0] as string
    expect(todayCall).toMatch(/^during:\d{4}-\d{2}-\d{2}$/)

    // Switch date qualifier to before
    await w.find('[data-date-kind="before"]').trigger('click')
    const input = w.find<HTMLInputElement>('[data-date-input]')
    input.element.value = '2026-10-24'
    await input.trigger('input')
    await input.trigger('change')
    expect(w.emitted('select')?.[1]).toEqual(['before:2026-10-24'])
  })

  it('handles escape and keyboard navigation', async () => {
    const w = mount(SearchFilterDropdown, {
      ...mountOpts,
      props: { open: true, lookup },
    })

    await w.find('[data-search-filter-dropdown]').trigger('keydown', { key: 'Escape' })
    expect(w.emitted('close')).toBeTruthy()

    await w.find('[data-search-filter-dropdown]').trigger('keydown', { key: 'ArrowDown' })
    expect(w.find('[data-search-filter-dropdown]').exists()).toBe(true)
  })
  it('applies a picked date with the apply button and clears the field', async () => {
    const w = mount(SearchFilterDropdown, { ...mountOpts, props: { open: true, lookup } })
    expect(w.find('[data-date-apply]').exists()).toBe(false)
    await w.find('[data-date-kind="after"]').trigger('click')
    const input = w.find<HTMLInputElement>('[data-date-input]')
    input.element.value = '2026-02-03'
    await input.trigger('input')
    await w.find('[data-date-apply]').trigger('click')
    expect(w.emitted('select')?.[0]).toEqual(['after:2026-02-03'])
    expect(w.find('[data-date-apply]').exists()).toBe(false)
  })

  it('does not emit a date when none is chosen', async () => {
    const w = mount(SearchFilterDropdown, { ...mountOpts, props: { open: true, lookup } })
    await w.find<HTMLInputElement>('[data-date-input]').trigger('change')
    expect(w.emitted('select')).toBeUndefined()
  })

  it('moves focus through the controls with the arrow keys and wraps around', async () => {
    const w = mount(SearchFilterDropdown, { ...mountOpts, props: { open: true, lookup }, attachTo: document.body })
    const root = w.find('[data-search-filter-dropdown]')
    const controls = Array.from(root.element.querySelectorAll<HTMLElement>('button, input'))
    // Nothing focused yet: the first control gets focus.
    await root.trigger('keydown', { key: 'ArrowDown' })
    expect(document.activeElement).toBe(controls[0])
    await root.trigger('keydown', { key: 'ArrowRight' })
    expect(document.activeElement).toBe(controls[1])
    await root.trigger('keydown', { key: 'ArrowUp' })
    expect(document.activeElement).toBe(controls[0])
    await root.trigger('keydown', { key: 'ArrowLeft' })
    expect(document.activeElement).toBe(controls.at(-1))
    await root.trigger('keydown', { key: 'ArrowDown' })
    expect(document.activeElement).toBe(controls[0])
    w.unmount()
  })

  it('ignores other keys', async () => {
    const w = mount(SearchFilterDropdown, { ...mountOpts, props: { open: true, lookup } })
    await w.find('[data-search-filter-dropdown]').trigger('keydown', { key: 'a' })
    expect(w.emitted('close')).toBeUndefined()
  })
})
