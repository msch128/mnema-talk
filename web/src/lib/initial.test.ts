import { describe, expect, it } from 'vitest'
import { initialOf } from './initial'

describe('initialOf', () => {
  it('upper-cases the first character of the trimmed name', () => {
    expect(initialOf('  ada')).toBe('A')
  })
  it('keeps an astral first character whole', () => {
    expect(initialOf('🤖 Helper')).toBe('🤖')
    expect(initialOf('𠮷野')).toBe('𠮷')
  })
  it('falls back to a question mark', () => {
    expect(initialOf('')).toBe('?')
    expect(initialOf('   ')).toBe('?')
    expect(initialOf(undefined)).toBe('?')
  })
})
