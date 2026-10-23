// Search syntax: free text plus from: in: has: is: before: after: mentions: filters.
// Values with spaces are quoted (from:"Ada Lovelace"). This module is pure: it
// parses and serializes the query line and maps parsed filters to request
// parameters; resolving names to ids is done against the given members/channels.

export const HAS_KINDS = ['file', 'image', 'video', 'link', 'poll', 'edit'] as const
export type HasKind = typeof HAS_KINDS[number]

export const IS_KINDS = ['pinned'] as const
export type IsKind = typeof IS_KINDS[number]

export const FILTER_KEYS = ['from', 'in', 'has', 'is', 'before', 'after', 'mentions'] as const
export type FilterKey = typeof FILTER_KEYS[number]

export interface SearchFilters {
  from?: string
  in?: string
  has?: HasKind
  is?: IsKind
  before?: string // YYYY-MM-DD
  after?: string // YYYY-MM-DD
  mentions?: string
}

export interface ParsedSearch {
  text: string
  filters: SearchFilters
  /** Raw tokens of a known filter key whose value is not valid (yet), e.g. has:foo. */
  invalid: string[]
}

interface Token { raw: string; key: string; value: string; quoted: boolean }

const DATE = /^(\d{4})-(\d{2})-(\d{2})$/

function isFilterKey(key: string): key is FilterKey {
  return (FILTER_KEYS as readonly string[]).includes(key)
}

function isHasKind(v: string): v is HasKind {
  return (HAS_KINDS as readonly string[]).includes(v)
}

function isIsKind(v: string): v is IsKind {
  return (IS_KINDS as readonly string[]).includes(v)
}

/** The keys whose values are a fixed word, written in lower case. */
function isWordKey(key: string): boolean {
  return key === 'has' || key === 'is'
}

/** True for a real calendar date written as YYYY-MM-DD. */
export function isSearchDate(v: string): boolean {
  const m = DATE.exec(v)
  if (!m) return false
  const [y, mo, d] = [Number(m[1]), Number(m[2]), Number(m[3])]
  const date = new Date(y, mo - 1, d)
  return date.getFullYear() === y && date.getMonth() === mo - 1 && date.getDate() === d
}

function readQuoted(input: string, start: number): { value: string; end: number } {
  let i = start + 1
  let value = ''
  while (i < input.length && input[i] !== '"') {
    if (input[i] === '\\' && i + 1 < input.length) i++
    value += input[i]
    i++
  }
  return { value, end: i < input.length ? i + 1 : i }
}

function readWord(input: string, start: number): { value: string; end: number } {
  let i = start
  while (i < input.length && !/\s/.test(input[i] ?? '')) i++
  return { value: input.slice(start, i), end: i }
}

function readToken(input: string, start: number): { token: Token; end: number } {
  const colon = /^([A-Za-z]+):/.exec(input.slice(start))
  const key = colon?.[1]?.toLowerCase() ?? ''
  if (colon && isFilterKey(key)) {
    const valueStart = start + colon[0].length
    const quoted = input[valueStart] === '"'
    const part = quoted ? readQuoted(input, valueStart) : readWord(input, valueStart)
    return { token: { raw: input.slice(start, part.end), key, value: part.value, quoted }, end: part.end }
  }
  if (input[start] === '"') {
    const part = readQuoted(input, start)
    return { token: { raw: input.slice(start, part.end), key: '', value: part.value, quoted: true }, end: part.end }
  }
  const part = readWord(input, start)
  return { token: { raw: part.value, key: '', value: part.value, quoted: false }, end: part.end }
}

function tokenize(input: string): Token[] {
  const tokens: Token[] = []
  let i = 0
  while (i < input.length) {
    if (/\s/.test(input[i] ?? '')) { i++; continue }
    const { token, end } = readToken(input, i)
    tokens.push(token)
    i = end
  }
  return tokens
}

function validFilterValue(key: FilterKey, value: string): boolean {
  if (!value) return false
  if (key === 'has') return isHasKind(value)
  if (key === 'is') return isIsKind(value)
  if (key === 'before' || key === 'after') return isSearchDate(value)
  return true
}

function setFilter(filters: SearchFilters, key: FilterKey, value: string) {
  if (key === 'has') { if (isHasKind(value)) filters.has = value } else if (key === 'is') { if (isIsKind(value)) filters.is = value } else filters[key] = value
}

/** Parses a query line into free text and filters; later filters of a kind win. */
export function parseSearchQuery(input: string): ParsedSearch {
  const filters: SearchFilters = {}
  const words: string[] = []
  const invalid: string[] = []
  for (const t of tokenize(input)) {
    if (!t.key) { words.push(t.value); continue }
    const key = t.key as FilterKey
    const value = isWordKey(key) ? t.value.toLowerCase() : t.value
    if (!validFilterValue(key, value)) { invalid.push(t.raw); continue }
    setFilter(filters, key, value)
  }
  return { text: words.join(' '), filters, invalid }
}

export interface SplitQuery { filters: SearchFilters; rest: string }

/**
 * Separates the filters that are finished (valid and followed by whitespace or
 * another token) from the text still being typed, which keeps its raw form.
 */
export function splitCommitted(input: string): SplitQuery {
  const tokens = tokenize(input)
  const endsOpen = !/\s$/.test(input)
  const filters: SearchFilters = {}
  const rest: string[] = []
  tokens.forEach((t, i) => {
    const key = t.key as FilterKey
    const value = isWordKey(key) ? t.value.toLowerCase() : t.value
    const done = i < tokens.length - 1 || !endsOpen
    if (t.key && done && validFilterValue(key, value)) setFilter(filters, key, value)
    else rest.push(t.raw)
  })
  const joined = rest.join(' ')
  return { filters, rest: joined && !endsOpen ? joined + ' ' : joined }
}

/** Quotes a filter value when it contains whitespace or quotes. */
export function quoteValue(value: string): string {
  if (!/[\s"\\]/.test(value)) return value
  return `"${value.replace(/["\\]/g, '\\$&')}"`
}

/** Writes filters followed by the free text back into one query line. */
export function serializeSearchQuery(p: Pick<ParsedSearch, 'text' | 'filters'>): string {
  const parts: string[] = []
  for (const key of FILTER_KEYS) {
    const v = p.filters[key]
    if (v) parts.push(`${key}:${quoteValue(v)}`)
  }
  if (p.text) parts.push(p.text)
  return parts.join(' ')
}

export interface SearchLookup {
  members: { id: string; username: string; display_name?: string }[]
  channels: { id: string; name: string }[]
}

export interface ResolvedSearch {
  params: URLSearchParams
  /** Filters whose name matched no member or channel. */
  unresolved: FilterKey[]
}

function norm(s: string): string {
  return s.trim().replace(/^[@#]/, '').toLowerCase()
}

function findMember(lookup: SearchLookup, name: string): string | undefined {
  const n = norm(name)
  const byUser = lookup.members.find(m => m.username.toLowerCase() === n)
  if (byUser) return byUser.id
  return lookup.members.find(m => (m.display_name ?? '').toLowerCase() === n)?.id
}

function findChannel(lookup: SearchLookup, name: string): string | undefined {
  const n = norm(name)
  return lookup.channels.find(c => c.name.toLowerCase() === n)?.id
}

/** Local start of the given day as an RFC 3339 instant (UTC). */
export function dayStart(date: string, offsetDays = 0): string {
  const m = DATE.exec(date)
  if (!m) return ''
  return new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]) + offsetDays).toISOString()
}

const CRITERIA = ['q', 'channel_id', 'author_id', 'mentions', 'has', 'is', 'after', 'before_date']

/** Whether the parameters hold at least one criterion the server accepts alone. */
export function isSearchable(params: URLSearchParams): boolean {
  return CRITERIA.some(k => params.has(k))
}

/**
 * Maps a parsed query to request parameters. after: includes that day and
 * before: ends with it. A filter whose name cannot be resolved is left out
 * and reported, so the list never silently widens to "everything".
 */
export function buildSearchParams(p: ParsedSearch, lookup: SearchLookup, extra: Record<string, string> = {}): ResolvedSearch {
  const params = new URLSearchParams(extra)
  const unresolved: FilterKey[] = []
  if (p.text) params.set('q', p.text)
  const { filters } = p
  const resolve = (key: 'from' | 'mentions', param: string) => {
    const name = filters[key]
    if (!name) return
    const id = findMember(lookup, name)
    if (id) params.set(param, id); else unresolved.push(key)
  }
  resolve('from', 'author_id')
  resolve('mentions', 'mentions')
  if (filters.in) {
    const id = findChannel(lookup, filters.in)
    if (id) params.set('channel_id', id); else unresolved.push('in')
  }
  if (filters.has) params.set('has', filters.has)
  if (filters.is) params.set('is', filters.is)
  if (filters.after) params.set('after', dayStart(filters.after))
  if (filters.before) params.set('before_date', dayStart(filters.before, 1))
  return { params, unresolved }
}
