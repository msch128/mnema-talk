import { describe, expect, it } from 'vitest'
import { applySuggestion, suggestFor } from './searchSuggest'

const lookup = {
  members: [
    { id: 'u1', username: 'ada', display_name: 'Ada Lovelace' },
    { id: 'u2', username: 'bob', display_name: '' },
    ...Array.from({ length: 8 }, (_, i) => ({ id: `x${i}`, username: `zed${i}`, display_name: '' })),
  ],
  channels: [{ id: 'c1', name: 'general' }, { id: 'c2', name: 'Lounge' }],
}

describe('suggestFor', () => {
  it('returns nothing when no filter value is being typed', () => {
    expect(suggestFor('', lookup)).toBeNull()
    expect(suggestFor('hello', lookup)).toBeNull()
    expect(suggestFor('from:ada done', lookup)).toBeNull()
    expect(suggestFor('before:2026', lookup)).toBeNull()
  })

  it('suggests members by username or display name', () => {
    const ctx = suggestFor('hello from:lov', lookup)
    expect(ctx?.key).toBe('from')
    expect(ctx?.start).toBe(6)
    expect(ctx?.items).toEqual([{ value: 'ada', label: 'Ada Lovelace', detail: '@ada' }])
    expect(suggestFor('mentions:', lookup)?.items).toHaveLength(6)
    expect(suggestFor('from:b', lookup)?.items[0]).toEqual({ value: 'bob', label: 'bob' })
  })

  it('suggests channels and has kinds', () => {
    expect(suggestFor('in:lou', lookup)?.items).toEqual([{ value: 'Lounge', label: '#Lounge' }])
    expect(suggestFor('in:#gen', lookup)?.items[0]?.value).toBe('general')
    expect(suggestFor('has:i', lookup)?.items.map(i => i.value)).toEqual(['image'])
    expect(suggestFor('has:', lookup)?.items).toHaveLength(7)
    expect(suggestFor('author:', lookup)?.items.map(i => i.value)).toEqual(['bot', 'webhook', 'member'])
  })

  it('works inside an open quote and stays quiet for an exact single match or no match', () => {
    expect(suggestFor('from:"Ada L', lookup)?.items[0]?.value).toBe('ada')
    expect(suggestFor('has:image', lookup)).toBeNull()
    expect(suggestFor('in:zzz', lookup)).toBeNull()
  })
})

describe('applySuggestion', () => {
  it('replaces the open token and finishes it with a space', () => {
    const ctx = suggestFor('hello from:lov', lookup)!
    expect(applySuggestion('hello from:lov', ctx, ctx.items[0]!)).toBe('hello from:ada ')
    const quoted = { key: 'from' as const, start: 0, items: [] }
    expect(applySuggestion('from:Ad', quoted, { value: 'Ada Lovelace', label: 'x' })).toBe('from:"Ada Lovelace" ')
  })
})
