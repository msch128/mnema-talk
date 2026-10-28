import { describe, expect, it } from 'vitest'
import { nameInitial } from './initial'

describe('nameInitial', () => {
  it('uppercases the first letter', () => {
    expect(nameInitial('alice')).toBe('A')
  })
  it('keeps an astral first character whole', () => {
    expect(nameInitial('\u{1F98A} Fox')).toBe('\u{1F98A}')
    expect(nameInitial('\u{1D56C}x')).toHaveLength(2)
  })
  it('falls back for an empty name', () => {
    expect(nameInitial('')).toBe('?')
  })
})
