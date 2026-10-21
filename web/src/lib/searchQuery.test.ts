import { describe, expect, it } from 'vitest'
import {
  buildSearchParams, dayStart, isSearchDate, isSearchable, parseSearchQuery, quoteValue, serializeSearchQuery, splitCommitted,
} from './searchQuery'

const lookup = {
  members: [
    { id: 'u1', username: 'ada', display_name: 'Ada Lovelace' },
    { id: 'u2', username: 'bob', display_name: '' },
  ],
  channels: [{ id: 'c1', name: 'general' }, { id: 'c2', name: 'Lounge' }],
}

describe('parseSearchQuery', () => {
  it('reads free text and every filter kind', () => {
    const p = parseSearchQuery('hello from:ada in:general has:image before:2026-02-01 after:2026-01-01 mentions:bob world')
    expect(p.text).toBe('hello world')
    expect(p.filters).toEqual({ from: 'ada', in: 'general', has: 'image', before: '2026-02-01', after: '2026-01-01', mentions: 'bob' })
    expect(p.invalid).toEqual([])
  })

  it('supports quoting for values with spaces and for phrases', () => {
    const p = parseSearchQuery('from:"Ada Lovelace" "two words" in:"a \\"b\\""')
    expect(p.filters.from).toBe('Ada Lovelace')
    expect(p.filters.in).toBe('a "b"')
    expect(p.text).toBe('two words')
  })

  it('tolerates an unterminated quote', () => {
    expect(parseSearchQuery('from:"Ada Lo').filters.from).toBe('Ada Lo')
    expect(parseSearchQuery('"open phrase').text).toBe('open phrase')
  })

  it('keeps unknown keys as text, lets the last filter of a kind win and ignores key case', () => {
    const p = parseSearchQuery('foo:bar FROM:ada from:bob HAS:LINK')
    expect(p.text).toBe('foo:bar')
    expect(p.filters).toEqual({ from: 'bob', has: 'link' })
  })

  it('reports invalid values instead of searching for them', () => {
    const p = parseSearchQuery('has:audio before:2026-13-40 after:soon from: x')
    expect(p.invalid).toEqual(['has:audio', 'before:2026-13-40', 'after:soon', 'from:'])
    expect(p.filters).toEqual({})
    expect(p.text).toBe('x')
  })

  it('handles empty input', () => {
    expect(parseSearchQuery('   ')).toEqual({ text: '', filters: {}, invalid: [] })
  })
})

describe('serialize and quote', () => {
  it('quotes only when needed and round-trips', () => {
    expect(quoteValue('plain')).toBe('plain')
    expect(quoteValue('a b')).toBe('"a b"')
    expect(quoteValue('say "hi"')).toBe('"say \\"hi\\""')
    const line = serializeSearchQuery({ text: 'hello', filters: { from: 'Ada Lovelace', has: 'video' } })
    expect(line).toBe('from:"Ada Lovelace" has:video hello')
    expect(parseSearchQuery(line)).toMatchObject({ text: 'hello', filters: { from: 'Ada Lovelace', has: 'video' } })
    expect(serializeSearchQuery({ text: '', filters: {} })).toBe('')
  })
})

describe('splitCommitted', () => {
  it('commits finished filters and keeps the token being typed', () => {
    expect(splitCommitted('from:ada hel')).toEqual({ filters: { from: 'ada' }, rest: 'hel' })
    expect(splitCommitted('hello has:image')).toEqual({ filters: {}, rest: 'hello has:image' })
    expect(splitCommitted('hello has:image ')).toEqual({ filters: { has: 'image' }, rest: 'hello ' })
    expect(splitCommitted('has:foo ')).toEqual({ filters: {}, rest: 'has:foo ' })
    expect(splitCommitted('')).toEqual({ filters: {}, rest: '' })
  })
})

describe('dates', () => {
  it('validates real calendar days only', () => {
    expect(isSearchDate('2026-02-28')).toBe(true)
    expect(isSearchDate('2026-02-30')).toBe(false)
    expect(isSearchDate('26-02-01')).toBe(false)
  })

  it('maps days to local day boundaries', () => {
    expect(new Date(dayStart('2026-03-04')).getDate()).toBe(4)
    expect(new Date(dayStart('2026-03-04', 1)).getTime() - new Date(dayStart('2026-03-04')).getTime()).toBeGreaterThanOrEqual(23 * 3600_000)
    expect(dayStart('nope')).toBe('')
  })
})

describe('buildSearchParams', () => {
  it('resolves names case-insensitively by username, display name and channel', () => {
    const { params, unresolved } = buildSearchParams(
      parseSearchQuery('hi from:"ada lovelace" mentions:@BOB in:#lounge has:file after:2026-01-01 before:2026-01-31'), lookup, { limit: '25' })
    expect(params.get('limit')).toBe('25')
    expect(params.get('q')).toBe('hi')
    expect(params.get('author_id')).toBe('u1')
    expect(params.get('mentions')).toBe('u2')
    expect(params.get('channel_id')).toBe('c2')
    expect(params.get('has')).toBe('file')
    expect(new Date(params.get('after')!).getTime()).toBeLessThan(new Date(params.get('before_date')!).getTime())
    expect(unresolved).toEqual([])
  })

  it('reports names that match nothing and leaves them out', () => {
    const { params, unresolved } = buildSearchParams(parseSearchQuery('from:ghost in:nowhere mentions:ghost'), lookup)
    expect(unresolved).toEqual(['from', 'mentions', 'in'])
    expect(isSearchable(params)).toBe(false)
  })

  it('treats a single day as a non-empty window', () => {
    const { params } = buildSearchParams(parseSearchQuery('after:2026-05-05 before:2026-05-05'), lookup)
    expect(new Date(params.get('after')!).getTime()).toBeLessThan(new Date(params.get('before_date')!).getTime())
  })
})

describe('isSearchable', () => {
  it('needs at least one criterion', () => {
    expect(isSearchable(new URLSearchParams({ limit: '25' }))).toBe(false)
    expect(isSearchable(new URLSearchParams({ limit: '25', has: 'video' }))).toBe(true)
  })
})
