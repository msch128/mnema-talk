// Search syntax: free text plus from: in: has: is: before: after: mentions: filters.
// Values with spaces are quoted (from:"Ada Lovelace"). This module is pure: it
// parses and serializes the query line and maps parsed filters to request
// parameters; resolving names to ids is done against the given members/channels.

export const HAS_KINDS = ['file', 'image', 'video', 'link', 'embed', 'poll', 'edit'] as const
export type HasKind = typeof HAS_KINDS[number]

export const IS_KINDS = ['pinned'] as const
export type IsKind = typeof IS_KINDS[number]

export const AUTHOR_KINDS = ['bot', 'webhook', 'member'] as const
export type AuthorKind = typeof AUTHOR_KINDS[number]

export const FILTER_KEYS = ['from', 'in', 'has', 'is', 'before', 'after', 'during', 'mentions', 'author', 'filename'] as const
export type FilterKey = typeof FILTER_KEYS[number]

export interface SearchFilters {
  from?: string
  in?: string
  has?: HasKind
  is?: IsKind
  before?: string // YYYY-MM-DD
  after?: string // YYYY-MM-DD
  during?: string // YYYY-MM-DD
  mentions?: string
  author?: AuthorKind
  filename?: string
}

export interface ParsedSearch {
  text: string
  filters: SearchFilters
  /** Raw tokens of a known filter key whose value is not valid (yet), e.g. has:foo. */
  invalid: string[]
}

interface Token { raw: string; key: string; value: string; quoted: boolean; exclude?: boolean }

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

function isAuthorKind(v: string): v is AuthorKind {
  return (AUTHOR_KINDS as readonly string[]).includes(v)
}

/** The keys whose values are a fixed word, written in lower case. */
function isWordKey(key: string): boolean {
  return key === 'has' || key === 'is' || key === 'author'
}

/** Local calendar day, offsetDays from today, as YYYY-MM-DD. */
export function isoDay(offsetDays = 0): string {
  const d = new Date()
  d.setDate(d.getDate() + offsetDays)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

const DAY_WORDS: Record<string, number> = { today: 0, heute: 0, yesterday: -1, gestern: -1 }

function isDateKey(key: string): boolean {
  return key === 'before' || key === 'after' || key === 'during'
}

/** The value as written after "key:", with day words and word values normalized. */
function readValue(key: FilterKey, value: string): string {
  if (isWordKey(key)) return value.toLowerCase()
  const offset = isDateKey(key) ? DAY_WORDS[value.toLowerCase()] : undefined
  return offset === undefined ? value : isoDay(offset)
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
  const exclude = input[start] === '-' && input[start + 1] === '"'
  if (input[start] === '"' || exclude) {
    const part = readQuoted(input, exclude ? start + 1 : start)
    return { token: { raw: input.slice(start, part.end), key: '', value: part.value, quoted: true, exclude }, end: part.end }
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
  if (key === 'author') return isAuthorKind(value)
  if (key === 'before' || key === 'after' || key === 'during') return isSearchDate(value)
  return true
}

function setFilter(filters: SearchFilters, key: FilterKey, value: string) {
  if (key === 'has') {
    if (isHasKind(value)) filters.has = value
  } else if (key === 'is') {
    if (isIsKind(value)) filters.is = value
  } else if (key === 'author') {
    if (isAuthorKind(value)) filters.author = value
  } else {
    filters[key] = value
  }
}

/**
 * A free-text token as the server reads it: a "quoted phrase" is one term and
 * -word (or -"phrase") excludes. Quotes are kept only where they carry meaning.
 */
function plainTerm(t: Token): string {
  if (!t.quoted) return t.value
  const inner = t.value.replace(/"/g, '')
  if (t.exclude) return `-"${inner}"`
  return /\s/.test(inner) || inner.startsWith('-') ? `"${inner}"` : inner
}

/** Terms of the free text worth highlighting in a hit: words and phrases, never excluded ones. */
export function textTerms(text: string): string[] {
  const out: string[] = []
  for (const m of text.matchAll(/(-?)(?:"([^"]*)"?|(\S+))/g)) {
    if (m[1] && (m[3] || m[2] !== undefined)) continue
    const term = (m[2] ?? m[3] ?? '').trim()
    if (term) out.push(term)
  }
  return out
}

/** Parses a query line into free text and filters; later filters of a kind win. */
export function parseSearchQuery(input: string): ParsedSearch {
  const filters: SearchFilters = {}
  const words: string[] = []
  const invalid: string[] = []
  for (const t of tokenize(input)) {
    if (!t.key) { words.push(plainTerm(t)); continue }
    const key = t.key as FilterKey
    const value = readValue(key, t.value)
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
    const value = readValue(key, t.value)
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

/** Local timezone offset as [+-]HH:MM. */
export function localTimezoneOffset(): string {
  const offsetMin = -new Date().getTimezoneOffset()
  const sign = offsetMin >= 0 ? '+' : '-'
  const abs = Math.abs(offsetMin)
  const hours = String(Math.floor(abs / 60)).padStart(2, '0')
  const mins = String(abs % 60).padStart(2, '0')
  return `${sign}${hours}:${mins}`
}

const CRITERIA = ['channel_id', 'author_id', 'mentions', 'has', 'is', 'after', 'before_date', 'during', 'author', 'filename']
const MIN_TEXT = 2

/**
 * Whether the parameters hold a criterion the server accepts alone: a filter,
 * or text with at least two characters that is not only exclusions.
 */
export function isSearchable(params: URLSearchParams): boolean {
  if (CRITERIA.some(k => params.has(k))) return true
  const wanted = textTerms(params.get('q') ?? '').join('')
  return [...wanted].length >= MIN_TEXT
}

export interface ClosedFilterToken { key: FilterKey; value: string; raw: string; valid: boolean; open: boolean }

/** The filter tokens of the line; open is the one still being typed at its end. */
export function filterTokens(input: string): ClosedFilterToken[] {
  const tokens = tokenize(input)
  const endsOpen = !/\s$/.test(input)
  return tokens.flatMap((t, i) => {
    if (!t.key) return []
    const key = t.key as FilterKey
    const value = readValue(key, t.value)
    return [{ key, value, raw: t.raw, valid: validFilterValue(key, value), open: endsOpen && i === tokens.length - 1 }]
  })
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
  if (filters.during) {
    params.set('during', filters.during)
    params.set('tz', localTimezoneOffset())
  }
  if (filters.author) params.set('author', filters.author)
  if (filters.filename) params.set('filename', filters.filename)
  return { params, unresolved }
}
