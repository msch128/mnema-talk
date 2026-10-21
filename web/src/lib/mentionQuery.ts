// Mention autocomplete in the composer: find the "@que" being typed at the
// caret, rank matching members, and splice the chosen name in.

import type { User } from '../types/domain'

type Mentionable = User & { is_bot?: boolean }
export type MentionSuggestion =
  | { username: 'here' | 'all'; display_name: string; group: true; user?: never }
  | { username: string; display_name: string; user: Mentionable; group?: false }
const NAME_CHARS = /[A-Za-z0-9_.-]/

function findAtPosition(text: string, caret: number): number {
  let i = caret - 1
  while (i >= 0 && NAME_CHARS.test(text[i] || '')) i--
  return i
}

function isValidMentionBoundary(text: string, atIndex: number): boolean {
  if (atIndex <= 0) return true
  const prev = text[atIndex - 1] || ''
  return !NAME_CHARS.test(prev) && prev !== '@'
}

/**
 * The mention being typed right before the caret: { start, query } where
 * start is the index of '@', or null. A mention starts at the beginning of
 * the text or after a character that cannot be part of a username.
 */
export function findMentionQuery(text: string | null | undefined, caret: number | null | undefined) {
  if (typeof text !== 'string' || caret == null) return null
  const i = findAtPosition(text, caret)
  if (i < 0 || text[i] !== '@') return null
  if (!isValidMentionBoundary(text, i)) return null
  const query = text.slice(i + 1, caret)
  if (query.length > 32) return null
  return { start: i, query }
}

/** The keyword mentions, offered after the members. */
export const GROUP_MENTIONS = ['here', 'all'] as const

function isEligibleMember(m: User & { disabled?: boolean }, selfId: string | null): boolean {
  if (!m?.username || m.id === selfId) return false
  return !m.disabled
}

function matchesPrefix(u: string, d: string, q: string): boolean {
  return u.startsWith(q) || d.startsWith(q)
}

function matchesWordPrefix(d: string, q: string): boolean {
  return d.split(/\s+/).some(w => w.startsWith(q))
}

function matchesSubstring(u: string, d: string, q: string): boolean {
  return !!q && (u.includes(q) || d.includes(q))
}

function scoreMember(m: User & { disabled?: boolean }, q: string, selfId: string | null): number {
  if (!isEligibleMember(m, selfId)) return -1
  const u = m.username.toLowerCase()
  const d = (m.display_name || '').toLowerCase()
  if (matchesPrefix(u, d, q)) return 0
  if (matchesWordPrefix(d, q)) return 1
  if (matchesSubstring(u, d, q)) return 2
  return -1
}

function compareScoredMembers(a: { score: number; m: User }, b: { score: number; m: User }): number {
  if (a.score !== b.score) return a.score - b.score
  const nameA = a.m.display_name || a.m.username
  const nameB = b.m.display_name || b.m.username
  return nameA.localeCompare(nameB)
}

function matchGroupMentions(q: string, items: MentionSuggestion[], limit: number) {
  const max = limit + GROUP_MENTIONS.length
  for (const g of GROUP_MENTIONS) {
    if (g.startsWith(q) && items.length < max) {
      items.push({ username: g, display_name: g, group: true })
    }
  }
}

/**
 * Suggestions for query: members whose username or display name starts with
 * it (then those that contain it), then @here / @all. Each item is
 * { username, display_name, user?, group? }.
 */
export function suggestMentions(
  query: string,
  members: Array<User & { disabled?: boolean }> | null | undefined,
  { selfId = null, limit = 8 }: { selfId?: string | null; limit?: number } = {}
): MentionSuggestion[] {
  const q = (query || '').toLowerCase()
  const scored: Array<{ score: number; m: User }> = []
  for (const m of members || []) {
    const score = scoreMember(m, q, selfId)
    if (score >= 0) scored.push({ score, m })
  }
  scored.sort(compareScoredMembers)
  const items: MentionSuggestion[] = scored
    .slice(0, limit)
    .map(({ m }) => ({ username: m.username, display_name: m.display_name || m.username, user: m }))
  matchGroupMentions(q, items, limit)
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
