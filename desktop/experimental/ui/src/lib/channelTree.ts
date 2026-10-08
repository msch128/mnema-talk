// Sidebar structure: uncategorized channels first, then every
// category in sort order with all of its channels (text and voice mixed).

import type { Channel } from '../types/domain'
export interface LayoutChannel { id: string; category_id?: string | null; sort_order?: number }
export interface LayoutCategory<C extends LayoutChannel = Channel> { id: string; sort_order?: number; channels?: C[] }
export interface ChannelTree<C extends LayoutChannel = Channel, K extends LayoutCategory<C> = LayoutCategory<C>> { uncategorized: C[]; categories: K[] }

const bySortOrder = <T extends { sort_order?: number }>(list: T[] | null | undefined): T[] =>
  (list || [])
    .map((item, index) => ({ item, index }))
    .sort((a, b) => (a.item.sort_order ?? 0) - (b.item.sort_order ?? 0) || a.index - b.index)
    .map(({ item }) => item)

export function buildChannelTree<C extends LayoutChannel, K extends LayoutCategory<C>>(categories: K[] | null | undefined, uncategorized: C[] | null | undefined): ChannelTree<C, K> {
  return {
    uncategorized: bySortOrder(uncategorized),
    categories: bySortOrder(categories).map(cat => ({ ...cat, channels: bySortOrder(cat.channels) }))
  }
}

export const COLLAPSED_KEY = 'mnema.sidebar.collapsedCategories'

export function loadCollapsed(storage: Pick<Storage, 'getItem'> | null = globalThis.localStorage): string[] {
  try {
    const ids: unknown = JSON.parse(storage?.getItem(COLLAPSED_KEY) || '[]')
    return Array.isArray(ids) ? ids.filter((id: unknown): id is string => typeof id === 'string') : []
  } catch {
    return []
  }
}

export function saveCollapsed(ids: string[], storage: Pick<Storage, 'setItem'> | null = globalThis.localStorage): void {
  try {
    storage?.setItem(COLLAPSED_KEY, JSON.stringify(ids))
  } catch {
    // Storage unavailable (private mode, quota): collapse state is per session then.
  }
}
