import { ref } from 'vue'
import type { SearchFilters } from './searchQuery'

/**
 * Search filters added in 1.1 (has:poll, has:edit, is:pinned). A 1.0.x server
 * answers them with 400, so the first such answer marks them unsupported: the
 * suggestions disappear and the results explain it instead of failing.
 * markFiltersSupported is for a server feature list that names the filters
 * (`search.filters.v2`).
 */
export const NEW_HAS_KINDS: readonly string[] = ['poll', 'edit', 'embed']

export const filtersUnsupported = ref(false)

export function usesNewFilters(filters: SearchFilters): boolean {
  return filters.is !== undefined || (filters.has !== undefined && NEW_HAS_KINDS.includes(filters.has))
}

/** Called when a search with a new filter was refused as invalid. */
export function markFiltersUnsupported() {
  filtersUnsupported.value = true
}

export function markFiltersSupported() {
  filtersUnsupported.value = false
}
