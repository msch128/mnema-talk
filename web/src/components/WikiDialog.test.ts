import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import WikiDialog from './WikiDialog.vue'
import { ApiError } from '../shared/lib/api'
import { pendingConfirm } from '../shared/lib/confirm'
import { currentRoute, navigate } from '../shared/lib/router'
import { wikiDirty, wikiQuery } from '../lib/wikiNav'
import { loadWikiDraft, wikiDraftKey } from '../lib/wikiDraft'
import { useWikiStore } from '../stores/wiki'
import { useCompactLayout } from '../shared/composables/useCompactLayout'
import { EDIT_WIKI, grantWiki, MANAGE_WIKI, summary, VIEW_WIKI, wikiPage } from '../wiki.fixture'

const api = vi.hoisted(() => ({ fetchWikiTree: vi.fn(), fetchWikiPage: vi.fn(), searchWiki: vi.fn(), fetchWikiRevisions: vi.fn(), fetchWikiRevision: vi.fn() }))
vi.mock('../lib/wikiApi', () => api)

enableAutoUnmount(afterEach)

beforeEach(() => {
  setActivePinia(createPinia())
  Object.values(api).forEach(fn => fn.mockReset())
  api.fetchWikiTree.mockResolvedValue([summary('p1', 'FAQ')])
  api.fetchWikiPage.mockResolvedValue(wikiPage())
  api.searchWiki.mockResolvedValue([])
  api.fetchWikiRevisions.mockResolvedValue([])
  grantWiki(VIEW_WIKI | EDIT_WIKI)
  wikiDirty.value = false
  wikiQuery.value = ''
})
afterEach(() => { pendingConfirm.value?.resolve(false) })

async function openAt(path: string) {
  window.history.replaceState({ idx: 0 }, '', path)
  navigate(path, { replace: true })
  const w = mount(WikiDialog, { attachTo: document.body })
  await flushPromises()
  return w
}

describe('WikiDialog', () => {
  it('shows the start page and reads the tree', async () => {
    const w = await openAt('/wiki')
    expect(w.find('[data-testid="wiki-start"]').exists()).toBe(true)
    expect(api.fetchWikiTree).toHaveBeenCalled()
    expect(w.find('[role="dialog"]').exists()).toBe(true)
  })

  it('shows the page the address names', async () => {
    const w = await openAt('/wiki/faq')
    expect(api.fetchWikiPage).toHaveBeenCalledWith('faq')
    expect(w.get('[data-testid="wiki-title"]').text()).toBe('FAQ')
  })

  it('moves the address to the new name after a rename', async () => {
    api.fetchWikiPage.mockResolvedValue(wikiPage({ slug: 'help', redirected_from: 'faq' }))
    const w = await openAt('/wiki/faq')
    expect(currentRoute.value).toMatchObject({ wiki: { mode: 'page', slug: 'help' } })
    expect(w.find('[data-testid="wiki-redirected"]').exists()).toBe(true)
  })

  it('keeps the edit and history modes when it follows a rename', async () => {
    api.fetchWikiPage.mockResolvedValue(wikiPage({ slug: 'help' }))
    await openAt('/wiki/faq/edit')
    expect(currentRoute.value).toMatchObject({ wiki: { mode: 'edit', slug: 'help' } })
  })

  it.each([[404, 'wiki-page-error'], [403, 'wiki-page-error'], [500, 'wiki-page-error']])('says why a page cannot be shown (%i)', async (status, id) => {
    api.fetchWikiPage.mockRejectedValue(new ApiError(status, 'X', 'no'))
    const w = await openAt('/wiki/faq')
    expect(w.find(`[data-testid="${id}"]`).exists()).toBe(true)
    await w.get(`[data-testid="${id}"] button`).trigger('click')
    expect(currentRoute.value).toMatchObject({ wiki: { mode: 'home' } })
  })

  it('opens the editor for a page and for a new page', async () => {
    let w = await openAt('/wiki/faq/edit')
    expect(w.find('[data-testid="wiki-editor"]').exists()).toBe(true)
    w = await openAt('/wiki/new')
    expect(w.find('[data-testid="wiki-editor"]').exists()).toBe(true)
  })

  it('does not open an editor for those who may not edit', async () => {
    grantWiki(VIEW_WIKI)
    let w = await openAt('/wiki/new')
    expect(w.find('[data-testid="wiki-editor"]').exists()).toBe(false)
    expect(w.find('[data-testid="wiki-no-edit"]').exists()).toBe(true)
    w = await openAt('/wiki/faq/edit')
    expect(w.find('[data-testid="wiki-editor"]').exists()).toBe(false)
    expect(w.find('[data-testid="wiki-no-edit"]').exists()).toBe(true)
  })

  it('refuses the editor on a locked page unless the member manages the wiki', async () => {
    api.fetchWikiPage.mockResolvedValue(wikiPage({ locked: true }))
    let w = await openAt('/wiki/faq/edit')
    expect(w.find('[data-testid="wiki-editor"]').exists()).toBe(false)
    grantWiki(VIEW_WIKI | EDIT_WIKI | MANAGE_WIKI)
    w = await openAt('/wiki/faq/edit')
    expect(w.find('[data-testid="wiki-editor"]').exists()).toBe(true)
  })

  it('shows the history, the deleted pages and the search', async () => {
    grantWiki(VIEW_WIKI | EDIT_WIKI | MANAGE_WIKI)
    expect((await openAt('/wiki/faq/history')).find('[data-testid="wiki-history-view"]').exists()).toBe(true)
    expect((await openAt('/wiki/deleted')).find('[data-testid="wiki-deleted-view"]').exists()).toBe(true)
    expect((await openAt('/wiki/search')).find('[data-testid="wiki-search"]').exists()).toBe(true)
  })

  it('opens the search from the box in the header', async () => {
    const w = await openAt('/wiki')
    const input = w.get('[data-testid="wiki-search-input"]')
    await input.setValue('loot')
    expect(wikiQuery.value).toBe('loot')
    expect(currentRoute.value).toMatchObject({ wiki: { mode: 'search' } })
    await input.setValue('loot r')
    expect(wikiQuery.value).toBe('loot r')
  })

  it('has a New page button for editors only', async () => {
    let w = await openAt('/wiki')
    await w.get('[data-testid="wiki-new-top"]').trigger('click')
    expect(currentRoute.value).toMatchObject({ wiki: { mode: 'new' } })
    grantWiki(VIEW_WIKI)
    w = await openAt('/wiki')
    expect(w.find('[data-testid="wiki-new-top"]').exists()).toBe(false)
  })

  it('closes with the button and leaves the wiki address', async () => {
    const w = await openAt('/wiki')
    await w.get('[data-testid="wiki-close"]').trigger('click')
    await flushPromises()
    expect(currentRoute.value.view).not.toBe('wiki')
  })

  it('asks before an unsaved page is closed, and stays when the member declines', async () => {
    const w = await openAt('/wiki/faq/edit')
    await w.get('[data-testid="wiki-text-input"]').setValue('changed')
    await w.get('[data-testid="wiki-close"]').trigger('click')
    await vi.waitFor(() => expect(pendingConfirm.value).not.toBeNull())
    pendingConfirm.value?.resolve(false)
    await flushPromises()
    expect(currentRoute.value).toMatchObject({ wiki: { mode: 'edit' } })
    expect((w.get('[data-testid="wiki-text-input"]').element as HTMLTextAreaElement).value).toBe('changed')
  })

  it('shows the page tree', async () => {
    useWikiStore().pages = [summary('p1', 'FAQ')]
    const w = await openAt('/wiki')
    expect(w.find('[data-wiki-page="faq"]').exists()).toBe(true)
  })

  it('reads a page from the server when it opens, even when an older copy was kept', async () => {
    const store = useWikiStore()
    store.applyPage(wikiPage({ rev: 1, content: 'old copy' }))
    api.fetchWikiPage.mockResolvedValue(wikiPage({ rev: 2, content: 'new text' }))
    const w = await openAt('/wiki/faq')
    expect(api.fetchWikiPage).toHaveBeenCalledWith('faq')
    expect(w.get('[data-testid="wiki-content"]').text()).toContain('new text')
  })

  it('reads the tree again when the right to manage comes or goes', async () => {
    await openAt('/wiki')
    expect(api.fetchWikiTree).toHaveBeenLastCalledWith(false)
    grantWiki(VIEW_WIKI | EDIT_WIKI | MANAGE_WIKI)
    await flushPromises()
    expect(api.fetchWikiTree).toHaveBeenLastCalledWith(true)
  })

  it('shows what a member wrote when the right to edit goes away meanwhile, and keeps it', async () => {
    const w = await openAt('/wiki/faq/edit')
    await w.get('[data-testid="wiki-text-input"]').setValue('hours of work')
    grantWiki(VIEW_WIKI)
    await flushPromises()
    expect(w.find('[data-testid="wiki-editor"]').exists()).toBe(false)
    expect(w.find('[data-testid="wiki-no-edit"]').exists()).toBe(true)
    expect((w.get('[data-testid="wiki-held-text"]').element as HTMLTextAreaElement).value).toContain('hours of work')
    expect(loadWikiDraft(wikiDraftKey('', 'faq'))).toMatchObject({ content: 'hours of work' })
    expect(wikiDirty.value).toBe(true)

    // Leaving asks, and a yes lets the text go.
    const close = w.get('[data-testid="wiki-close"]').trigger('click')
    await vi.waitFor(() => expect(pendingConfirm.value).not.toBeNull())
    pendingConfirm.value?.resolve(true)
    await close
    await flushPromises()
    expect(loadWikiDraft(wikiDraftKey('', 'faq'))).toBeNull()
  })

  it('lets the held text be discarded on request', async () => {
    const w = await openAt('/wiki/faq/edit')
    await w.get('[data-testid="wiki-text-input"]').setValue('hours of work')
    grantWiki(VIEW_WIKI)
    await flushPromises()
    await w.get('[data-testid="wiki-held-discard"]').trigger('click')
    await vi.waitFor(() => expect(pendingConfirm.value).not.toBeNull())
    pendingConfirm.value?.resolve(true)
    await flushPromises()
    expect(w.find('[data-testid="wiki-held-draft"]').exists()).toBe(false)
    expect(wikiDirty.value).toBe(false)
    expect(loadWikiDraft(wikiDraftKey('', 'faq'))).toBeNull()
  })

  it('closes the page list on a narrow screen with a tap beside it, and gives focus back to its button', async () => {
    const { isCompact } = useCompactLayout()
    isCompact.value = true
    try {
      const w = await openAt('/wiki')
      const toggle = w.get('[data-testid="wiki-tree-toggle"]')
      await toggle.trigger('click')
      await flushPromises()
      expect(toggle.attributes('aria-expanded')).toBe('true')
      expect(w.get('[data-testid="wiki-tree"]').element.contains(document.activeElement)).toBe(true)
      await w.get('[data-testid="wiki-tree-scrim"]').trigger('click')
      await flushPromises()
      expect(toggle.attributes('aria-expanded')).toBe('false')
      expect(w.find('[data-testid="wiki-tree-scrim"]').exists()).toBe(false)
      expect(document.activeElement).toBe(toggle.element)
    } finally {
      isCompact.value = false
    }
  })
})
