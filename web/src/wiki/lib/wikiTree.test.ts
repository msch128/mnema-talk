import { describe, expect, it } from 'vitest'
import type { WikiPageSummary } from './wikiApi'
import { breadcrumbs, buildTree, height, resolveWikiLink, subtreeSlugs, visibleNodes } from './wikiTree'

function page(id: string, title: string, parent: string | null = null, extra: Partial<WikiPageSummary> = {}): WikiPageSummary {
  return { id, slug: title.toLowerCase().replace(/ /g, '-'), title, parent_id: parent, position: 0, locked: false, updated_at: '2026-01-01T00:00:00Z', ...extra }
}

const pages = [page('1', 'Guides'), page('2', 'Raid guide', '1'), page('3', 'Loot', '2'), page('4', 'Rules'), page('5', 'Orphan', 'gone')]

describe('wiki tree', () => {
  it('nests by parent with depths, a missing parent counts as top', () => {
    const roots = buildTree(pages)
    expect(roots.map(n => n.slug)).toEqual(['guides', 'rules', 'orphan'])
    expect(roots[0]?.children[0]?.children[0]).toMatchObject({ slug: 'loot', depth: 3 })
    expect(height(roots[0]!)).toBe(3)
    expect(height(roots[1]!)).toBe(1)
  })

  it('leaves out what is below collapsed pages', () => {
    const roots = buildTree(pages)
    expect(visibleNodes(roots, new Set()).map(n => n.slug)).toEqual(['guides', 'raid-guide', 'loot', 'rules', 'orphan'])
    expect(visibleNodes(roots, new Set(['2'])).map(n => n.slug)).toEqual(['guides', 'raid-guide', 'rules', 'orphan'])
  })

  it('builds the trail from the top', () => {
    expect(breadcrumbs(pages, 'loot').map(p => p.slug)).toEqual(['guides', 'raid-guide', 'loot'])
    expect(breadcrumbs(pages, 'none')).toEqual([])
  })

  it('finds the slugs below a page, itself included', () => {
    const roots = buildTree(pages)
    expect([...subtreeSlugs(roots, 'raid-guide')].sort()).toEqual(['loot', 'raid-guide'])
    expect(subtreeSlugs(roots, 'nope').size).toBe(0)
  })

  it('resolves [[links]] by title first, then address, ignoring case; deleted pages do not count', () => {
    const all = [...pages, page('6', 'Gone', null, { deleted_at: '2026-02-01T00:00:00Z' }), page('7', 'Other', null, { slug: 'rules-2' })]
    expect(resolveWikiLink(all, 'RAID GUIDE')).toBe('raid-guide')
    expect(resolveWikiLink(all, ' rules-2 ')).toBe('rules-2')
    expect(resolveWikiLink(all, 'Rules')).toBe('rules')
    expect(resolveWikiLink(all, 'Gone')).toBeNull()
    expect(resolveWikiLink(all, '   ')).toBeNull()
    expect(resolveWikiLink(all, 'Missing')).toBeNull()
  })
})
