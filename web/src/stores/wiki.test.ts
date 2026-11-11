import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { ApiError } from '../shared/lib/api'
import { useWikiStore } from './wiki'
import { wikiDirty, wikiQuery } from '../lib/wikiNav'
import { EDIT_WIKI, grantWiki, MANAGE_WIKI, summary, VIEW_WIKI, wikiPage } from '../wiki.fixture'

const api = vi.hoisted(() => ({ fetchWikiTree: vi.fn(), fetchWikiPage: vi.fn() }))
vi.mock('../lib/wikiApi', () => api)

beforeEach(() => {
  setActivePinia(createPinia())
  api.fetchWikiTree.mockReset()
  api.fetchWikiPage.mockReset()
  grantWiki(VIEW_WIKI)
})

describe('wiki store', () => {
  it('derives what the member may do from the permissions', () => {
    const wiki = useWikiStore()
    expect([wiki.canView, wiki.canEdit, wiki.canManage]).toEqual([true, false, false])
    grantWiki(VIEW_WIKI | EDIT_WIKI | MANAGE_WIKI)
    expect([wiki.canView, wiki.canEdit, wiki.canManage]).toEqual([true, true, true])
  })

  it('reads the tree, asking for deleted pages only as a manager', async () => {
    api.fetchWikiTree.mockResolvedValue([summary('1', 'Rules'), summary('2', 'Old', { deleted_at: '2026-02-01T00:00:00Z' })])
    const wiki = useWikiStore()
    await wiki.loadTree()
    expect(api.fetchWikiTree).toHaveBeenLastCalledWith(false)
    expect(wiki.treeLoaded).toBe(true)
    expect(wiki.live.map(p => p.slug)).toEqual(['rules'])
    expect(wiki.deleted.map(p => p.slug)).toEqual(['old'])
    grantWiki(VIEW_WIKI | EDIT_WIKI | MANAGE_WIKI)
    await wiki.loadTree()
    expect(api.fetchWikiTree).toHaveBeenLastCalledWith(true)
  })

  it('lists the most recent pages first, at most eight', async () => {
    api.fetchWikiTree.mockResolvedValue(Array.from({ length: 10 }, (_, i) => summary(String(i), `Page ${i}`, { updated_at: `2026-03-${String(i + 10)}T00:00:00Z` })))
    const wiki = useWikiStore()
    await wiki.loadTree()
    expect(wiki.recent).toHaveLength(8)
    expect(wiki.recent[0]?.slug).toBe('page-9')
  })

  it('marks a failed read and a plain error is logged, not thrown', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    api.fetchWikiTree.mockRejectedValue(new Error('offline'))
    const wiki = useWikiStore()
    await wiki.loadTree()
    expect(wiki.treeFailed).toBe(true)
    expect(wiki.treeLoaded).toBe(true)
    expect(warn).toHaveBeenCalled()
    api.fetchWikiTree.mockRejectedValue(new ApiError(500, 'INTERNAL_ERROR', 'x'))
    await wiki.loadTree()
    expect(warn).toHaveBeenCalledTimes(1)
    api.fetchWikiTree.mockResolvedValue([])
    await wiki.loadTree()
    expect(wiki.treeFailed).toBe(false)
    warn.mockRestore()
  })

  it('drops a stale tree answer', async () => {
    let first!: (v: unknown) => void
    api.fetchWikiTree.mockReturnValueOnce(new Promise(r => { first = r }))
    api.fetchWikiTree.mockResolvedValueOnce([summary('2', 'New')])
    const wiki = useWikiStore()
    const slow = wiki.loadTree()
    await wiki.loadTree()
    first([summary('1', 'Old')])
    await slow
    expect(wiki.pages.map(p => p.slug)).toEqual(['new'])
  })

  it('reads a page and says why it could not', async () => {
    const wiki = useWikiStore()
    api.fetchWikiPage.mockResolvedValueOnce(wikiPage())
    expect((await wiki.loadPage('faq'))?.slug).toBe('faq')
    expect(wiki.pageState).toBe('ready')
    for (const [status, state] of [[404, 'missing'], [403, 'denied'], [500, 'failed']] as const) {
      api.fetchWikiPage.mockRejectedValueOnce(new ApiError(status, 'X', 'no'))
      expect(await wiki.loadPage('faq')).toBeNull()
      expect(wiki.pageState).toBe(state)
      expect(wiki.page).toBeNull()
    }
    api.fetchWikiPage.mockRejectedValueOnce(new Error('network'))
    await wiki.loadPage('faq')
    expect(wiki.pageState).toBe('failed')
  })

  it('drops a stale page answer, success or failure', async () => {
    const wiki = useWikiStore()
    let slow!: (v: unknown) => void
    let slowFail!: (e: unknown) => void
    api.fetchWikiPage.mockReturnValueOnce(new Promise(r => { slow = r }))
    const a = wiki.loadPage('a')
    api.fetchWikiPage.mockReturnValueOnce(new Promise((_, rej) => { slowFail = rej }))
    const b = wiki.loadPage('b')
    api.fetchWikiPage.mockResolvedValueOnce(wikiPage({ slug: 'c' }))
    await wiki.loadPage('c')
    slow(wikiPage({ slug: 'a' }))
    slowFail(new ApiError(404, 'NOT_FOUND', 'x'))
    expect(await a).toBeNull()
    expect(await b).toBeNull()
    expect(wiki.page?.slug).toBe('c')
    expect(wiki.pageState).toBe('ready')
  })

  it('applies a saved page to the screen and to its place in the tree', () => {
    const wiki = useWikiStore()
    wiki.pages = [summary('p1', 'FAQ')]
    wiki.applyPage(wikiPage({ title: 'FAQ 2', locked: true }))
    expect(wiki.pages).toHaveLength(1)
    expect(wiki.pages[0]).toMatchObject({ title: 'FAQ 2', locked: true })
    expect(wiki.pages[0]).not.toHaveProperty('content')
    wiki.applyPage(wikiPage({ id: 'p9', slug: 'new', deleted_at: '2026-04-01T00:00:00Z' }))
    expect(wiki.pages).toHaveLength(2)
    expect(wiki.pages[1]?.deleted_at).toBe('2026-04-01T00:00:00Z')
  })

  it('resets everything', async () => {
    api.fetchWikiTree.mockResolvedValue([summary('1', 'A')])
    const wiki = useWikiStore()
    await wiki.loadTree()
    wiki.applyPage(wikiPage())
    wiki.reset()
    expect(wiki.pages).toEqual([])
    expect(wiki.page).toBeNull()
    expect(wiki.pageState).toBe('idle')
    expect(wiki.treeLoaded).toBe(false)
  })

  it('forgets the search text and the unsaved flag of the member who signed out', () => {
    const wiki = useWikiStore()
    wikiQuery.value = 'secret plans'
    wikiDirty.value = true
    wiki.reset()
    expect(wikiQuery.value).toBe('')
    expect(wikiDirty.value).toBe(false)
  })
})
