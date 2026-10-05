// Sidebar structure: uncategorized channels first, then every
// category in sort order with all of its channels (text and voice mixed).

const bySortOrder = list =>
  (list || [])
    .map((item, index) => ({ item, index }))
    .sort((a, b) => (a.item.sort_order ?? 0) - (b.item.sort_order ?? 0) || a.index - b.index)
    .map(({ item }) => item)

export function buildChannelTree(categories, uncategorized) {
  return {
    uncategorized: bySortOrder(uncategorized),
    categories: bySortOrder(categories).map(cat => ({ ...cat, channels: bySortOrder(cat.channels) }))
  }
}

export const COLLAPSED_KEY = 'mnema.sidebar.collapsedCategories'

export function loadCollapsed(storage = globalThis.localStorage) {
  try {
    const ids = JSON.parse(storage?.getItem(COLLAPSED_KEY) || '[]')
    return Array.isArray(ids) ? ids.filter(id => typeof id === 'string') : []
  } catch {
    return []
  }
}

export function saveCollapsed(ids, storage = globalThis.localStorage) {
  try {
    storage?.setItem(COLLAPSED_KEY, JSON.stringify(ids))
  } catch {
    // Storage unavailable (private mode, quota): collapse state is per session then.
  }
}
