import { computed } from 'vue'
import { currentRoute } from '../shared/lib/router'
import type { WikiLocation } from '../lib/wikiPaths'

/** Where in the wiki the address is (the start page when the address is not the wiki's). */
export function useWikiLocation() {
  const location = computed<WikiLocation>(() => (currentRoute.value.view === 'wiki' ? currentRoute.value.wiki : { mode: 'home' }))
  const slug = computed(() => ('slug' in location.value ? location.value.slug : ''))
  return { location, slug }
}
