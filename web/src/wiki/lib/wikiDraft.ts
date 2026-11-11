// An unsaved wiki page kept in this tab's session storage, so a reload does not
// lose what was written. It lives only as long as the tab, is never sent to the
// server, and goes when the page is saved, the text is discarded or the member
// signs out. Every storage access tolerates blocked or full storage.
import { isRecord } from '../../types/validation'

export interface WikiDraft {
  title: string
  content: string
  summary: string
  slug: string
  parent: string
  /** The revision the text was started from, so a save after a reload still notices what changed meanwhile. */
  baseRev: number
}

const PREFIX = 'mnema.wiki.draft.'

/** One draft per account and page; a page that does not exist yet has the address `new`. */
export function wikiDraftKey(user: string, slug: string | null): string {
  return `${PREFIX}${user}.${slug === null ? 'new' : `page:${slug}`}`
}

function decode(raw: unknown): WikiDraft | null {
  if (!isRecord(raw)) return null
  const { title, content, summary, slug, parent, baseRev } = raw
  if (typeof title !== 'string' || typeof content !== 'string' || typeof summary !== 'string' || typeof slug !== 'string' || typeof parent !== 'string') return null
  if (typeof baseRev !== 'number' || !Number.isInteger(baseRev) || baseRev < 0) return null
  return { title, content, summary, slug, parent, baseRev }
}

export function loadWikiDraft(key: string): WikiDraft | null {
  try {
    const raw = sessionStorage.getItem(key)
    return raw ? decode(JSON.parse(raw)) : null
  } catch {
    return null
  }
}

export function saveWikiDraft(key: string, draft: WikiDraft): void {
  try { sessionStorage.setItem(key, JSON.stringify(draft)) } catch { /* storage is blocked or full: the draft is simply not kept */ }
}

export function clearWikiDraft(key: string): void {
  try { sessionStorage.removeItem(key) } catch { /* storage is blocked */ }
}

/** Removes every wiki draft of this tab (sign-out). */
export function clearAllWikiDrafts(): void {
  try {
    for (const key of Object.keys(sessionStorage)) if (key.startsWith(PREFIX)) sessionStorage.removeItem(key)
  } catch { /* storage is blocked */ }
}
