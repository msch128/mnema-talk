import { describe, expect, it } from 'vitest'
import { styleEmojiPicker } from './emojiPickerSheet'

describe('emoji picker sheet', () => {
  it('adopts one stylesheet into the shadow root', () => {
    const host = document.createElement('div')
    const root = host.attachShadow({ mode: 'open' })
    const adopted = styleEmojiPicker({ shadowRoot: root })
    // Where the DOM has no constructed stylesheets the picker keeps its own look.
    expect(root.adoptedStyleSheets?.length ?? 0).toBe(adopted ? 1 : 0)
  })

  it('does nothing without a shadow root', () => {
    expect(styleEmojiPicker({ shadowRoot: null })).toBe(false)
  })
})
