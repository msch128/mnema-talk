// Mention autocomplete in the composer: find the "@que" being typed at the
// caret, rank matching members, and splice the chosen name in.

import type { User } from '../types/domain'
export type MentionSuggestion =
  | { username: 'here' | 'all'; display_name: string; group: true; user?: never }
  | { username: string; display_name: string; user: User; group?: false }
const NAME_CHARS = /[A-Za-z0-9_.-]/

/**
 * The mention being typed right before the caret: { start, query } where
 * start is the index of '@', or null. A mention starts at the beginning of
 * the text or after a character that cannot be part of a username.
 */
export function findMentionQuery(text: string | null | undefined, caret: number | null | undefined) {
  if (typeof text !== 'string' || caret == null) return null
  let i = caret - 1
  while (i >= 0 && NAME_CHARS.test(text[i] || '')) i--
  if (i < 0 || text[i] !== '@') return null
  if (i > 0 && (NAME_CHARS.test(text[i - 1] || '') || text[i - 1] === '@')) return null
  const query = text.slice(i + 1, caret)
  if (query.length > 32) return null
  return { start: i, query }
}

/** The keyword mentions, offered after the members. */
export const GROUP_MENTIONS = ['here', 'all'] as const

/**
 * Suggestions for query: members whose username or display name starts with
 * it (then those that contain it), then @here / @all. Each item is
 * { username, display_name, user?, group? }.
 */
export function suggestMentions(query: string, members: Array<User & { disabled?: boolean }> | null | undefined, { selfId = null, limit = 8 }: { selfId?: string | null; limit?: number } = {}): MentionSuggestion[] {
  const q = (query || '').toLowerCase()
  const scored: Array<{ score: number; m: User }> = []
  for (const m of members || []) {
    if (!m?.username || m.id === selfId || m.disabled) continue
    const u = m.username.toLowerCase()
    const d = (m.display_name || '').toLowerCase()
    let score = -1
    if (u.startsWith(q) || d.startsWith(q)) score = 0
    else if (d.split(/\s+/).some(w => w.startsWith(q))) score = 1
    else if (q && (u.includes(q) || d.includes(q))) score = 2
    if (score >= 0) scored.push({ score, m })
  }
  scored.sort((a, b) => a.score - b.score || (a.m.display_name || a.m.username).localeCompare(b.m.display_name || b.m.username))
  const items: MentionSuggestion[] = scored.slice(0, limit).map(({ m }) => ({ username: m.username, display_name: m.display_name || m.username, user: m }))
  for (const g of GROUP_MENTIONS) {
    if (g.startsWith(q) && items.length < limit + GROUP_MENTIONS.length) items.push({ username: g, display_name: g, group: true })
  }
  return items
}

/** Replaces the mention at start..caret with "@username " and returns { text, caret }. */
export function applyMention(text: string, start: number, caret: number, username: string) {
  const before = text.slice(0, start)
  let after = text.slice(caret)
  // Swallow the rest of a half-typed name after the caret.
  const rest = after.match(/^[A-Za-z0-9_.-]*/)?.[0] || ''
  after = after.slice(rest.length)
  const insert = `@${username}` + (after.startsWith(' ') ? '' : ' ')
  return { text: before + insert + after, caret: before.length + insert.length }
}
