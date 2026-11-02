// The page tree as the sidebar of the wiki shows it: the flat list the server
// sends, nested by parent, plus what the screens ask of it.
import type { WikiPageSummary } from './wikiApi'

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

/** The slug a `[[Title]]` or `[[slug]]` points to, or null when no page matches. */
export function resolveWikiLink(pages: readonly WikiPageSummary[], target: string): string | null {
  const wanted = target.trim().toLowerCase()
  if (!wanted) return null
  const live = pages.filter(p => !p.deleted_at)
  const hit = live.find(p => p.title.toLowerCase() === wanted) ?? live.find(p => p.slug === wanted)
  return hit?.slug ?? null
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
