// Custom emoji autocomplete in the composer: find the ":que" being typed at
// the caret, rank the matching emoji and splice the chosen :name: in.
import type { CustomEmoji } from './customEmoji'

const NAME_CHAR = /[a-z0-9_]/i
/** Typing starts after this many characters, so ":)" or a time never opens the list. */
const MIN_QUERY = 2

/**
 * The ":name" being typed right before the caret: { start, query } where start
 * is the index of ':', or null. It must start the text or follow white space.
 */
export function findEmojiQuery(text: string | null | undefined, caret: number | null | undefined): { start: number; query: string } | null {
  if (typeof text !== 'string' || caret == null) return null
  let i = caret - 1
  while (i >= 0 && NAME_CHAR.test(text[i] ?? '')) i--
  if (i < 0 || text[i] !== ':') return null
  if (i > 0 && !/\s/.test(text[i - 1] ?? '')) return null
  const query = text.slice(i + 1, caret)
  return query.length >= MIN_QUERY && query.length <= 32 ? { start: i, query } : null
}

/** Emoji whose name starts with the query, then those that contain it. */
export function suggestEmoji(query: string, emoji: readonly CustomEmoji[], limit = 8): CustomEmoji[] {
  const q = query.toLowerCase()
  const starts = emoji.filter(e => e.name.startsWith(q))
  const contains = emoji.filter(e => !e.name.startsWith(q) && e.name.includes(q))
  return [...starts, ...contains].slice(0, limit)
}

/** Replaces the ":que" at start..caret with ":name: " and returns { text, caret }. */
export function applyEmoji(text: string, start: number, caret: number, name: string) {
  const before = text.slice(0, start)
  let after = text.slice(caret)
  after = after.slice(after.match(/^[a-z0-9_]*/i)?.[0].length ?? 0)
  const insert = `:${name}:` + (after.startsWith(' ') ? '' : ' ')
  return { text: before + insert + after, caret: before.length + insert.length }
}
