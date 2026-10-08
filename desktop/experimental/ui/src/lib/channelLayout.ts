// Channel layout: the order of the categories and where each channel sits.
// Shared by the sidebar (drag and drop, Alt+Arrow, saved at once) and the
// admin dashboard's Layout tab (edited locally, saved in one go).
//
// All functions work on the tree from buildChannelTree():
//   { uncategorized: Channel[], categories: [{ ...Category, channels: Channel[] }] }
// A "container" is the uncategorized list (id null) or one category. Text and
// voice channels are freely mixed inside a container.
//
// Nothing here mutates its input. A call that changes nothing returns the
// input tree itself, so `next === tree` means "no move".

import type { LayoutChannel, LayoutCategory, ChannelTree } from './channelTree'
import type { LayoutRequest } from '../types/domain'

export type DropHit =
  | { zone: 'channel' | 'header' | 'empty'; id: string; ratio?: number }
  | { zone: 'section' | 'end'; id: string | null; ratio?: number }
  | { zone: 'start' }
export interface DropIndicator { key: string; edge: 'top' | 'bottom' | 'inside' }
export interface DropTarget { categoryId: string | null; gap: number; index: number; indicator: DropIndicator | null }
const clamp = (n: number, min: number, max: number) => Math.min(max, Math.max(min, n))

/** The uncategorized list first, then every category, as { id, channels }. */
export function containers<C extends LayoutChannel, K extends LayoutCategory<C>>(tree: ChannelTree<C, K>) {
  return [
    { id: null, channels: tree.uncategorized || [] },
    ...(tree.categories || []).map(c => ({ id: c.id, channels: c.channels || [] }))
  ]
}

/** The channel list of a container, or null for an unknown category. */
export function channelsIn<C extends LayoutChannel, K extends LayoutCategory<C>>(tree: ChannelTree<C, K>, categoryId: string | null | undefined): C[] | null {
  if (categoryId == null) return tree.uncategorized || []
  const cat = (tree.categories || []).find(c => c.id === categoryId)
  return cat ? cat.channels || [] : null
}

/** Where a channel sits: { channel, categoryId, index, total } or null. */
export function locateChannel<C extends LayoutChannel, K extends LayoutCategory<C>>(tree: ChannelTree<C, K>, channelId: string) {
  for (const { id, channels } of containers(tree)) {
    const index = channels.findIndex(c => c.id === channelId)
    const channel = channels[index]
    if (index !== -1 && channel) return { channel, categoryId: id, index, total: channels.length }
  }
  return null
}

/** Where a category sits: { category, index, total } or null. */
export function locateCategory<C extends LayoutChannel, K extends LayoutCategory<C>>(tree: ChannelTree<C, K>, categoryId: string | null) {
  const list = tree.categories || []
  const index = list.findIndex(c => c.id === categoryId)
  const category = list[index]
  return index === -1 || !category ? null : { category, index, total: list.length }
}

function withChannels<C extends LayoutChannel, K extends LayoutCategory<C>>(tree: ChannelTree<C, K>, categoryId: string | null, channels: C[]): ChannelTree<C, K> {
  if (categoryId == null) return { ...tree, uncategorized: channels }
  return { ...tree, categories: tree.categories.map(c => (c.id === categoryId ? { ...c, channels } : c)) }
}

/**
 * Moves a channel into `targetCategoryId` (null = uncategorized) so that it
 * ends up at `index` there (clamped). Across containers the moved channel is
 * copied with its new category_id.
 */
export function moveChannel<C extends LayoutChannel, K extends LayoutCategory<C>>(tree: ChannelTree<C, K>, channelId: string, targetCategoryId: string | null | undefined, index: number): ChannelTree<C, K> {
  const target = targetCategoryId ?? null
  const from = locateChannel(tree, channelId)
  if (!from || !channelsIn(tree, target)) return tree

  const source = (channelsIn(tree, from.categoryId) || []).filter(c => c.id !== channelId)
  if (from.categoryId === target) {
    const at = clamp(index, 0, source.length)
    if (at === from.index) return tree
    source.splice(at, 0, from.channel)
    return withChannels(tree, target, source)
  }
  const dest = [...(channelsIn(tree, target) || [])]
  dest.splice(clamp(index, 0, dest.length), 0, { ...from.channel, category_id: target })
  return withChannels(withChannels(tree, from.categoryId, source), target, dest)
}

/** Moves a category so that it ends up at `index` (clamped). */
export function moveCategory<C extends LayoutChannel, K extends LayoutCategory<C>>(tree: ChannelTree<C, K>, categoryId: string, index: number): ChannelTree<C, K> {
  const from = locateCategory(tree, categoryId)
  if (!from) return tree
  const list = tree.categories.filter(c => c.id !== categoryId)
  const at = clamp(index, 0, list.length)
  if (at === from.index) return tree
  list.splice(at, 0, from.category)
  return { ...tree, categories: list }
}

/**
 * A drop "gap" counts slots in the list as it is shown, the moved item still
 * in place (gap 2 = between the 2nd and 3rd row). Returns the index the item
 * gets once it has left its old slot.
 */
export function gapToIndex(fromIndex: number, sameList: boolean, gap: number): number {
  return sameList && fromIndex < gap ? gap - 1 : gap
}

export function moveChannelToGap<C extends LayoutChannel, K extends LayoutCategory<C>>(tree: ChannelTree<C, K>, channelId: string, categoryId: string | null, gap: number): ChannelTree<C, K> {
  const from = locateChannel(tree, channelId)
  if (!from) return tree
  const index = gapToIndex(from.index, from.categoryId === (categoryId ?? null), gap)
  return moveChannel(tree, channelId, categoryId, index)
}

export function moveCategoryToGap<C extends LayoutChannel, K extends LayoutCategory<C>>(tree: ChannelTree<C, K>, categoryId: string, gap: number): ChannelTree<C, K> {
  const from = locateCategory(tree, categoryId)
  if (!from) return tree
  return moveCategory(tree, categoryId, gapToIndex(from.index, true, gap))
}

// ---- Keyboard (Alt+ArrowUp / Alt+ArrowDown) ----

/**
 * The slot one step up (dir < 0) or down (dir > 0) from a channel, or null at
 * the very top or bottom. Inside a container a channel swaps with its
 * neighbour. Past the first channel it goes to the end of the container above
 * (the previous category, or the uncategorized group above the first
 * category); past the last channel to the start of the next category. Empty
 * categories are stops of their own, so every category can be reached.
 */
export function channelStep<C extends LayoutChannel, K extends LayoutCategory<C>>(tree: ChannelTree<C, K>, channelId: string, dir: number) {
  const from = locateChannel(tree, channelId)
  if (!from || !dir) return null
  const list = containers(tree)
  const k = list.findIndex(c => c.id === from.categoryId)
  if (dir < 0) {
    if (from.index > 0) return { categoryId: from.categoryId, index: from.index - 1 }
    if (k === 0) return null
    const prev = list[k - 1]
    return prev ? { categoryId: prev.id, index: prev.channels.length } : null
  }
  const current = list[k]
  if (current && from.index < current.channels.length - 1) return { categoryId: from.categoryId, index: from.index + 1 }
  if (k === list.length - 1) return null
  const next = list[k + 1]
  return next ? { categoryId: next.id, index: 0 } : null
}

export function stepChannel<C extends LayoutChannel, K extends LayoutCategory<C>>(tree: ChannelTree<C, K>, channelId: string, dir: number): ChannelTree<C, K> {
  const step = channelStep(tree, channelId, dir)
  return step ? moveChannel(tree, channelId, step.categoryId, step.index) : tree
}

/** Categories swap with their neighbour and stop at either end. */
export function stepCategory<C extends LayoutChannel, K extends LayoutCategory<C>>(tree: ChannelTree<C, K>, categoryId: string, dir: number): ChannelTree<C, K> {
  const from = locateCategory(tree, categoryId)
  if (!from || !dir) return tree
  const index = from.index + Math.sign(dir)
  if (index < 0 || index >= from.total) return tree
  return moveCategory(tree, categoryId, index)
}

// ---- Adding and removing (admin dashboard keeps unsaved order across edits) ----

export function removeChannel<C extends LayoutChannel, K extends LayoutCategory<C>>(tree: ChannelTree<C, K>, channelId: string): ChannelTree<C, K> {
  const from = locateChannel(tree, channelId)
  if (!from) return tree
  return withChannels(tree, from.categoryId, (channelsIn(tree, from.categoryId) || []).filter(c => c.id !== channelId))
}

/** Its channels become uncategorized (ON DELETE SET NULL on the server). */
export function removeCategory<C extends LayoutChannel, K extends LayoutCategory<C>>(tree: ChannelTree<C, K>, categoryId: string): ChannelTree<C, K> {
  const from = locateCategory(tree, categoryId)
  if (!from) return tree
  const orphans = (from.category.channels || []).map(ch => ({ ...ch, category_id: null }))
  return {
    ...tree,
    uncategorized: [...(tree.uncategorized || []), ...orphans],
    categories: tree.categories.filter(c => c.id !== categoryId)
  }
}

/** Adds a category (once) at `index`, at the end by default. */
export function addCategory<C extends LayoutChannel, K extends LayoutCategory<C>>(tree: ChannelTree<C, K>, category: K | null | undefined, index = Infinity): ChannelTree<C, K> {
  if (!category?.id || locateCategory(tree, category.id)) return tree
  const list = [...(tree.categories || [])]
  list.splice(clamp(index, 0, list.length), 0, { ...category, channels: [...(category.channels || [])] })
  return { ...tree, categories: list }
}

/** sort_order for something appended after `list` (max + 1, gaps allowed). */
export function nextSortOrder(list: Array<{ sort_order?: number }> | null | undefined): number {
  return (list || []).reduce((max, item) => Math.max(max, (item.sort_order ?? -1) + 1), 0)
}

// ---- Payload of PUT /api/admin/layout ----

/**
 * { categories: [{ id, sort_order }], channels: [{ id, category_id, sort_order }] }
 * with dense sort orders: uncategorized channels first, then every category.
 */
export function toLayoutPayload<C extends LayoutChannel, K extends LayoutCategory<C>>(tree: ChannelTree<C, K>): LayoutRequest {
  return {
    categories: (tree.categories || []).map((c, i) => ({ id: c.id, sort_order: i })),
    channels: containers(tree).flatMap(({ id, channels }) =>
      channels.map((ch, i) => ({ id: ch.id, category_id: id, sort_order: i }))
    )
  }
}

export function samePayload(a: unknown, b: unknown): boolean {
  return JSON.stringify(a) === JSON.stringify(b)
}

export function sameLayout<C extends LayoutChannel, K extends LayoutCategory<C>>(a: ChannelTree<C, K>, b: ChannelTree<C, K>): boolean {
  return samePayload(toLayoutPayload(a), toLayoutPayload(b))
}

/**
 * Arranges a (fresh) tree in the order a payload describes. Used to keep a
 * pending reorder on screen while the server's data refreshes underneath:
 * names, unread state etc. come from `tree`, positions from `payload`.
 * Items the payload doesn't know (created meanwhile) follow the placed ones
 * in their current order; placements into unknown categories are ignored.
 */
export function applyLayoutPayload<C extends LayoutChannel, K extends LayoutCategory<C>>(tree: ChannelTree<C, K>, payload: LayoutRequest | null | undefined): ChannelTree<C, K> {
  if (!payload) return tree
  const catOrder = new Map((payload.categories || []).map(c => [c.id, c.sort_order]))
  const placement = new Map((payload.channels || []).map(c => [c.id, c]))
  const known = new Set((tree.categories || []).map(c => c.id))
  const rank = (order: number | null | undefined, i: number): [number, number] => (order == null ? [1, i] : [0, order])
  const byRank = (a: { rank: [number, number]; i: number }, b: { rank: [number, number]; i: number }) => a.rank[0] - b.rank[0] || a.rank[1] - b.rank[1] || a.i - b.i

  const buckets = new Map<string | null, Array<{ ch: C; rank: [number, number]; i: number }>>(containers(tree).map(c => [c.id, []]))
  let i = 0
  for (const { id, channels } of containers(tree)) {
    for (const ch of channels) {
      const found = placement.get(ch.id)
      const p = found && (found.category_id == null || known.has(found.category_id)) ? found : null
      const target = p ? p.category_id ?? null : id
      const moved = target !== id
      buckets.get(target)?.push({ ch: moved ? { ...ch, category_id: target } : ch, rank: rank(p?.sort_order, i), i: i++ })
    }
  }
  const sorted = (id: string | null): C[] => (buckets.get(id) || []).sort(byRank).map(e => e.ch)

  const categories = (tree.categories || [])
    .map((c, idx) => ({ c, rank: rank(catOrder.get(c.id), idx), i: idx }))
    .sort(byRank)
    .map(({ c }) => ({ ...c, channels: sorted(c.id) }))
  return { uncategorized: sorted(null), categories }
}

// ---- Drop rules for the sidebar ----
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

function lastKey<C extends LayoutChannel, K extends LayoutCategory<C>>(tree: ChannelTree<C, K>, categoryId: string | null) {
  const list = channelsIn(tree, categoryId)
  const last = list?.[list.length - 1]
  return last ? `channel:${last.id}` : null
}

// The indicator for "at the end of this container".
function endIndicator<C extends LayoutChannel, K extends LayoutCategory<C>>(tree: ChannelTree<C, K>, categoryId: string | null): DropIndicator | null {
  const last = lastKey(tree, categoryId)
  if (last) return { key: last, edge: 'bottom' }
  if (categoryId != null) return { key: `header:${categoryId}`, edge: 'bottom' }
  const first = tree.categories?.[0]
  return first ? { key: `header:${first.id}`, edge: 'top' } : null
}

/**
 * Where a dragged channel lands. opts.isCollapsed(categoryId) tells whether
 * a category's channels are hidden right now.
 */
export function resolveChannelDrop<C extends LayoutChannel, K extends LayoutCategory<C>>(tree: ChannelTree<C, K>, channelId: string, hit: DropHit | null, { isCollapsed = () => false }: { isCollapsed?: (id: string) => boolean } = {}): DropTarget | null {
  const from = locateChannel(tree, channelId)
  if (!from || !hit) return null
  const list = containers(tree)
  let categoryId: string | null = null
  let gap = 0
  let indicator: DropIndicator | null = null

  const intoCollapsed = (id: string) => {
    // Dropping a channel on the collapsed category it already sits in changes nothing.
    if (from.categoryId === id) return false
    categoryId = id
    gap = channelsIn(tree, id)?.length || 0
    indicator = { key: `header:${id}`, edge: 'inside' }
    return true
  }
  const atEnd = (id: string | null) => {
    if (id != null && isCollapsed(id)) return intoCollapsed(id)
    categoryId = id
    gap = channelsIn(tree, id)?.length || 0
    indicator = endIndicator(tree, id)
    return true
  }

  switch (hit.zone) {
    case 'channel': {
      const at = locateChannel(tree, hit.id)
      if (!at) return null
      const before = (hit.ratio ?? 0) < 0.5
      categoryId = at.categoryId
      gap = at.index + (before ? 0 : 1)
      indicator = { key: `channel:${hit.id}`, edge: before ? 'top' : 'bottom' }
      break
    }
    case 'header': {
      const k = list.findIndex(c => c.id === hit.id)
      if (k < 1) return null
      const prev = list[k - 1]
      if (!prev) return null
      // The container above is a place of its own only while its end is visible.
      const prevOpen = prev.id == null || !isCollapsed(prev.id)
      const ratio = hit.ratio ?? 0.5
      if (isCollapsed(hit.id)) {
        if (ratio < HEADER_EDGE && prevOpen) {
          atEnd(prev.id)
          indicator = { key: `header:${hit.id}`, edge: 'top' }
        } else if (!intoCollapsed(hit.id)) {
          return null
        }
      } else if (ratio < 0.5 && prevOpen) {
        atEnd(prev.id)
        indicator = { key: `header:${hit.id}`, edge: 'top' }
      } else {
        categoryId = hit.id
        gap = 0
        indicator = { key: `header:${hit.id}`, edge: 'bottom' }
      }
      break
    }
    case 'empty':
      if (!channelsIn(tree, hit.id)) return null
      categoryId = hit.id
      gap = 0
      indicator = { key: `header:${hit.id}`, edge: 'bottom' }
      break
    case 'end':
      if (!channelsIn(tree, hit.id ?? null)) return null
      if (!atEnd(hit.id ?? null)) return null
      break
    case 'start': {
      categoryId = null
      gap = 0
      const first = tree.uncategorized?.[0]
      indicator = first ? { key: `channel:${first.id}`, edge: 'top' } : endIndicator(tree, null)
      break
    }
    default:
      return null
  }

  const index = gapToIndex(from.index, from.categoryId === categoryId, gap)
  if (from.categoryId === categoryId && index === from.index) return null
  return { categoryId, gap, index, indicator }
}

/** Where a dragged category lands (it never goes into another one). */
export function resolveCategoryDrop<C extends LayoutChannel, K extends LayoutCategory<C>>(tree: ChannelTree<C, K>, categoryId: string, hit: DropHit | null): DropTarget | null {
  const from = locateCategory(tree, categoryId)
  if (!from || !hit) return null
  const cats = tree.categories
  let gap
  if (hit.zone === 'start' || (hit.zone === 'section' && hit.id == null)) {
    gap = 0
  } else if (hit.zone === 'end') {
    gap = cats.length
  } else if (hit.zone === 'section') {
    const at = locateCategory(tree, hit.id)
    if (!at) return null
    gap = at.index + ((hit.ratio ?? 0) < 0.5 ? 0 : 1)
  } else {
    return null
  }
  const index = gapToIndex(from.index, true, gap)
  if (index === from.index) return null
  const at = cats[gap]
  const last = cats[cats.length - 1]
  if (!last) return null
  const indicator: DropIndicator = at
    ? { key: `header:${at.id}`, edge: 'top' }
    : { key: `section:${last.id}`, edge: 'bottom' }
  return { categoryId: null, gap, index, indicator }
}
