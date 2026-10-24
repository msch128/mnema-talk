import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import SearchResults from './SearchResults.vue'
import { i18nPlugin, setLocale } from '../i18n'
import { tooltip } from '../directives/tooltip'
import { fixtureId, messageFixture } from '../test-fixtures.fixture'

let wrapper: VueWrapper
afterEach(() => wrapper?.unmount())

const channelId = fixtureId(7)

function make(extra: Record<string, unknown> = {}) {
  setLocale('en')
  wrapper = mount(SearchResults, {
    props: {
      results: [], roots: {}, hasMore: false, loadingMore: false, searching: false, failed: false,
      hasCriteria: true, selectedIndex: 0, terms: [], channelNames: new Map([[channelId, 'general']]),
      voiceChannels: new Set<string>(), ...extra,
    },
    global: { plugins: [i18nPlugin], directives: { tooltip } },
    attachTo: document.body,
  })
  return wrapper
}
const hit = (n: number, extra: Record<string, unknown> = {}) =>
  messageFixture({ id: fixtureId(100 + n), channel_id: channelId, content: `hello ${n}`, ...extra })
const sortButtons = () => wrapper.findAll('[data-sort]')
const sortEmits = () => wrapper.emitted<[string]>('update:sort')?.map(e => e[0])

describe('SearchResults', () => {
  it('shows a hint until there is something to search for', () => {
    make({ hasCriteria: false })
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.text()).not.toBe('')
    expect(wrapper.findAll('[role="option"]')).toHaveLength(0)
  })

  it('says the search failed, or that the filters are not supported', () => {
    make({ failed: true })
    expect(wrapper.find('[role="alert"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="search-filter-unsupported"]').exists()).toBe(false)
    wrapper.unmount()
    make({ failed: true, filterUnsupported: true })
    expect(wrapper.find('[data-testid="search-filter-unsupported"]').exists()).toBe(true)
  })

  it('says nothing was found, but not while a search is running', () => {
    make()
    expect(wrapper.text()).toContain('No messages found')
    wrapper.unmount()
    make({ searching: true })
    expect(wrapper.text()).not.toContain('No messages found')
  })

  it('lists the hits and reports the picked one', async () => {
    const results = [hit(1), hit(2)]
    make({ results, selectedIndex: 1, terms: ['hello'] })
    const items = wrapper.findAll('[role="option"]')
    expect(items).toHaveLength(2)
    expect(items[1]!.attributes('aria-selected')).toBe('true')
    expect(items[0]!.attributes('aria-selected')).toBe('false')
    await items[0]!.trigger('click')
    expect(wrapper.emitted<[unknown]>('select')?.[0]?.[0]).toMatchObject({ id: results[0]!.id })
  })

  it('shows a thread root for replies', () => {
    const reply = hit(1, { parent_id: fixtureId(200) })
    make({ results: [reply], roots: { [fixtureId(200)]: { id: fixtureId(200), channel_id: channelId, snippet: 'the original post', display_name: 'Ada', username: 'ada' } } })
    expect(wrapper.text()).toContain('the original post')
  })

  it('scrolls the selected hit into view when the selection moves', async () => {
    make({ results: [hit(1), hit(2)], selectedIndex: 0 })
    const scroll = vi.fn()
    const el = wrapper.findAll('[role="option"]')[1]!.element as HTMLElement
    el.scrollIntoView = scroll
    await wrapper.setProps({ selectedIndex: 1 })
    await wrapper.vm.$nextTick()
    expect(scroll).toHaveBeenCalledWith({ block: 'nearest' })
  })

  it('offers more results and disables the button while loading', async () => {
    make({ results: [hit(1)], hasMore: true })
    const more = wrapper.findAll('button').find(b => b.text().includes('more'))!
    await more.trigger('click')
    expect(wrapper.emitted('more')).toHaveLength(1)
    await wrapper.setProps({ loadingMore: true })
    expect(more.attributes('disabled')).toBeDefined()
  })

  it('hides the header without the v3 search', () => {
    make({ results: [hit(1)] })
    expect(wrapper.find('[data-testid="search-results-header"]').exists()).toBe(false)
  })

  it('shows the count and the sort choice with v3, newest by default', async () => {
    make({ showV3: true, results: [hit(1), hit(2)] })
    expect(wrapper.get('[data-testid="search-results-header"]').text()).toContain('2')
    expect(sortButtons().map(b => b.attributes('aria-checked'))).toEqual(['true', 'false', 'false'])
    await sortButtons()[2]!.trigger('click')
    expect(sortEmits()).toEqual(['relevance'])
    await wrapper.setProps({ sort: 'oldest' })
    expect(sortButtons().map(b => b.attributes('aria-checked'))).toEqual(['false', 'true', 'false'])
  })

  it('shows the header title instead of a count when only criteria are set', () => {
    make({ showV3: true, results: [] })
    expect(wrapper.find('[data-testid="search-results-header"]').exists()).toBe(true)
    wrapper.unmount()
    make({ showV3: true, results: [], hasCriteria: false })
    expect(wrapper.find('[data-testid="search-results-header"]').exists()).toBe(false)
  })

  it('moves the sort with the arrow keys and wraps around', async () => {
    make({ showV3: true, results: [hit(1)] })
    const group = wrapper.get('[role="radiogroup"]')
    await group.trigger('keydown', { key: 'ArrowRight' })
    await group.trigger('keydown', { key: 'ArrowDown' })
    await group.trigger('keydown', { key: 'ArrowLeft' })
    await group.trigger('keydown', { key: 'ArrowUp' })
    await group.trigger('keydown', { key: 'a' })
    expect(sortEmits()).toEqual(['oldest', 'oldest', 'relevance', 'relevance'])
    await wrapper.setProps({ sort: 'relevance' })
    await group.trigger('keydown', { key: 'ArrowRight' })
    expect(sortEmits()?.at(-1)).toBe('newest')
  })
})
