// Drop rules for the sidebar: where a dragged channel or category lands, given
// what the pointer is over. The layout functions themselves are in channelLayout.
import type { LayoutChannel, LayoutCategory, ChannelTree } from './channelTree'
import { containers, channelsIn, locateChannel, locateCategory, gapToIndex } from './channelLayout'

export type DropHit =
  | { zone: 'channel' | 'header' | 'empty'; id: string; ratio?: number }
  | { zone: 'section' | 'end'; id: string | null; ratio?: number }
  | { zone: 'start' }
export interface DropIndicator { key: string; edge: 'top' | 'bottom' | 'inside' }
export interface DropTarget { categoryId: string | null; gap: number; index: number; indicator: DropIndicator | null }

//
// The sidebar measures what the pointer is over and passes a "hit":
//   { zone: 'channel', id, ratio }  a channel row; ratio 0 (top) … 1 (bottom),
//                                   the voice users under a row count as 1
//   { zone: 'header', id, ratio }   a category header
//   { zone: 'empty', id }           the "no channels" line of an empty category
//   { zone: 'end', id }             free space after a container (id null = uncategorized)
//   { zone: 'start' }               above everything
// and for category drags
//   { zone: 'section', id, ratio }  a whole category (or the uncategorized group, id null)
//
// The result says where the item goes ({ categoryId, gap, index }) and where
// to draw the indicator: { key: 'channel:<id>' | 'header:<id>' | 'section:<id>',
// edge: 'top' | 'bottom' | 'inside' }. null means "no move" (dropping there
// would change nothing), and nothing is drawn.

const HEADER_EDGE = 0.25

type Tree<C extends LayoutChannel, K extends LayoutCategory<C>> = ChannelTree<C, K>

/** Where in which container a channel would go, before its index is worked out. */
interface Placement { categoryId: string | null; gap: number; indicator: DropIndicator | null }

interface DropContext {
  isCollapsed: (id: string) => boolean
  /** The container the dragged channel sits in now. */
  fromCategoryId: string | null
}

function lastKey<C extends LayoutChannel, K extends LayoutCategory<C>>(tree: Tree<C, K>, categoryId: string | null) {
  const list = channelsIn(tree, categoryId)
  const last = list?.[list.length - 1]
  return last ? `channel:${last.id}` : null
}

// The indicator for "at the end of this container".
function endIndicator<C extends LayoutChannel, K extends LayoutCategory<C>>(tree: Tree<C, K>, categoryId: string | null): DropIndicator | null {
  const last = lastKey(tree, categoryId)
  if (last) return { key: last, edge: 'bottom' }
  if (categoryId != null) return { key: `header:${categoryId}`, edge: 'bottom' }
  const first = tree.categories?.[0]
  return first ? { key: `header:${first.id}`, edge: 'top' } : null
}

// Dropping a channel on the collapsed category it already sits in changes nothing.
function intoCollapsed<C extends LayoutChannel, K extends LayoutCategory<C>>(tree: Tree<C, K>, id: string, ctx: DropContext): Placement | null {
  if (ctx.fromCategoryId === id) return null
  return { categoryId: id, gap: channelsIn(tree, id)?.length || 0, indicator: { key: `header:${id}`, edge: 'inside' } }
}

function atEnd<C extends LayoutChannel, K extends LayoutCategory<C>>(tree: Tree<C, K>, id: string | null, ctx: DropContext): Placement | null {
  if (id != null && ctx.isCollapsed(id)) return intoCollapsed(tree, id, ctx)
  return { categoryId: id, gap: channelsIn(tree, id)?.length || 0, indicator: endIndicator(tree, id) }
}

function channelPlacement<C extends LayoutChannel, K extends LayoutCategory<C>>(tree: Tree<C, K>, id: string, ratio: number | undefined): Placement | null {
  const at = locateChannel(tree, id)
  if (!at) return null
  const before = (ratio ?? 0) < 0.5
  return { categoryId: at.categoryId, gap: at.index + (before ? 0 : 1), indicator: { key: `channel:${id}`, edge: before ? 'top' : 'bottom' } }
}

function isContainerOpen(container: { id: string | null }, ctx: DropContext): boolean {
  return container.id == null || !ctx.isCollapsed(container.id)
}

function shouldDropIntoPreviousContainer(prevOpen: boolean, ratio: number, collapsed: boolean): boolean {
  if (!prevOpen) return false
  const threshold = collapsed ? HEADER_EDGE : 0.5
  return ratio < threshold
}

function placeIntoPreviousContainer<C extends LayoutChannel, K extends LayoutCategory<C>>(
  tree: Tree<C, K>,
  prevId: string | null,
  id: string,
  ctx: DropContext
): Placement | null {
  const end = atEnd(tree, prevId, ctx)
  return end ? { ...end, indicator: { key: `header:${id}`, edge: 'top' } } : null
}

function headerPlacement<C extends LayoutChannel, K extends LayoutCategory<C>>(
  tree: Tree<C, K>,
  id: string,
  ratioHit: number | undefined,
  ctx: DropContext
): Placement | null {
  const list = containers(tree)
  const k = list.findIndex(c => c.id === id)
  if (k < 1) return null
  const prev = list[k - 1]
  if (!prev) return null

  const ratio = ratioHit ?? 0.5
  const collapsed = ctx.isCollapsed(id)
  const prevOpen = isContainerOpen(prev, ctx)
  if (shouldDropIntoPreviousContainer(prevOpen, ratio, collapsed)) {
    return placeIntoPreviousContainer(tree, prev.id, id, ctx)
  }
  if (collapsed) return intoCollapsed(tree, id, ctx)
  return { categoryId: id, gap: 0, indicator: { key: `header:${id}`, edge: 'bottom' } }
}

function startPlacement<C extends LayoutChannel, K extends LayoutCategory<C>>(tree: Tree<C, K>): Placement {
  const first = tree.uncategorized?.[0]
  return { categoryId: null, gap: 0, indicator: first ? { key: `channel:${first.id}`, edge: 'top' } : endIndicator(tree, null) }
}

function placeChannel<C extends LayoutChannel, K extends LayoutCategory<C>>(tree: Tree<C, K>, hit: DropHit, ctx: DropContext): Placement | null {
  switch (hit.zone) {
    case 'channel':
      return channelPlacement(tree, hit.id, hit.ratio)
    case 'header':
      return headerPlacement(tree, hit.id, hit.ratio, ctx)
    case 'empty':
      if (!channelsIn(tree, hit.id)) return null
      return { categoryId: hit.id, gap: 0, indicator: { key: `header:${hit.id}`, edge: 'bottom' } }
    case 'end':
      if (!channelsIn(tree, hit.id ?? null)) return null
      return atEnd(tree, hit.id ?? null, ctx)
    case 'start':
      return startPlacement(tree)
    default:
      return null
  }
}

/**
 * Where a dragged channel lands. opts.isCollapsed(categoryId) tells whether
 * a category's channels are hidden right now.
 */
export function resolveChannelDrop<C extends LayoutChannel, K extends LayoutCategory<C>>(tree: Tree<C, K>, channelId: string, hit: DropHit | null, { isCollapsed = () => false }: { isCollapsed?: (id: string) => boolean } = {}): DropTarget | null {
  const from = locateChannel(tree, channelId)
  if (!from || !hit) return null
  const placement = placeChannel(tree, hit, { isCollapsed, fromCategoryId: from.categoryId })
  if (!placement) return null
  const { categoryId, gap, indicator } = placement
  const index = gapToIndex(from.index, from.categoryId === categoryId, gap)
  if (from.categoryId === categoryId && index === from.index) return null
  return { categoryId, gap, index, indicator }
}

function calculateCategoryGap<C extends LayoutChannel, K extends LayoutCategory<C>>(
  tree: ChannelTree<C, K>,
  hit: DropHit,
  catsLength: number
): number | null {
  if (hit.zone === 'start' || (hit.zone === 'section' && hit.id == null)) {
    return 0
  }
  if (hit.zone === 'end') {
    return catsLength
  }
  if (hit.zone === 'section') {
    const at = locateCategory(tree, hit.id)
    if (!at) return null
    const before = (hit.ratio ?? 0) < 0.5
    return at.index + (before ? 0 : 1)
  }
  return null
}

function resolveCategoryIndicator<C extends LayoutChannel, K extends LayoutCategory<C>>(
  cats: K[],
  gap: number
): DropIndicator | null {
  const last = cats[cats.length - 1]
  if (!last) return null
  const at = cats[gap]
  if (at) {
    return { key: `header:${at.id}`, edge: 'top' }
  }
  return { key: `section:${last.id}`, edge: 'bottom' }
}

/** Where a dragged category lands (it never goes into another one). */
export function resolveCategoryDrop<C extends LayoutChannel, K extends LayoutCategory<C>>(
  tree: ChannelTree<C, K>,
  categoryId: string,
  hit: DropHit | null
): DropTarget | null {
  const from = locateCategory(tree, categoryId)
  if (!from || !hit) return null
  const cats = tree.categories
  const gap = calculateCategoryGap(tree, hit, cats.length)
  if (gap === null) return null
  const index = gapToIndex(from.index, true, gap)
  if (index === from.index) return null
  const indicator = resolveCategoryIndicator(cats, gap)
  if (!indicator) return null
  return { categoryId: null, gap, index, indicator }
}
