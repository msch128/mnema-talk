// The cost class of link resolution: a page with thousands of [[links]] on a
// full wiki computes each page's title key once, not once per link (the
// editor preview renders the page on every keystroke).
import { describe, expect, it, vi } from 'vitest'
import { renderMarkdown } from '../../shared/lib/markdown'
import type { WikiPageSummary } from './wikiApi'
import { wikiLinkRule } from './wikiLinks'
import { wikiTitleKey } from './wikiTitleKey'

vi.mock('./wikiTitleKey', async importOriginal => {
  const real = await importOriginal<typeof import('./wikiTitleKey')>()
  return { wikiTitleKey: vi.fn(real.wikiTitleKey) }
})

const PAGES = 500
const LINKS = 2000
const pages: WikiPageSummary[] = Array.from({ length: PAGES }, (_, i) => ({
  id: `p${i}`, slug: `page-${i}`, title: `Page number ${i}`, parent_id: null, position: i, locked: false, updated_at: '2026-01-01T00:00:00Z'
}))

describe('wiki link resolution', () => {
  it('makes one pass over the pages for a text with 2,000 links, however often it renders', () => {
    const text = Array.from({ length: LINKS }, (_, i) => `[[Page number ${i % 700}]]`).join(' ')
    const key = vi.mocked(wikiTitleKey)
    key.mockClear()
    const first = renderMarkdown(text, { document: true, rules: [wikiLinkRule(pages)] })
    expect(key).toHaveBeenCalledTimes(PAGES + LINKS)
    key.mockClear()
    renderMarkdown(text, { document: true, rules: [wikiLinkRule(pages)] })
    expect(key).toHaveBeenCalledTimes(LINKS) // the same list: its keys are not computed again
    expect(first).toContain('data-wiki="page-499"')
  })
})
