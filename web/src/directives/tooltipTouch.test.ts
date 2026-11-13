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

  it('puts the shown text away when the finger moves on', () => {
    el.dispatchEvent(pointer('pointerdown', 'touch'))
    vi.advanceTimersByTime(500)
    expect(show).toHaveBeenCalledOnce()
    el.dispatchEvent(pointer('pointermove', 'touch', 10, 14))
    expect(hide).not.toHaveBeenCalled()
    el.dispatchEvent(pointer('pointermove', 'touch', 10, 40))
    expect(hide).toHaveBeenCalledOnce()
  })
})

describe('holdToShow on a text inside a control', () => {
  // The control's touch area (::after) lies over the text, so the press reaches the control.
  let control: HTMLElement
  let text: HTMLElement
  let show: ReturnType<typeof vi.fn<() => void>>
  let off: () => void

  beforeEach(() => {
    vi.useFakeTimers()
    control = document.createElement('div')
    control.setAttribute('role', 'button')
    text = document.createElement('span')
    control.appendChild(text)
    document.body.appendChild(control)
    vi.spyOn(text, 'getBoundingClientRect').mockReturnValue(new DOMRect(40, 0, 200, 20))
    show = vi.fn<() => void>()
    off = holdToShow(text, show, () => undefined)
  })
  afterEach(() => { off(); control.remove(); vi.useRealTimers() })

  it('shows the text when the control is held on the text', () => {
    control.dispatchEvent(pointer('pointerdown', 'touch', 120, 10))
    vi.advanceTimersByTime(500)
    expect(show).toHaveBeenCalledOnce()
  })

  it('leaves a press elsewhere on the control to the control', () => {
    control.dispatchEvent(pointer('pointerdown', 'touch', 10, 10))
    vi.advanceTimersByTime(500)
    expect(show).not.toHaveBeenCalled()
  })
})
