import { describe, expect, it } from 'vitest'
import { edgeScrollSpeed, isDragPress } from './sortableDragParts'

function container(top: number, bottom: number) {
  return { getBoundingClientRect: () => ({ top, bottom, height: bottom - top }) } as unknown as HTMLElement
}

describe('edgeScrollSpeed', () => {
  const el = container(100, 400)

  it('does not scroll in the middle of the container', () => {
    expect(edgeScrollSpeed(el, 250, 48, 14)).toBe(0)
  })

  it('scrolls up near the top edge, faster the closer to it', () => {
    expect(edgeScrollSpeed(el, 140, 48, 14)).toBe(-Math.ceil(14 * (8 / 48)))
    expect(edgeScrollSpeed(el, 100, 48, 14)).toBe(-14)
  })

  it('scrolls down near the bottom edge and caps at the top speed beyond it', () => {
    expect(edgeScrollSpeed(el, 380, 48, 14)).toBe(Math.ceil(14 * (28 / 48)))
    expect(edgeScrollSpeed(el, 900, 48, 14)).toBe(14)
  })

  it('shrinks the edge zone for a short container and ignores an empty one', () => {
    expect(edgeScrollSpeed(container(0, 30), 5, 48, 10)).toBe(-Math.ceil(10 * (5 / 10)))
    expect(edgeScrollSpeed(container(10, 10), 10, 48, 10)).toBe(0)
  })
})

describe('isDragPress', () => {
  const press = (init: Partial<PointerEvent>, target: Element | null = null) => ({ button: 0, ctrlKey: false, metaKey: false, altKey: false, shiftKey: false, target, ...init }) as PointerEvent

  it('accepts a plain primary-button press and any touch', () => {
    expect(isDragPress(press({}), 'mouse')).toBe(true)
    expect(isDragPress(press({ button: 2, ctrlKey: true }), 'touch')).toBe(true)
  })

  it('refuses other buttons and modified presses for a mouse', () => {
    expect(isDragPress(press({ button: 1 }), 'mouse')).toBe(false)
    expect(isDragPress(press({ shiftKey: true }), 'pen')).toBe(false)
  })

  it('refuses a press on a no-drag element', () => {
    const inner = document.createElement('button')
    const outer = document.createElement('div')
    outer.setAttribute('data-no-drag', '')
    outer.append(inner)
    expect(isDragPress(press({}, inner), 'mouse')).toBe(false)
    expect(isDragPress(press({}, document.createElement('div')), 'mouse')).toBe(true)
  })
})
