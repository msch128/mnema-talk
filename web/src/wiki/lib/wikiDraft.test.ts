import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { clearAllWikiDrafts, clearWikiDraft, loadWikiDraft, saveWikiDraft, wikiDraftKey } from './wikiDraft'

const draft = { title: 'FAQ', content: 'text', summary: 's', slug: '', parent: '', baseRev: 2 }

beforeEach(() => sessionStorage.clear())
afterEach(() => vi.unstubAllGlobals())

describe('the wiki draft of a tab', () => {
  it('keeps one draft per account and page, and one for a page that does not exist yet', () => {
    expect(new Set([wikiDraftKey('a', 'faq'), wikiDraftKey('b', 'faq'), wikiDraftKey('a', null), wikiDraftKey('a', 'new')]).size).toBe(4)
  })

  it('reads back what was saved and forgets it when cleared', () => {
    const key = wikiDraftKey('a', 'faq')
    saveWikiDraft(key, draft)
    expect(loadWikiDraft(key)).toEqual(draft)
    clearWikiDraft(key)
    expect(loadWikiDraft(key)).toBeNull()
  })

  it.each([
    ['not JSON', '{'],
    ['not an object', '[1]'],
    ['a field of the wrong type', JSON.stringify({ ...draft, content: 5 })],
    ['a fractional revision', JSON.stringify({ ...draft, baseRev: 1.5 })],
    ['a negative revision', JSON.stringify({ ...draft, baseRev: -1 })]
  ])('treats %s as no draft', (_what, raw) => {
    sessionStorage.setItem('k', raw)
    expect(loadWikiDraft('k')).toBeNull()
  })

  it('removes every wiki draft on sign-out and nothing else', () => {
    saveWikiDraft(wikiDraftKey('a', 'faq'), draft)
    saveWikiDraft(wikiDraftKey('a', null), draft)
    sessionStorage.setItem('other', 'x')
    clearAllWikiDrafts()
    expect(sessionStorage.length).toBe(1)
    expect(sessionStorage.getItem('other')).toBe('x')
  })

  it('carries on when the storage is blocked', () => {
    vi.stubGlobal('sessionStorage', {
      setItem: () => { throw new Error('full') },
      getItem: () => { throw new Error('blocked') },
      removeItem: () => { throw new Error('blocked') }
    })
    expect(() => saveWikiDraft('k', draft)).not.toThrow()
    expect(loadWikiDraft('k')).toBeNull()
    expect(() => clearWikiDraft('k')).not.toThrow()
  })
})
