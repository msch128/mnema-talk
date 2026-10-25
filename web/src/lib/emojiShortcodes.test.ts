import { describe, it, expect } from 'vitest'
import { loadEmojiShortcodes, useEmojiShortcodes } from './emojiShortcodes'

describe('emoji shortcode table', () => {
  it('loads once and shares the table', async () => {
    const first = await loadEmojiShortcodes()
    expect(first.smile).toBe('😄')
    expect(await loadEmojiShortcodes()).toBe(first)
  })

  it('fills the ref on ensure and keeps it on the next call', async () => {
    const { table, ensure } = useEmojiShortcodes()
    expect(table.value).toEqual({})
    await ensure()
    const loaded = table.value
    expect(loaded.tada).toBe('🎉')
    await ensure()
    expect(table.value).toBe(loaded)
  })
})
