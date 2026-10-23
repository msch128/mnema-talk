// Autocomplete for the filter token being typed at the end of the search line.

import type { FilterKey, SearchLookup } from './searchQuery'
import { HAS_KINDS, IS_KINDS, quoteValue } from './searchQuery'
import { NEW_HAS_KINDS, filtersUnsupported } from './searchSupport'

export interface SearchSuggestion {
  /** Value written after "key:". */
  value: string
  /** Text shown in the list. */
  label: string
  /** Secondary text (e.g. the username of a member shown by display name). */
  detail?: string | undefined
}

export interface SuggestionContext {
  key: FilterKey
  /** Index in the input where the "key:" token starts. */
  start: number
  items: SearchSuggestion[]
}

const OPEN = /(^|\s)(from|in|has|is|mentions):("([^"]*)|([^\s"]*))$/i
const MAX = 6

function fold(s: string): string {
  return s.toLowerCase().replace(/^[@#]/, '')
}

function memberItems(lookup: SearchLookup, partial: string): SearchSuggestion[] {
  const p = fold(partial)
  const out: SearchSuggestion[] = []
  for (const m of lookup.members) {
    const display = m.display_name || ''
    if (!p || m.username.toLowerCase().includes(p) || display.toLowerCase().includes(p)) {
      out.push({ value: m.username, label: display || m.username, detail: display ? `@${m.username}` : undefined })
    }
    if (out.length === MAX) break
  }
  return out
}

function channelItems(lookup: SearchLookup, partial: string): SearchSuggestion[] {
  const p = fold(partial)
  return lookup.channels
    .filter(c => !p || c.name.toLowerCase().includes(p))
    .slice(0, MAX)
    .map(c => ({ value: c.name, label: `#${c.name}` }))
}

function hasItems(partial: string): SearchSuggestion[] {
  const p = partial.toLowerCase()
  return HAS_KINDS.filter(k => k.startsWith(p) && !(filtersUnsupported.value && NEW_HAS_KINDS.includes(k))).map(k => ({ value: k, label: k }))
}

function isItems(partial: string): SearchSuggestion[] {
  if (filtersUnsupported.value) return []
  const p = partial.toLowerCase()
  return IS_KINDS.filter(k => k.startsWith(p)).map(k => ({ value: k, label: k }))
}

/** Suggestions for the filter value under the end of the input, or null. */
export function suggestFor(input: string, lookup: SearchLookup): SuggestionContext | null {
  const m = OPEN.exec(input)
  if (!m) return null
  const key = (m[2] ?? '').toLowerCase() as FilterKey
  const partial = m[4] ?? m[5] ?? ''
  const start = (m.index ?? 0) + (m[1]?.length ?? 0)
  let items: SearchSuggestion[]
  if (key === 'has') items = hasItems(partial)
  else if (key === 'is') items = isItems(partial)
  else if (key === 'in') items = channelItems(lookup, partial)
  else items = memberItems(lookup, partial)
  // A fully typed value that is the only match needs no list.
  if (items.length === 1 && items[0]?.value.toLowerCase() === partial.toLowerCase()) return null
  return items.length ? { key, start, items } : null
}

/** Replaces the token under the caret with the chosen suggestion. */
export function applySuggestion(input: string, ctx: SuggestionContext, item: SearchSuggestion): string {
  return `${input.slice(0, ctx.start)}${ctx.key}:${quoteValue(item.value)} `
}
