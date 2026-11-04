import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from './api'
import {
  changeWikiPage, createWikiPage, deleteWikiPage, fetchWikiPage, fetchWikiRevision, fetchWikiRevisions, fetchWikiTree,
  lockWikiPage, restoreWikiRevision, searchWiki, undeleteWikiPage
} from './wikiApi'

vi.mock('./api', () => ({ api: vi.fn() }))

const call = vi.mocked(api)
const lastCall = () => call.mock.calls.at(-1) as [string, { method?: string; json?: unknown }]

beforeEach(() => {
  call.mockReset()
  call.mockResolvedValue({ pages: [] })
})

describe('wiki calls', () => {
  it('reads the tree, with the deleted pages on request', async () => {
    expect(await fetchWikiTree(false)).toEqual([])
    expect(lastCall()[0]).toBe('/api/wiki')
    await fetchWikiTree(true)
    expect(lastCall()[0]).toBe('/api/wiki?deleted=true')
  })

  it('encodes the address of a page', async () => {
    await fetchWikiPage('a b/c')
    expect(lastCall()[0]).toBe('/api/wiki/pages/a%20b%2Fc')
  })

  it('uses the method and body each change needs', async () => {
    await createWikiPage({ title: 'T', content: 'c' })
    expect(lastCall()).toMatchObject(['/api/wiki/pages', { method: 'POST', json: { title: 'T', content: 'c' } }])
    await changeWikiPage('faq', { base_rev: 2, content: 'x' })
    expect(lastCall()).toMatchObject(['/api/wiki/pages/faq', { method: 'PATCH', json: { base_rev: 2, content: 'x' } }])
    await deleteWikiPage('faq')
    expect(lastCall()).toMatchObject(['/api/wiki/pages/faq', { method: 'DELETE' }])
    await undeleteWikiPage('faq')
    expect(lastCall()).toMatchObject(['/api/wiki/pages/faq/undelete', { method: 'POST' }])
    await lockWikiPage('faq', true)
    expect(lastCall()).toMatchObject(['/api/wiki/pages/faq/lock', { method: 'PUT', json: { locked: true } }])
  })

  it('pages through revisions, reads one and restores one', async () => {
    await fetchWikiRevisions('faq')
    expect(lastCall()[0]).toBe('/api/wiki/pages/faq/revisions')
    await fetchWikiRevisions('faq', 51)
    expect(lastCall()[0]).toBe('/api/wiki/pages/faq/revisions?before=51')
    await fetchWikiRevision('faq', 7)
    expect(lastCall()[0]).toBe('/api/wiki/pages/faq/revisions/7')
    await restoreWikiRevision('faq', 7, { base_rev: 9 })
    expect(lastCall()).toMatchObject(['/api/wiki/pages/faq/revisions/7/restore', { method: 'POST', json: { base_rev: 9 } }])
  })

  it('encodes a search', async () => {
    await searchWiki('loot & greed')
    expect(lastCall()[0]).toBe('/api/wiki/search?q=loot%20%26%20greed')
  })
})
