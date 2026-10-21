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
type ChannelContainer<C> = { id: string | null; channels: C[] }

function stepUp<C>(from: { categoryId: string | null; index: number }, list: ChannelContainer<C>[], k: number) {
  if (from.index > 0) return { categoryId: from.categoryId, index: from.index - 1 }
  if (k <= 0) return null
  const prev = list[k - 1]
  return prev ? { categoryId: prev.id, index: prev.channels.length } : null
}

function stepDown<C>(from: { categoryId: string | null; index: number }, list: ChannelContainer<C>[], k: number) {
  const current = list[k]
  if (current && from.index < current.channels.length - 1) {
    return { categoryId: from.categoryId, index: from.index + 1 }
  }
  if (k >= list.length - 1) return null
  const next = list[k + 1]
  return next ? { categoryId: next.id, index: 0 } : null
}

export function channelStep<C extends LayoutChannel, K extends LayoutCategory<C>>(tree: ChannelTree<C, K>, channelId: string, dir: number) {
  const from = locateChannel(tree, channelId)
  if (!from || !dir) return null
  const list = containers(tree)
  const k = list.findIndex(c => c.id === from.categoryId)
  return dir < 0 ? stepUp(from, list, k) : stepDown(from, list, k)
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

type RankedItem<T> = { item: T; rank: [number, number]; seq: number }

function compareRanked<T>(a: RankedItem<T>, b: RankedItem<T>): number {
  if (a.rank[0] !== b.rank[0]) return a.rank[0] - b.rank[0]
  if (a.rank[1] !== b.rank[1]) return a.rank[1] - b.rank[1]
  return a.seq - b.seq
}

function itemRank(order: number | null | undefined, seq: number): [number, number] {
  return order == null ? [1, seq] : [0, order]
}

function resolvePlacementTarget(
  placement: Map<string, { category_id: string | null; sort_order: number }>,
  known: Set<string>,
  channelId: string,
  currentContainerId: string | null
): { target: string | null; sortOrder?: number } {
  const p = placement.get(channelId)
  if (!p) return { target: currentContainerId }
  if (p.category_id != null && !known.has(p.category_id)) {
    return { target: currentContainerId }
  }
  return { target: p.category_id ?? null, sortOrder: p.sort_order }
}

function collectPlacedChannels<C extends LayoutChannel, K extends LayoutCategory<C>>(
  tree: ChannelTree<C, K>,
  placement: Map<string, { category_id: string | null; sort_order: number }>
): Map<string | null, C[]> {
  const known = new Set((tree.categories || []).map(c => c.id))
  const buckets = new Map<string | null, Array<RankedItem<C>>>(containers(tree).map(c => [c.id, []]))
  let seq = 0

  for (const { id, channels } of containers(tree)) {
    for (const ch of channels) {
      const { target, sortOrder } = resolvePlacementTarget(placement, known, ch.id, id)
      const moved = target !== id
      const item = moved ? { ...ch, category_id: target } : ch
      buckets.get(target)?.push({ item, rank: itemRank(sortOrder, seq), seq: seq++ })
    }
  }

  const result = new Map<string | null, C[]>()
  for (const [id, items] of buckets.entries()) {
    result.set(id, items.sort(compareRanked).map(entry => entry.item))
  }
  return result
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
  const placed = collectPlacedChannels(tree, placement)

  const categories = (tree.categories || [])
    .map((c, idx) => ({ item: c, rank: itemRank(catOrder.get(c.id), idx), seq: idx }))
    .sort(compareRanked)
    .map(({ item }) => ({ ...item, channels: placed.get(item.id) || [] }))

  return { uncategorized: placed.get(null) || [], categories }
}
