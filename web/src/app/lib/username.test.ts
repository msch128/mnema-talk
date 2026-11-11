import { describe, expect, it } from 'vitest'
import { normalizeUsername } from './username'

describe('normalizeUsername', () => {
  it('turns spaces into underscores and capitals into lowercase', () => {
    expect(normalizeUsername('Max Muster')).toBe('max_muster')
    expect(normalizeUsername('A  B')).toBe('a__b')
  })

  it('leaves valid names untouched', () => {
    expect(normalizeUsername('max.muster_1')).toBe('max.muster_1')
  })
})
