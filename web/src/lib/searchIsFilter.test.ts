import { describe, it, expect } from 'vitest'
import { buildSearchParams, isSearchable, parseSearchQuery, serializeSearchQuery, splitCommitted } from './searchQuery'
import { suggestFor } from './searchSuggest'

describe('is: and the poll and edit kinds', () => {
  const lookup = { members: [], channels: [] }

  it('parses is:pinned, has:poll and has:edit (case-insensitive) and rejects other values', () => {
    const p = parseSearchQuery('lemon is:Pinned has:POLL has:edit is:unread')
    expect(p.text).toBe('lemon')
    expect(p.filters).toEqual({ is: 'pinned', has: 'edit' })
    expect(p.invalid).toEqual(['is:unread'])
    expect(parseSearchQuery('has:poll').filters).toEqual({ has: 'poll' })
  })

  it('maps them to request parameters that count as criteria and writes them back', () => {
    const parsed = parseSearchQuery('is:pinned has:poll')
    const { params } = buildSearchParams(parsed, lookup)
    expect(params.get('is')).toBe('pinned')
    expect(params.get('has')).toBe('poll')
    expect(isSearchable(new URLSearchParams({ is: 'pinned' }))).toBe(true)
    expect(serializeSearchQuery(parsed)).toBe('has:poll is:pinned')
    expect(splitCommitted('is:pinned ')).toEqual({ filters: { is: 'pinned' }, rest: '' })
  })

  it('suggests the values while typing', () => {
    expect(suggestFor('is:', lookup)?.items.map(i => i.value)).toEqual(['pinned'])
    expect(suggestFor('is:pi', lookup)?.items.map(i => i.value)).toEqual(['pinned'])
    expect(suggestFor('has:p', lookup)?.items.map(i => i.value)).toEqual(['poll'])
  })
})
