// The page tree as the sidebar of the wiki shows it: the flat list the server
// sends, nested by parent, plus what the screens ask of it.
import { computed, type Ref } from 'vue'
import type { WikiPageSummary } from './wikiApi'
import { wikiTitleKey } from './wikiTitleKey'

export interface WikiNode extends WikiPageSummary {
  children: WikiNode[]
  depth: number
}

/** Nests the pages; siblings keep the order the server sent (position, then title). A page whose parent is missing counts as a top page. */
export function buildTree(pages: readonly WikiPageSummary[]): WikiNode[] {
  const nodes = new Map<string, WikiNode>()
  for (const p of pages) nodes.set(p.id, { ...p, children: [], depth: 1 })
  const roots: WikiNode[] = []
  for (const node of nodes.values()) {
    const parent = node.parent_id ? nodes.get(node.parent_id) : undefined
    if (parent) parent.children.push(node)
    else roots.push(node)
  }
  const setDepth = (list: WikiNode[], depth: number) => {
    for (const n of list) {
      n.depth = depth
      setDepth(n.children, depth + 1)
    }
  }
  setDepth(roots, 1)
  return roots
}

/** The nodes in reading order, below the collapsed ones left out. */
export function visibleNodes(roots: readonly WikiNode[], collapsed: ReadonlySet<string>): WikiNode[] {
  const out: WikiNode[] = []
  const walk = (list: readonly WikiNode[]) => {
    for (const n of list) {
      out.push(n)
      if (!collapsed.has(n.id)) walk(n.children)
    }
  }
  walk(roots)
  return out
}

/** The pages from the top down to the page itself. */
export function breadcrumbs(pages: readonly WikiPageSummary[], slug: string): WikiPageSummary[] {
  const byId = new Map(pages.map(p => [p.id, p]))
  const trail: WikiPageSummary[] = []
  let at = pages.find(p => p.slug === slug)
  while (at && trail.length < 10) {
    trail.unshift(at)
    at = at.parent_id ? byId.get(at.parent_id) : undefined
  }
  return trail
}

/** Finds the slug a `[[Title]]` or `[[slug]]` points to, or null when no page matches. */
export type WikiLinkResolver = (target: string) => string | null

const resolvers = new WeakMap<readonly WikiPageSummary[], WikiLinkResolver>()

/**
 * The resolver for one page list. Titles compare by wikiTitleKey, as the
 * server does; a title match wins over an address. Two pages from before that
 * rule may share a key: the first in the server's tree order wins. Each
 * page's key is computed once per list (a page may hold thousands of links,
 * and the editor preview renders on every keystroke). The store replaces the
 * list on every change and never edits it in place, so a list's resolver is kept.
 */
export function wikiLinkResolver(pages: readonly WikiPageSummary[]): WikiLinkResolver {
  const known = resolvers.get(pages)
  if (known) return known
  const byKey = new Map<string, string>()
  const slugs = new Set<string>()
  for (const p of pages) {
    if (p.deleted_at) continue
    const key = wikiTitleKey(p.title)
    if (!byKey.has(key)) byKey.set(key, p.slug)
    slugs.add(p.slug)
  }
  const resolve: WikiLinkResolver = target => {
    const trimmed = target.trim()
    if (!trimmed) return null
    const slug = trimmed.toLowerCase()
    return byKey.get(wikiTitleKey(trimmed)) ?? (slugs.has(slug) ? slug : null)
  }
  resolvers.set(pages, resolve)
  return resolve
}

/** The slug a `[[Title]]` or `[[slug]]` points to, or null when no page matches (wikiLinkResolver). */
export function resolveWikiLink(pages: readonly WikiPageSummary[], target: string): string | null {
  return wikiLinkResolver(pages)(target)
}

/** The slugs of a page and everything below it (a page cannot move under these). */
export function subtreeSlugs(roots: readonly WikiNode[], slug: string): Set<string> {
  const out = new Set<string>()
  const collect = (n: WikiNode) => {
    out.add(n.slug)
    n.children.forEach(collect)
  }
  const find = (list: readonly WikiNode[]): WikiNode | undefined => {
    for (const n of list) {
      if (n.slug === slug) return n
      const inner = find(n.children)
      if (inner) return inner
    }
    return undefined
  }
  const start = find(roots)
  if (start) collect(start)
  return out
}

/** How many levels a node and its subpages take: 1 for a page without subpages. */
export function height(node: WikiNode): number {
  return 1 + Math.max(0, ...node.children.map(height))
}

/** What the tree store derives from the page list: the live pages as a tree, the deleted ones, the latest changes. */
export function wikiViews(pages: Ref<readonly WikiPageSummary[]>) {
  const live = computed(() => pages.value.filter(p => !p.deleted_at))
  return {
    live,
    tree: computed(() => buildTree(live.value)),
    deleted: computed(() => pages.value.filter(p => p.deleted_at)),
    recent: computed(() => [...live.value].sort((a, b) => b.updated_at.localeCompare(a.updated_at)).slice(0, 8))
  }
}
