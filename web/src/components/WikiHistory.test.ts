import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import WikiHistory from './WikiHistory.vue'
import { ApiError } from '../lib/api'
import { pendingConfirm } from '../lib/confirm'
import { currentRoute, navigate } from '../lib/router'
import { useToastStore } from '../stores/toast'
import { useWikiStore } from '../stores/wiki'
import { EDIT_WIKI, grantWiki, MANAGE_WIKI, revision, VIEW_WIKI, wikiPage } from '../wiki.fixture'

const api = vi.hoisted(() => ({ fetchWikiRevisions: vi.fn(), fetchWikiRevision: vi.fn(), restoreWikiRevision: vi.fn() }))
vi.mock('../lib/wikiApi', () => api)

const full = (rev: number) => revision(rev, { content: `text ${rev}\nline` })

enableAutoUnmount(afterEach)

beforeEach(() => {
  setActivePinia(createPinia())
  Object.values(api).forEach(fn => fn.mockReset())
  api.fetchWikiRevisions.mockResolvedValue([revision(3), revision(2, { summary: 'typo', restored_from: 1 }), revision(1)])
  api.fetchWikiRevision.mockImplementation((_slug: string, rev: number) => Promise.resolve(full(rev)))
  grantWiki(VIEW_WIKI | EDIT_WIKI)
  navigate('/wiki/faq/history', { replace: true })
})
afterEach(() => { pendingConfirm.value?.resolve(false) })

async function open(page = wikiPage()) {
  const w = mount(WikiHistory, { props: { page } })
  await flushPromises()
  return w
}

describe('WikiHistory', () => {
  it('lists the revisions with editor, summary and where a restore came from, and selects the newest', async () => {
    const w = await open()
    const items = w.findAll('[data-rev]')
    expect(items.map(i => i.attributes('data-rev'))).toEqual(['3', '2', '1'])
    expect(items[1]?.text()).toContain('typo')
    expect(items[0]?.attributes('aria-current')).toBe('true')
    expect(w.get('[data-testid="wiki-revision"]').text()).toContain('text 3')
  })

  it('shows what the chosen revision changed against the one before it', async () => {
    const w = await open()
    await w.get('[data-rev="2"]').trigger('click')
    await flushPromises()
    expect(api.fetchWikiRevision).toHaveBeenCalledWith('faq', 1)
    expect(w.find('[data-op="del"]').text()).toContain('text 1')
    expect(w.find('[data-op="add"]').text()).toContain('text 2')
  })

  it('has no diff for the first revision', async () => {
    const w = await open()
    await w.get('[data-rev="1"]').trigger('click')
    await flushPromises()
    expect(w.findAll('[data-op="add"]').length).toBeGreaterThan(0)
    expect(w.find('[data-op="del"]').exists()).toBe(false)
  })

  it('restores an older revision after two clicks, as a new revision', async () => {
    api.restoreWikiRevision.mockResolvedValue(wikiPage({ rev: 4, content: 'text 2' }))
    const w = await open()
    await w.get('[data-rev="2"]').trigger('click')
    await flushPromises()
    await w.get('[data-testid="wiki-restore"]').trigger('click')
    expect(api.restoreWikiRevision).not.toHaveBeenCalled() // the second click is the confirmation
    await vi.waitFor(() => expect(pendingConfirm.value).not.toBeNull())
    pendingConfirm.value?.resolve(true)
    await flushPromises()
    expect(api.restoreWikiRevision).toHaveBeenCalledWith('faq', 2, { base_rev: 3 })
    expect(useWikiStore().page?.rev).toBe(4)
    expect(currentRoute.value).toMatchObject({ wiki: { mode: 'page', slug: 'faq' } })
  })

  it('does nothing when the confirmation is declined', async () => {
    const w = await open()
    await w.get('[data-rev="2"]').trigger('click')
    await flushPromises()
    await w.get('[data-testid="wiki-restore"]').trigger('click')
    await vi.waitFor(() => expect(pendingConfirm.value).not.toBeNull())
    pendingConfirm.value?.resolve(false)
    await flushPromises()
    expect(api.restoreWikiRevision).not.toHaveBeenCalled()
  })

  it('offers no restore of the current revision, nor to a reader, nor on a locked page without Manage', async () => {
    let w = await open()
    expect(w.find('[data-testid="wiki-restore"]').exists()).toBe(false)
    await w.get('[data-rev="2"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="wiki-restore"]').exists()).toBe(true)
    grantWiki(VIEW_WIKI)
    w = await open()
    await w.get('[data-rev="2"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="wiki-restore"]').exists()).toBe(false)
    grantWiki(VIEW_WIKI | EDIT_WIKI)
    w = await open(wikiPage({ locked: true }))
    await w.get('[data-rev="2"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="wiki-restore"]').exists()).toBe(false)
    grantWiki(VIEW_WIKI | EDIT_WIKI | MANAGE_WIKI)
    w = await open(wikiPage({ locked: true }))
    await w.get('[data-rev="2"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="wiki-restore"]').exists()).toBe(true)
  })

  it('says the page moved on when the restore is stale, and reloads the list', async () => {
    api.restoreWikiRevision.mockRejectedValue(new ApiError(409, 'CONFLICT', 'the page changed since you opened it; open it again and apply your change to the new version'))
    const w = await open()
    await w.get('[data-rev="2"]').trigger('click')
    await flushPromises()
    await w.get('[data-testid="wiki-restore"]').trigger('click')
    await vi.waitFor(() => expect(pendingConfirm.value).not.toBeNull())
    pendingConfirm.value?.resolve(true)
    await flushPromises()
    expect(useToastStore().toasts.some(t => t.type === 'error')).toBe(true)
    expect(api.fetchWikiRevisions).toHaveBeenCalledTimes(2)
  })

  it('continues the list below the oldest shown revision', async () => {
    const first = Array.from({ length: 50 }, (_, i) => revision(100 - i))
    api.fetchWikiRevisions.mockResolvedValueOnce(first).mockResolvedValueOnce([revision(50), revision(49)])
    const w = await open(wikiPage({ rev: 100 }))
    await w.get('[data-testid="wiki-older"]').trigger('click')
    await flushPromises()
    expect(api.fetchWikiRevisions).toHaveBeenLastCalledWith('faq', 51)
    expect(w.findAll('[data-rev]')).toHaveLength(52)
    expect(w.find('[data-testid="wiki-older"]').exists()).toBe(false)
  })

  it('shows a failed load and a failed revision', async () => {
    api.fetchWikiRevisions.mockRejectedValue(new Error('offline'))
    let w = await open()
    expect(w.find('[role="alert"]').exists()).toBe(true)
    api.fetchWikiRevisions.mockResolvedValue([revision(2), revision(1)])
    api.fetchWikiRevision.mockRejectedValue(new Error('gone'))
    w = await open()
    expect(useToastStore().toasts.length).toBeGreaterThan(0)
    expect(w.find('[data-testid="wiki-revision"]').exists()).toBe(false)
  })

  it('renders the text of a revision through the safe renderer', async () => {
    api.fetchWikiRevision.mockResolvedValue(revision(3, { content: '<script>alert(1)</script> hi' }))
    const w = await open()
    expect(w.find('[data-testid="wiki-revision"] script').exists()).toBe(false)
  })
})
