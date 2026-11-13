import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { holdToShow } from './tooltipTouch'

function pointer(type: string, pointerType: string, x = 10, y = 10): Event {
  const event = new Event(type, { bubbles: true, cancelable: true })
  Object.assign(event, { pointerType, clientX: x, clientY: y })
  return event
}

describe('holdToShow', () => {
  let el: HTMLButtonElement
  let show: ReturnType<typeof vi.fn<() => void>>
  let hide: ReturnType<typeof vi.fn<() => void>>
  let off: () => void

  beforeEach(() => {
    vi.useFakeTimers()
    el = document.createElement('button')
    document.body.appendChild(el)
    show = vi.fn<() => void>()
    hide = vi.fn<() => void>()
    off = holdToShow(el, show, hide)
  })
  afterEach(() => { off(); el.remove(); vi.useRealTimers() })

  it('shows the full text on a long press and keeps the press from clicking', () => {
    const clicked = vi.fn()
    el.addEventListener('click', clicked)
    el.dispatchEvent(pointer('pointerdown', 'touch'))
    vi.advanceTimersByTime(500)
    expect(show).toHaveBeenCalledOnce()
    el.dispatchEvent(pointer('pointerup', 'touch'))
    el.click()
    expect(clicked).not.toHaveBeenCalled()
    vi.advanceTimersByTime(2500)
    expect(hide).toHaveBeenCalled()
    el.click()
    expect(clicked).toHaveBeenCalledOnce()
  })

  it('leaves a short tap alone', () => {
    const clicked = vi.fn()
    el.addEventListener('click', clicked)
    el.dispatchEvent(pointer('pointerdown', 'touch'))
    vi.advanceTimersByTime(200)
    el.dispatchEvent(pointer('pointerup', 'touch'))
    el.click()
    vi.advanceTimersByTime(1000)
    expect(show).not.toHaveBeenCalled()
    expect(clicked).toHaveBeenCalledOnce()
  })

  it('gives up when the finger moves (a scroll) and ignores the mouse', () => {
    el.dispatchEvent(pointer('pointerdown', 'touch'))
    el.dispatchEvent(pointer('pointermove', 'touch', 10, 40))
    vi.advanceTimersByTime(1000)
    el.dispatchEvent(pointer('pointerdown', 'mouse'))
    vi.advanceTimersByTime(1000)
    expect(show).not.toHaveBeenCalled()
  })
})
