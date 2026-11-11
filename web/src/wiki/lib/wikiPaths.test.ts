import { describe, expect, it } from 'vitest'
import { parseWikiLocation, wikiPath, type WikiLocation } from './wikiPaths'

describe('wiki paths', () => {
  it('reads the parts after /wiki', () => {
    expect(parseWikiLocation([])).toEqual({ mode: 'home' })
    expect(parseWikiLocation(['search'])).toEqual({ mode: 'search' })
    expect(parseWikiLocation(['new'])).toEqual({ mode: 'new' })
    expect(parseWikiLocation(['deleted'])).toEqual({ mode: 'deleted' })
    expect(parseWikiLocation(['faq'])).toEqual({ mode: 'page', slug: 'faq' })
    expect(parseWikiLocation(['faq', 'history'])).toEqual({ mode: 'history', slug: 'faq' })
    expect(parseWikiLocation(['faq', 'edit'])).toEqual({ mode: 'edit', slug: 'faq' })
    expect(parseWikiLocation(['faq', 'whatever'])).toEqual({ mode: 'page', slug: 'faq' })
  })

  it.each<WikiLocation>([
    { mode: 'home' }, { mode: 'search' }, { mode: 'new' }, { mode: 'deleted' },
    { mode: 'page', slug: 'faq' }, { mode: 'history', slug: 'raid-guide' }, { mode: 'edit', slug: 'a' }
  ])('writes and reads %j back', at => {
    const parts = wikiPath(at).split('/').filter(Boolean).slice(1).map(decodeURIComponent)
    expect(parseWikiLocation(parts)).toEqual(at)
  })

  it('encodes an address', () => {
    expect(wikiPath({ mode: 'page', slug: 'a b' })).toBe('/wiki/a%20b')
  })
})
