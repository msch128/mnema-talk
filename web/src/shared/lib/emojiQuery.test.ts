import { describe, it, expect } from 'vitest'
import { applyStandardEmoji, suggestStandardEmoji } from './emojiQuery'
import table from './emojiShortcodes.json'

describe('standard emoji shortcodes', () => {
  it('ships the common GitHub-style shortcodes', () => {
    const codes = Object.keys(table)
    expect(codes.length).toBeGreaterThan(1500)
    expect(table.smile).toBe('😄')
    expect(table.tada).toBe('🎉')
  })

  it('ranks prefix matches before contains matches and respects the limit', () => {
    const t = { sunny: 'A', moon_sun: 'B', sunflower: 'C', rain: 'D' }
    expect(suggestStandardEmoji('sun', t).map(e => e.code)).toEqual(['sunny', 'sunflower', 'moon_sun'])
    expect(suggestStandardEmoji('SUN', t, 2).map(e => e.code)).toEqual(['sunny', 'sunflower'])
    expect(suggestStandardEmoji('zzz', t)).toEqual([])
  })

  it('splices the glyph in with one trailing space and drops the rest of the word', () => {
    expect(applyStandardEmoji('say :smi now', 4, 8, '😄')).toEqual({ text: 'say 😄 now', caret: 6 })
    expect(applyStandardEmoji(':smi', 0, 4, '😄')).toEqual({ text: '😄 ', caret: 3 })
    expect(applyStandardEmoji(':smile', 0, 4, '😄').text).toBe('😄 ')
  })
})
