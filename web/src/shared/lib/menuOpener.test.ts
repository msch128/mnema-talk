import { afterEach, describe, expect, it, vi } from 'vitest'
import { menuOpener } from './menuKeys'

afterEach(() => { vi.restoreAllMocks(); document.body.replaceChildren() })

describe('menuOpener', () => {
  it('returns to what had focus', () => {
    const button = document.createElement('button')
    document.body.appendChild(button)
    button.focus()
    expect(menuOpener(null, 0, 0)).toBe(button)
  })

  it('falls back to the anchor when a touch or right click focused nothing', () => {
    const anchor = document.createElement('div')
    expect(menuOpener(anchor, 0, 0)).toBe(anchor)
  })

  it('falls back to the control under the point the menu opened at', () => {
    const row = document.createElement('a')
    row.href = '/c/general'
    const label = document.createElement('span')
    row.appendChild(label)
    document.body.appendChild(row)
    document.elementFromPoint = vi.fn(() => label)
    expect(menuOpener(null, 10, 10)).toBe(row)
  })
})
