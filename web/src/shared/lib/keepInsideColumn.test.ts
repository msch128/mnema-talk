import { afterEach, describe, expect, it } from 'vitest'
import { keepInsideColumn } from './keepInsideColumn'

// jsdom does no layout: each case states where the browser would put the column and the card.
function layout(column: { left: number; width: number }, card: { left: number; width: number }) {
  const main = document.createElement('main')
  const el = document.createElement('div')
  el.className = 'card'
  main.appendChild(el)
  document.body.appendChild(main)
  main.getBoundingClientRect = () => new DOMRect(column.left, 0, column.width, 600)
  el.getBoundingClientRect = () => {
    const max = parseFloat(el.style.getPropertyValue('max-width')) || Infinity
    const width = Math.min(card.width, max)
    const right = card.left + card.width
    return new DOMRect(right - width, 50, width, 300)
  }
  return el
}

function positioned(value: string) {
  const sheet = document.createElement('style')
  sheet.textContent = `.card { position: ${value} }`
  document.head.appendChild(sheet)
}

describe('keepInsideColumn', () => {
  afterEach(() => { document.body.replaceChildren(); document.head.replaceChildren() })

  it('leaves a card that fits under its icon where it is', () => {
    positioned('absolute')
    const card = layout({ left: 240, width: 900 }, { left: 700, width: 384 })
    keepInsideColumn(card)
    expect(card.style.getPropertyValue('translate')).toBe('')
    expect(card.style.getPropertyValue('max-width')).toBe('')
  })

  it('narrows and moves a card that would run past the left edge of a narrow column', () => {
    positioned('absolute')
    const card = layout({ left: 220, width: 360 }, { left: 48, width: 384 })
    keepInsideColumn(card)
    expect(card.style.getPropertyValue('max-width')).toBe('344px')
    expect(card.style.getPropertyValue('translate')).toBe('140px 0')
  })

  it('leaves the phone sheet alone', () => {
    positioned('fixed')
    const card = layout({ left: 0, width: 390 }, { left: -100, width: 384 })
    keepInsideColumn(card)
    expect(card.style.getPropertyValue('translate')).toBe('')
  })
})
