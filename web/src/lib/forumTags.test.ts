import { describe, it, expect } from 'vitest'
import { TAG_COLORS, tagChipClass, tagSwatchClass, MAX_POST_TAGS, MAX_FORUM_TAGS } from './forumTags'

describe('forum tag colours', () => {
  it('gives every palette colour its own chip and swatch classes', () => {
    const chips = TAG_COLORS.map(c => tagChipClass(c))
    const swatches = TAG_COLORS.map(c => tagSwatchClass(c))
    expect(new Set(chips).size).toBe(TAG_COLORS.length)
    expect(new Set(swatches).size).toBe(TAG_COLORS.length)
  })

  it('falls back to the plain look for no or an unknown colour', () => {
    const plain = tagChipClass()
    expect(tagChipClass('')).toBe(plain)
    expect(tagChipClass('#ff0000')).toBe(plain)
    expect(tagChipClass('constructor')).toBe(plain)
    expect(tagSwatchClass('nope')).toBe(tagSwatchClass())
  })

  it('mirrors the server limits', () => {
    expect(MAX_POST_TAGS).toBe(5)
    expect(MAX_FORUM_TAGS).toBe(20)
  })
})
