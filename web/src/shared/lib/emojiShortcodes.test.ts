import { describe, expect, it, vi } from 'vitest'
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

describe('failed table load', () => {
  it('is retried on the next call instead of cached', async () => {
    vi.resetModules()
    vi.doMock('./emojiShortcodes.json', () => { throw new Error('chunk failed') })
    const fresh = await import('./emojiShortcodes')
    await expect(fresh.loadEmojiShortcodes()).rejects.toThrow()
    vi.doMock('./emojiShortcodes.json', () => ({ default: { smile: 'x' } }))
    expect((await fresh.loadEmojiShortcodes()).smile).toBe('x')
    vi.doUnmock('./emojiShortcodes.json')
  })
})
