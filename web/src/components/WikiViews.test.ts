import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import WikiDeleted from './WikiDeleted.vue'
import WikiHome from './WikiHome.vue'
import WikiSearch from './WikiSearch.vue'
import { ApiError } from '../lib/api'
import { currentRoute, navigate } from '../lib/router'
import { wikiQuery } from '../lib/wikiNav'
import { useToastStore } from '../stores/toast'
import { useWikiStore } from '../stores/wiki'
import { EDIT_WIKI, grantWiki, MANAGE_WIKI, summary, VIEW_WIKI, wikiPage } from '../wiki.fixture'

const api = vi.hoisted(() => ({ searchWiki: vi.fn(), undeleteWikiPage: vi.fn(), fetchWikiTree: vi.fn() }))
vi.mock('../lib/wikiApi', () => api)

enableAutoUnmount(afterEach)

beforeEach(() => {
  setActivePinia(createPinia())
  Object.values(api).forEach(fn => fn.mockReset())
  api.fetchWikiTree.mockResolvedValue([])
  grantWiki(VIEW_WIKI)
  wikiQuery.value = ''
  navigate('/wiki', { replace: true })
})
afterEach(() => { vi.useRealTimers() })

describe('WikiHome', () => {
  it('points an editor to the first page of an empty wiki, and a reader to wait', async () => {
    const wiki = useWikiStore()
    wiki.treeLoaded = true
    let w = mount(WikiHome)
    expect(w.find('[data-testid="wiki-first-page"]').exists()).toBe(false)
    expect(w.find('[data-testid="wiki-start-empty"]').exists()).toBe(true)
    grantWiki(VIEW_WIKI | EDIT_WIKI)
    w = mount(WikiHome)
    await w.get('[data-testid="wiki-first-page"]').trigger('click')
    expect(currentRoute.value).toMatchObject({ wiki: { mode: 'new' } })
  })

  it('lists the top pages with their subpage count and the recent changes', async () => {
    const wiki = useWikiStore()
    wiki.treeLoaded = true
    wiki.pages = [summary('1', 'Guides'), summary('2', 'Raid', { parent_id: '1', updated_at: '2026-03-05T00:00:00Z' })]
    const w = mount(WikiHome)
    const tops = w.get('section').findAll('li')
    expect(tops).toHaveLength(1)
    expect(tops[0]?.text()).toContain('Guides')
    expect(w.findAll('time').map(t => t.attributes('datetime'))).toContain('2026-03-05T00:00:00Z')
    await w.findAll('section')[1]!.get('button').trigger('click')
    expect(currentRoute.value).toMatchObject({ wiki: { mode: 'page', slug: 'raid' } })
    await tops[0]!.get('button').trigger('click')
    expect(currentRoute.value).toMatchObject({ wiki: { slug: 'guides' } })
  })

  it('shows nothing of the pages before the tree is read', () => {
    const w = mount(WikiHome)
    expect(w.find('[data-testid="wiki-start-empty"]').exists()).toBe(false)
  })
})

describe('WikiSearch', () => {
  beforeEach(() => vi.useFakeTimers())

  it('waits for three characters, then searches after a short pause', async () => {
    api.searchWiki.mockResolvedValue([{ slug: 'loot', title: 'Loot', snippet: 'need before greed' }])
    wikiQuery.value = 'lo'
    const w = mount(WikiSearch)
    await vi.advanceTimersByTimeAsync(1000)
    expect(api.searchWiki).not.toHaveBeenCalled()
    wikiQuery.value = 'loot'
    await vi.advanceTimersByTimeAsync(100)
    expect(api.searchWiki).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(200)
    await flushPromises()
    expect(api.searchWiki).toHaveBeenCalledWith('loot')
    expect(w.get('[data-testid="wiki-hit"]').text()).toContain('need before greed')
    await w.get('[data-testid="wiki-hit"]').trigger('click')
    expect(currentRoute.value).toMatchObject({ wiki: { mode: 'page', slug: 'loot' } })
  })

  it('searches once for a burst of typing and keeps the latest answer', async () => {
    api.searchWiki.mockResolvedValue([])
    const w = mount(WikiSearch)
    for (const q of ['loo', 'loot', 'loot r']) {
      wikiQuery.value = q
      await vi.advanceTimersByTimeAsync(50)
    }
    await vi.advanceTimersByTimeAsync(400)
    await flushPromises()
    expect(api.searchWiki).toHaveBeenCalledTimes(1)
    expect(api.searchWiki).toHaveBeenCalledWith('loot r')
    expect(w.find('[data-testid="wiki-no-hits"]').exists()).toBe(true)
  })

  it('drops a slow answer to an older query', async () => {
    let slow!: (v: unknown) => void
    api.searchWiki.mockReturnValueOnce(new Promise(r => { slow = r }))
    api.searchWiki.mockResolvedValueOnce([{ slug: 'b', title: 'Second', snippet: '' }])
    const w = mount(WikiSearch)
    wikiQuery.value = 'first'
    await vi.advanceTimersByTimeAsync(300)
    wikiQuery.value = 'second'
    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()
    slow([{ slug: 'a', title: 'First', snippet: '' }])
    await flushPromises()
    expect(w.findAll('[data-testid="wiki-hit"]').map(h => h.text())).toEqual(['Second'])
  })

  it('shows a failed search, and clearing the box starts over', async () => {
    api.searchWiki.mockRejectedValue(new Error('offline'))
    const w = mount(WikiSearch)
    wikiQuery.value = 'loot'
    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()
    expect(w.find('[role="alert"]').exists()).toBe(true)
    wikiQuery.value = ''
    await flushPromises()
    expect(w.find('[role="alert"]').exists()).toBe(false)
  })

  it('does not search after it went away', async () => {
    const w = mount(WikiSearch)
    wikiQuery.value = 'loot'
    w.unmount()
    await vi.advanceTimersByTimeAsync(1000)
    expect(api.searchWiki).not.toHaveBeenCalled()
  })
})

describe('WikiDeleted', () => {
  const gone = summary('9', 'Old', { deleted_at: '2026-02-01T00:00:00Z' })

  it('is closed to those who do not manage the wiki', () => {
    useWikiStore().pages = [gone]
    const w = mount(WikiDeleted)
    expect(w.find('[role="alert"]').exists()).toBe(true)
    expect(w.find('[data-restore]').exists()).toBe(false)
  })

  it('says when nothing is deleted', () => {
    grantWiki(VIEW_WIKI | MANAGE_WIKI)
    expect(mount(WikiDeleted).find('[data-testid="wiki-no-deleted"]').exists()).toBe(true)
  })

  it('restores a page, and reads the tree again', async () => {
    grantWiki(VIEW_WIKI | MANAGE_WIKI)
    useWikiStore().pages = [gone]
    api.undeleteWikiPage.mockResolvedValue(wikiPage({ id: '9', slug: 'old' }))
    const w = mount(WikiDeleted)
    await w.get('[data-restore="old"]').trigger('click')
    await flushPromises()
    expect(api.undeleteWikiPage).toHaveBeenCalledWith('old')
    expect(api.fetchWikiTree).toHaveBeenCalled()
    expect(useToastStore().toasts.at(-1)?.type).toBe('success')
  })

  it('opens a deleted page to read it, and reports a failed restore', async () => {
    grantWiki(VIEW_WIKI | MANAGE_WIKI)
    useWikiStore().pages = [gone]
    api.undeleteWikiPage.mockRejectedValue(new ApiError(409, 'CONFLICT', 'the wiki holds at most 500 pages'))
    const w = mount(WikiDeleted)
    await w.get('li > button').trigger('click')
    expect(currentRoute.value).toMatchObject({ wiki: { slug: 'old' } })
    await w.get('[data-restore="old"]').trigger('click')
    await flushPromises()
    expect(useToastStore().toasts.at(-1)?.type).toBe('error')
  })
})
