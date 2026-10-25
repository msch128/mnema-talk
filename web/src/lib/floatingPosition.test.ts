import { describe, expect, it } from 'vitest'
import { boxOf, placeNear, pointBox } from './floatingPosition'

const view = { width: 1000, height: 800 }
const menu = { width: 200, height: 300 }

describe('placeNear', () => {
  it('opens below the anchor, aligned to its left edge', () => {
    const anchor = { left: 100, top: 100, right: 130, bottom: 130 }
    expect(placeNear(anchor, menu, view)).toEqual({ x: 100, y: 134 })
  })

  it('flips above the anchor when there is no room below', () => {
    const anchor = { left: 100, top: 740, right: 130, bottom: 770 }
    expect(placeNear(anchor, menu, view)).toEqual({ x: 100, y: 740 - 4 - 300 })
  })

  it('aligns to the anchor right edge when the left edge leaves no room', () => {
    const anchor = { left: 940, top: 100, right: 970, bottom: 130 }
    expect(placeNear(anchor, menu, view)).toEqual({ x: 770, y: 134 })
  })

  it('opens towards the inside for a pointer near the bottom right corner', () => {
    const { x, y } = placeNear(pointBox(990, 790), menu, view, { gap: 0 })
    expect(x).toBe(790)
    expect(y).toBe(490)
  })

  it('keeps a panel taller than either side inside the window', () => {
    const tall = { width: 200, height: 700 }
    const anchor = { left: 100, top: 300, right: 130, bottom: 330 }
    expect(placeNear(anchor, tall, view).y).toBe(92)
  })

  it('keeps a panel that fits where it was asked for, even close to the edge', () => {
    expect(placeNear({ left: 2, top: 2, right: 4, bottom: 4 }, menu, view).x).toBe(2)
  })

  it('never goes past the margin when the panel is wider than the window', () => {
    const wide = { width: 1200, height: 100 }
    expect(placeNear({ left: 2, top: 2, right: 4, bottom: 4 }, wide, view).x).toBe(8)
  })
})

describe('boxOf', () => {
  it('copies the four edges and nothing else', () => {
    expect(boxOf({ left: 1, top: 2, right: 3, bottom: 4, width: 2, height: 2 } as DOMRect)).toEqual({ left: 1, top: 2, right: 3, bottom: 4 })
  })
})
