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
    expect(sortButtons().map(b => b.attributes('aria-checked'))).toEqual(['true', 'false'])
    await sortButtons()[1]!.trigger('click')
    expect(sortEmits()).toEqual(['oldest'])
    await wrapper.setProps({ sort: 'oldest' })
    expect(sortButtons().map(b => b.attributes('aria-checked'))).toEqual(['false', 'true'])
  })

  it('shows the header title instead of a count when only criteria are set', () => {
    make({ showV3: true, results: [] })
    expect(wrapper.find('[data-testid="search-results-header"]').exists()).toBe(true)
    wrapper.unmount()
    make({ showV3: true, results: [], hasCriteria: false })
    expect(wrapper.find('[data-testid="search-results-header"]').exists()).toBe(false)
  })

  it('moves the sort with Left and Right and wraps around; Up and Down stay with the results', async () => {
    make({ showV3: true, results: [hit(1)] })
    const group = wrapper.get('[role="radiogroup"]')
    await group.trigger('keydown', { key: 'ArrowRight' })
    await group.trigger('keydown', { key: 'ArrowLeft' })
    await group.trigger('keydown', { key: 'ArrowDown' })
    await group.trigger('keydown', { key: 'ArrowUp' })
    await group.trigger('keydown', { key: 'a' })
    expect(sortEmits()).toEqual(['oldest', 'oldest'])
    await wrapper.setProps({ sort: 'oldest' })
    await group.trigger('keydown', { key: 'ArrowRight' })
    expect(sortEmits()?.at(-1)).toBe('newest')
  })

  it('keeps one tab stop on the sort choice and one on the result list', () => {
    make({ showV3: true, results: [hit(1), hit(2)], sort: 'oldest' })
    expect(sortButtons().map(b => b.attributes('tabindex'))).toEqual(['-1', '0'])
    expect(wrapper.get('#search-results').attributes('tabindex')).toBe('0')
    expect(wrapper.findAll('[role="option"]').every(o => o.attributes('tabindex') === undefined)).toBe(true)
    wrapper.unmount()
    make({ results: [] })
    expect(wrapper.get('#search-results').attributes('tabindex')).toBe('-1')
  })

  it('says how many results there are, with the plural, and that there are more', () => {
    make({ showV3: true, results: [hit(1)] })
    expect(wrapper.get('[data-testid="search-results-header"]').text()).toContain('1 result')
    expect(wrapper.get('[data-testid="search-results-header"]').text()).not.toContain('1 results')
    wrapper.unmount()
    make({ showV3: true, results: [hit(1), hit(2)], hasMore: true })
    expect(wrapper.get('[data-testid="search-results-header"]').text()).toContain('2+ results')
  })

  it('shows a skeleton while the first page loads and dims the old hits while searching again', async () => {
    make({ showV3: true, results: [], searching: true })
    expect(wrapper.find('[data-testid="search-skeleton"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="search-results-header"]').text()).toContain('Searching')
    wrapper.unmount()
    make({ showV3: true, results: [hit(1)], searching: true })
    expect(wrapper.find('[data-testid="search-skeleton"]').exists()).toBe(false)
    expect(wrapper.get('#search-results').classes()).toContain('opacity-60')
    expect(wrapper.get('#search-results').attributes('aria-busy')).toBe('true')
  })

  it('offers a retry on failure', async () => {
    make({ failed: true })
    await wrapper.get('[data-search-retry]').trigger('click')
    expect(wrapper.emitted('retry')).toHaveLength(1)
  })

  it('says what was searched when nothing is found and offers to remove the filters', async () => {
    make({ queryText: 'Stand', filterCount: 1 })
    expect(wrapper.get('[data-testid="search-no-results"]').text()).toContain('No messages found')
    expect(wrapper.get('[data-testid="search-no-results"]').text()).toContain('Nothing found for “Stand”')
    await wrapper.get('[data-search-clear-filters]').trigger('click')
    expect(wrapper.emitted('clear-filters')).toHaveLength(1)
    wrapper.unmount()
    make({ queryText: '', filterCount: 0 })
    expect(wrapper.get('[data-testid="search-no-results"]').text()).toContain('Nothing matches these filters')
    expect(wrapper.find('[data-search-clear-filters]').exists()).toBe(false)
  })

  it('explains filters that cannot be applied instead of listing anything', () => {
    make({
      showV3: true,
      results: [],
      problems: [
        { kind: 'member', filter: 'from', value: 'nobody' },
        { kind: 'channel', filter: 'in', value: 'nowhere' },
        { kind: 'date', filter: 'before', value: 'soon' },
        { kind: 'option', filter: 'has', value: 'foo', options: 'file, image' },
        { kind: 'empty', filter: 'is', value: '' },
      ],
    })
    const text = wrapper.get('[data-testid="search-problems"]').text()
    expect(text).toContain('No member “nobody” found')
    expect(text).toContain('No channel “nowhere” found')
    expect(text).toContain('Can’t read the date “soon” after before:')
    expect(text).toContain('Choose one of: file, image')
    expect(text).toContain('is: needs a value')
    expect(wrapper.find('[data-testid="search-results-header"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="search-no-results"]').exists()).toBe(false)
  })
})
