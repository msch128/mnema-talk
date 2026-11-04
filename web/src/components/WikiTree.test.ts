import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import WikiTree from './WikiTree.vue'
import { currentRoute, navigate } from '../lib/router'
import { useWikiStore } from '../stores/wiki'
import { EDIT_WIKI, grantWiki, MANAGE_WIKI, summary, VIEW_WIKI } from '../wiki.fixture'

enableAutoUnmount(afterEach)

beforeEach(() => {
  setActivePinia(createPinia())
  grantWiki(VIEW_WIKI)
  navigate('/wiki', { replace: true })
  const wiki = useWikiStore()
  wiki.treeLoaded = true
  wiki.pages = [summary('1', 'Guides'), summary('2', 'Raid', { parent_id: '1' }), summary('3', 'Loot', { parent_id: '2' }), summary('4', 'Rules', { locked: true }), summary('5', 'Old', { deleted_at: '2026-02-01T00:00:00Z' })]
})

const pages = (w: ReturnType<typeof mount>) => w.findAll('[data-wiki-page]').map(b => b.attributes('data-wiki-page'))

describe('WikiTree', () => {
  it('shows the live pages nested, without the deleted ones', () => {
    const w = mount(WikiTree)
    expect(pages(w)).toEqual(['guides', 'raid', 'loot', 'rules'])
    expect(w.get('[data-wiki-page="loot"]').element.closest('li')?.getAttribute('data-depth')).toBe('3')
    expect(w.get('[data-wiki-page="rules"]').text()).toContain('Rules')
  })

  it('folds and unfolds a branch', async () => {
    const w = mount(WikiTree)
    await w.get('li[data-depth="1"] button[aria-label]').trigger('click')
    expect(pages(w)).toEqual(['guides', 'rules'])
    await w.get('li[data-depth="1"] button[aria-label]').trigger('click')
    expect(pages(w)).toEqual(['guides', 'raid', 'loot', 'rules'])
  })

  it('opens the branch of the page on screen', async () => {
    const w = mount(WikiTree)
    await w.get('li[data-depth="1"] button[aria-label]').trigger('click')
    navigate('/wiki/loot')
    await w.vm.$nextTick()
    expect(pages(w)).toEqual(['guides', 'raid', 'loot', 'rules'])
    expect(w.get('[data-wiki-page="loot"]').attributes('aria-current')).toBe('page')
  })

  it('opens a page and the start page', async () => {
    const w = mount(WikiTree)
    await w.get('[data-wiki-page="raid"]').trigger('click')
    expect(currentRoute.value).toMatchObject({ wiki: { mode: 'page', slug: 'raid' } })
    await w.get('[data-testid="wiki-home"]').trigger('click')
    expect(currentRoute.value).toMatchObject({ wiki: { mode: 'home' } })
  })

  it('offers New page to editors and the deleted pages to managers, with their count', async () => {
    expect(mount(WikiTree).find('[data-testid="wiki-new"]').exists()).toBe(false)
    expect(mount(WikiTree).find('[data-testid="wiki-deleted"]').exists()).toBe(false)
    grantWiki(VIEW_WIKI | EDIT_WIKI | MANAGE_WIKI)
    const w = mount(WikiTree)
    await w.get('[data-testid="wiki-new"]').trigger('click')
    expect(currentRoute.value).toMatchObject({ wiki: { mode: 'new' } })
    expect(w.get('[data-testid="wiki-deleted"]').text()).toContain('1')
    await w.get('[data-testid="wiki-deleted"]').trigger('click')
    expect(currentRoute.value).toMatchObject({ wiki: { mode: 'deleted' } })
  })

  it('says when there are no pages, or the tree could not be read', () => {
    const wiki = useWikiStore()
    wiki.pages = []
    const w = mount(WikiTree)
    expect(w.find('[data-testid="wiki-tree-empty"]').exists()).toBe(true)
    wiki.treeFailed = true
    return w.vm.$nextTick().then(() => {
      expect(w.find('[role="alert"]').exists()).toBe(true)
      expect(w.find('[data-testid="wiki-tree-empty"]').exists()).toBe(false)
    })
  })
})
