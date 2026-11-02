import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import WikiPageView from './WikiPageView.vue'
import { ApiError } from '../lib/api'
import { pendingConfirm } from '../lib/confirm'
import { currentRoute, navigate } from '../lib/router'
import { useToastStore } from '../stores/toast'
import { useWikiStore } from '../stores/wiki'
import { EDIT_WIKI, grantWiki, MANAGE_WIKI, summary, VIEW_WIKI, wikiPage } from '../wiki.fixture'

const api = vi.hoisted(() => ({ deleteWikiPage: vi.fn(), undeleteWikiPage: vi.fn(), lockWikiPage: vi.fn(), changeWikiPage: vi.fn(), fetchWikiTree: vi.fn() }))
vi.mock('../lib/wikiApi', () => api)
const copy = vi.hoisted(() => vi.fn())
vi.mock('../lib/clipboard', () => ({ copyToClipboard: copy }))

enableAutoUnmount(afterEach)

beforeEach(() => {
  setActivePinia(createPinia())
  Object.values(api).forEach(fn => fn.mockReset())
  api.fetchWikiTree.mockResolvedValue([])
  copy.mockReset()
  grantWiki(VIEW_WIKI)
  navigate('/wiki/faq', { replace: true })
})
afterEach(() => { pendingConfirm.value?.resolve(false) })

const show = (page = wikiPage()) => mount(WikiPageView, { props: { page } })
const btn = (w: ReturnType<typeof show>, id: string) => w.find(`[data-testid="wiki-${id}"]`)

describe('WikiPageView', () => {
  it('shows title, text, editor and revision; a reader gets no editing tools', () => {
    const w = show()
    expect(btn(w, 'title').text()).toBe('FAQ')
    expect(w.get('.markdown-body').text()).toContain('Hello')
    expect(w.text()).toContain('Eda')
    for (const id of ['edit', 'lock', 'move', 'delete']) expect(btn(w, id).exists()).toBe(false)
    expect(btn(w, 'history').exists()).toBe(true)
  })

  it('offers Edit to editors, but a locked page only to managers', () => {
    grantWiki(VIEW_WIKI | EDIT_WIKI)
    expect(btn(show(), 'edit').exists()).toBe(true)
    expect(btn(show(wikiPage({ locked: true })), 'edit').exists()).toBe(false)
    expect(btn(show(wikiPage({ locked: true })), 'locked').exists()).toBe(true)
    grantWiki(VIEW_WIKI | EDIT_WIKI | MANAGE_WIKI)
    expect(btn(show(wikiPage({ locked: true })), 'edit').exists()).toBe(true)
  })

  it('opens edit and history', async () => {
    grantWiki(VIEW_WIKI | EDIT_WIKI)
    const w = show()
    await btn(w, 'edit').trigger('click')
    expect(currentRoute.value).toMatchObject({ wiki: { mode: 'edit', slug: 'faq' } })
    await btn(w, 'history').trigger('click')
    expect(currentRoute.value).toMatchObject({ wiki: { mode: 'history', slug: 'faq' } })
  })

  it('shows the way down from the parents and to the subpages', async () => {
    useWikiStore().pages = [summary('top', 'Guides'), summary('p1', 'FAQ', { parent_id: 'top' }), summary('c1', 'Child', { parent_id: 'p1' })]
    const w = show(wikiPage({ parent_id: 'top' }))
    const crumbs = w.get('nav')
    expect(crumbs.text()).toContain('Guides')
    await crumbs.get('button').trigger('click')
    expect(currentRoute.value).toMatchObject({ wiki: { slug: 'guides' } })
    await w.get('section button').trigger('click')
    expect(currentRoute.value).toMatchObject({ wiki: { slug: 'child' } })
  })

  it('says a page is empty and where a redirect came from', () => {
    const w = show(wikiPage({ content: '', redirected_from: 'old-faq' }))
    expect(btn(w, 'empty-page').exists()).toBe(true)
    expect(btn(w, 'redirected').text()).toContain('old-faq')
  })

  it('shows a page without a known editor', () => {
    const w = show((() => { const p = wikiPage(); delete p.updated_by; return p })())
    expect(w.text()).not.toContain('Eda')
  })

  it('copies the address of the page', async () => {
    const w = show()
    await w.get('button[aria-label]:not([data-testid])').trigger('click')
    expect(copy).toHaveBeenCalledWith(`${window.location.origin}/wiki/faq`, expect.any(String))
  })

  describe('as a manager', () => {
    beforeEach(() => grantWiki(VIEW_WIKI | EDIT_WIKI | MANAGE_WIKI))

    it('locks and unlocks', async () => {
      api.lockWikiPage.mockResolvedValue(wikiPage({ locked: true }))
      const w = show()
      await btn(w, 'lock').trigger('click')
      await flushPromises()
      expect(api.lockWikiPage).toHaveBeenCalledWith('faq', true)
      expect(useWikiStore().page?.locked).toBe(true)
      api.lockWikiPage.mockResolvedValue(wikiPage({ locked: false }))
      await w.setProps({ page: wikiPage({ locked: true }) })
      await btn(w, 'lock').trigger('click')
      await flushPromises()
      expect(api.lockWikiPage).toHaveBeenLastCalledWith('faq', false)
    })

    it('shows a refusal as a toast', async () => {
      api.lockWikiPage.mockRejectedValue(new ApiError(403, 'FORBIDDEN', 'missing permission'))
      const w = show()
      await btn(w, 'lock').trigger('click')
      await flushPromises()
      expect(useToastStore().toasts.at(-1)?.type).toBe('error')
    })

    it('deletes after a confirmation and returns to the start', async () => {
      api.deleteWikiPage.mockResolvedValue(wikiPage({ deleted_at: '2026-04-01T00:00:00Z' }))
      const w = show()
      await btn(w, 'delete').trigger('click')
      await vi.waitFor(() => expect(pendingConfirm.value).not.toBeNull())
      expect(api.deleteWikiPage).not.toHaveBeenCalled()
      pendingConfirm.value?.resolve(true)
      await flushPromises()
      expect(api.deleteWikiPage).toHaveBeenCalledWith('faq')
      expect(currentRoute.value).toMatchObject({ wiki: { mode: 'home' } })
    })

    it('keeps the page when the confirmation is declined', async () => {
      const w = show()
      await btn(w, 'delete').trigger('click')
      await vi.waitFor(() => expect(pendingConfirm.value).not.toBeNull())
      pendingConfirm.value?.resolve(false)
      await flushPromises()
      expect(api.deleteWikiPage).not.toHaveBeenCalled()
    })

    it('restores a deleted page and offers no editing on it', async () => {
      api.undeleteWikiPage.mockResolvedValue(wikiPage())
      const w = show(wikiPage({ deleted_at: '2026-04-01T00:00:00Z' }))
      expect(btn(w, 'deleted-banner').exists()).toBe(true)
      expect(btn(w, 'edit').exists()).toBe(false)
      expect(btn(w, 'delete').exists()).toBe(false)
      await btn(w, 'undelete').trigger('click')
      await flushPromises()
      expect(api.undeleteWikiPage).toHaveBeenCalledWith('faq')
      expect(useToastStore().toasts.at(-1)?.type).toBe('success')
    })

    it('opens the move form and closes it again', async () => {
      const w = show()
      await btn(w, 'move').trigger('click')
      expect(btn(w, 'move-form').exists()).toBe(true)
      await btn(w, 'move').trigger('click')
      expect(btn(w, 'move-form').exists()).toBe(false)
    })
  })
})
