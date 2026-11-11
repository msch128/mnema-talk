// The addresses of the wiki dialog. Words the wiki uses itself (search, new,
// deleted, history, edit) are reserved on the server, so a page's address never
// reads as one of them.

export type WikiLocation =
  | { mode: 'home' }
  | { mode: 'search' | 'new' | 'deleted' }
  | { mode: 'page' | 'history' | 'edit'; slug: string }

const SECTIONS = new Set(['search', 'new', 'deleted'])
const PAGE_MODES = new Set(['history', 'edit'])

/** Reads the parts of an address after /wiki. */
export function parseWikiLocation(parts: readonly string[]): WikiLocation {
  const first = parts[0]
  if (!first) return { mode: 'home' }
  if (SECTIONS.has(first)) return { mode: first as 'search' | 'new' | 'deleted' }
  const second = parts[1]
  return { mode: second && PAGE_MODES.has(second) ? second as 'history' | 'edit' : 'page', slug: first }
}

/** The address of a location. */
export function wikiPath(at: WikiLocation): string {
  if (at.mode === 'home') return '/wiki'
  if ('slug' in at) {
    const base = `/wiki/${encodeURIComponent(at.slug)}`
    return at.mode === 'page' ? base : `${base}/${at.mode}`
  }
  return `/wiki/${at.mode}`
}
