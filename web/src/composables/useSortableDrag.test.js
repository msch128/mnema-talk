import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { effectScope } from 'vue'
import { useSortableDrag, DRAG_DEFAULTS } from './useSortableDrag'

function pointer(type, init = {}) {
  const e = new MouseEvent(type, { bubbles: true, cancelable: true, clientX: init.x ?? 0, clientY: init.y ?? 0, button: init.button ?? 0, ctrlKey: !!init.ctrlKey })
  Object.defineProperty(e, 'pointerId', { value: init.pointerId ?? 1 })
  Object.defineProperty(e, 'pointerType', { value: init.pointerType ?? 'mouse' })
  Object.defineProperty(e, 'isPrimary', { value: init.isPrimary ?? true })
  return e
}

let scope, drag, calls, el, resolve
function setup(extra = {}) {
  calls = { start: [], drop: [], end: [], longPress: [] }
  resolve = vi.fn(({ x, y }) => (x >= 0 && y >= 0 && y < 1000 ? { y } : null))
  scope = effectScope()
  drag = scope.run(() => useSortableDrag({
    resolve,
    hitTest: () => null,
    onStart: item => calls.start.push(item),
    onDrop: (item, target) => calls.drop.push([item, target]),
    onEnd: (item, dropped) => calls.end.push([item, dropped]),
    onLongPress: item => calls.longPress.push(item),
    ...extra
  }))
  return drag
}

// Starts a press on `el` the way the template binding does.
function press(init = {}) {
  const e = pointer('pointerdown', init)
  Object.defineProperty(e, 'target', { value: init.target ?? el })
  drag.pointerDown(e, init.item ?? { id: 'a' })
}
const move = init => window.dispatchEvent(pointer('pointermove', init))
const up = init => window.dispatchEvent(pointer('pointerup', init))

beforeEach(() => {
  vi.useFakeTimers()
  el = document.createElement('div')
  document.body.appendChild(el)
})
afterEach(() => {
  scope?.stop()
  vi.useRealTimers()
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

describe('mouse', () => {
  it('starts only past the movement threshold, then drops on the target', () => {
    setup()
    press({ x: 10, y: 10 })
    expect(drag.state.phase).toBe('pending')
    move({ x: 12, y: 11 })
    expect(drag.state.phase).toBe('pending')
    expect(calls.start).toEqual([])

    move({ x: 10, y: 10 + DRAG_DEFAULTS.mouseThreshold })
    expect(drag.state.phase).toBe('dragging')
    expect(calls.start).toEqual([{ id: 'a' }])
    expect(document.documentElement.classList.contains('mnema-dragging')).toBe(true)

    move({ x: 10, y: 80 })
    expect(drag.state.target).toEqual({ y: 80 })
    up({ x: 10, y: 80 })
    expect(calls.drop).toEqual([[{ id: 'a' }, { y: 80 }]])
    expect(calls.end).toEqual([[{ id: 'a' }, true]])
    expect(drag.state.phase).toBe('idle')
    expect(drag.state.item).toBeNull()
    expect(document.documentElement.classList.contains('mnema-dragging')).toBe(false)
  })

  it('a plain click stays a click', () => {
    setup()
    const onClick = vi.fn()
    el.addEventListener('click', onClick)
    press({ x: 10, y: 10 })
    move({ x: 11, y: 11 })
    up({ x: 11, y: 11 })
    el.click()
    expect(onClick).toHaveBeenCalledTimes(1)
    expect(calls.start).toEqual([])
    expect(calls.drop).toEqual([])
  })

  it('swallows the click right after a drag, but not later ones', () => {
    setup()
    const onClick = vi.fn()
    el.addEventListener('click', onClick)
    press({ x: 0, y: 0 })
    move({ x: 0, y: 30 })
    up({ x: 0, y: 30 })
    el.click()
    expect(onClick).not.toHaveBeenCalled()
    el.click()
    expect(onClick).toHaveBeenCalledTimes(1)
  })

  it('stops guarding clicks after a short while', () => {
    setup()
    const onClick = vi.fn()
    el.addEventListener('click', onClick)
    press({ x: 0, y: 0 })
    move({ x: 0, y: 30 })
    up({ x: 0, y: 30 })
    vi.advanceTimersByTime(DRAG_DEFAULTS.clickGuardMs + 1)
    el.click()
    expect(onClick).toHaveBeenCalledTimes(1)
  })

  it('ignores other buttons, modifier clicks, secondary pointers and no-drag areas', () => {
    setup()
    press({ button: 2 })
    expect(drag.state.phase).toBe('idle')
    press({ ctrlKey: true })
    expect(drag.state.phase).toBe('idle')
    press({ isPrimary: false })
    expect(drag.state.phase).toBe('idle')
    const button = document.createElement('button')
    button.setAttribute('data-no-drag', '')
    el.appendChild(button)
    press({ target: button })
    expect(drag.state.phase).toBe('idle')
  })

  it('does nothing while disabled', () => {
    setup({ enabled: () => false })
    press()
    expect(drag.state.phase).toBe('idle')
  })

  it('ignores moves of other pointers', () => {
    setup()
    press({ x: 0, y: 0, pointerId: 1 })
    move({ x: 0, y: 50, pointerId: 2 })
    expect(drag.state.phase).toBe('pending')
  })
})

describe('cancelling', () => {
  function dragging() {
    setup()
    press({ x: 0, y: 0 })
    move({ x: 0, y: 40 })
    expect(drag.state.phase).toBe('dragging')
  }

  it('Escape cancels without dropping and keeps the key from others', () => {
    dragging()
    const behind = vi.fn()
    document.addEventListener('keydown', behind)
    const e = new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })
    document.body.dispatchEvent(e)
    expect(e.defaultPrevented).toBe(true)
    expect(behind).not.toHaveBeenCalled()
    expect(drag.state.phase).toBe('idle')
    expect(calls.drop).toEqual([])
    expect(calls.end).toEqual([[{ id: 'a' }, false]])
    document.removeEventListener('keydown', behind)
  })

  it('a drop without a target is a cancel', () => {
    dragging()
    move({ x: -50, y: 40 })
    expect(drag.state.target).toBeNull()
    up({ x: -50, y: 40 })
    expect(calls.drop).toEqual([])
    expect(calls.end).toEqual([[{ id: 'a' }, false]])
  })

  it('pointercancel and losing the window cancel', () => {
    dragging()
    window.dispatchEvent(pointer('pointercancel'))
    expect(drag.state.phase).toBe('idle')
    press({ x: 0, y: 0 })
    move({ x: 0, y: 40 })
    window.dispatchEvent(new Event('blur'))
    expect(drag.state.phase).toBe('idle')
    expect(calls.drop).toEqual([])
  })

  it('cancel() ends a drag from outside; disposing the scope cleans up', () => {
    dragging()
    drag.cancel()
    expect(drag.state.phase).toBe('idle')
    press({ x: 0, y: 0 })
    move({ x: 0, y: 40 })
    scope.stop()
    expect(drag.state.phase).toBe('idle')
    expect(document.documentElement.classList.contains('mnema-dragging')).toBe(false)
    move({ x: 0, y: 60 })
    expect(resolve).toHaveBeenCalledTimes(2)
  })

  it('a right-click while a mouse press is pending gives the press up', () => {
    setup()
    press({ x: 0, y: 0 })
    const e = new MouseEvent('contextmenu', { bubbles: true, cancelable: true })
    el.dispatchEvent(e)
    expect(e.defaultPrevented).toBe(false)
    expect(drag.state.phase).toBe('idle')
  })
})

describe('touch', () => {
  const touch = { pointerType: 'touch' }

  it('picks up after a long press, with a short vibration', () => {
    const vibrate = vi.fn()
    vi.stubGlobal('navigator', { ...navigator, vibrate })
    setup()
    press({ ...touch, x: 5, y: 5 })
    vi.advanceTimersByTime(DRAG_DEFAULTS.longPressMs - 1)
    expect(drag.state.phase).toBe('pending')
    vi.advanceTimersByTime(1)
    expect(drag.state.phase).toBe('dragging')
    expect(vibrate).toHaveBeenCalledWith(10)
    expect(calls.start).toHaveLength(1)

    move({ ...touch, x: 5, y: 120 })
    up({ ...touch, x: 5, y: 120 })
    expect(calls.drop).toEqual([[{ id: 'a' }, { y: 120 }]])
  })

  it('moving before the long press is a scroll and gives up', () => {
    setup()
    press({ ...touch, x: 5, y: 5 })
    move({ ...touch, x: 5, y: 5 + DRAG_DEFAULTS.touchSlop + 1 })
    expect(drag.state.phase).toBe('idle')
    vi.advanceTimersByTime(DRAG_DEFAULTS.longPressMs)
    expect(drag.state.phase).toBe('idle')
    expect(calls.start).toEqual([])
  })

  it('small jitter does not give up the press', () => {
    setup()
    press({ ...touch, x: 5, y: 5 })
    move({ ...touch, x: 8, y: 9 })
    vi.advanceTimersByTime(DRAG_DEFAULTS.longPressMs)
    expect(drag.state.phase).toBe('dragging')
  })

  it('a tap stays a tap', () => {
    setup()
    press({ ...touch })
    up({ ...touch })
    vi.advanceTimersByTime(DRAG_DEFAULTS.longPressMs)
    expect(drag.state.phase).toBe('idle')
    expect(calls.start).toEqual([])
  })

  it('lifting without moving after pick-up is a long press, not a drop', () => {
    setup()
    press({ ...touch, x: 5, y: 5 })
    vi.advanceTimersByTime(DRAG_DEFAULTS.longPressMs)
    move({ ...touch, x: 7, y: 8 })
    up({ ...touch, x: 7, y: 8 })
    expect(calls.longPress).toEqual([{ id: 'a' }])
    expect(calls.drop).toEqual([])
    expect(calls.end).toEqual([[{ id: 'a' }, false]])
  })

  it('blocks page scrolling and the long-press menu only while dragging', () => {
    setup()
    const before = new Event('touchmove', { cancelable: true })
    drag.touchMove(before)
    expect(before.defaultPrevented).toBe(false)

    press({ ...touch, x: 5, y: 5 })
    vi.advanceTimersByTime(DRAG_DEFAULTS.longPressMs)
    const during = new Event('touchmove', { cancelable: true })
    drag.touchMove(during)
    expect(during.defaultPrevented).toBe(true)

    const menu = new MouseEvent('contextmenu', { bubbles: true, cancelable: true })
    el.dispatchEvent(menu)
    expect(menu.defaultPrevented).toBe(true)
    expect(drag.state.phase).toBe('dragging')
  })
})

describe('auto-scroll', () => {
  it('scrolls the container near its edges, faster closer to them, and re-resolves', () => {
    vi.stubGlobal('requestAnimationFrame', cb => setTimeout(cb, 16))
    vi.stubGlobal('cancelAnimationFrame', id => clearTimeout(id))
    const box = { scrollTop: 100, getBoundingClientRect: () => ({ top: 0, bottom: 300, height: 300 }) }
    setup({ scrollContainer: () => box })
    press({ x: 0, y: 150 })
    move({ x: 0, y: 160 })
    vi.advanceTimersByTime(50)
    expect(box.scrollTop).toBe(100)

    const resolvedBefore = resolve.mock.calls.length
    move({ x: 0, y: 290 }) // 38px into the 48px bottom edge
    vi.advanceTimersByTime(16)
    expect(box.scrollTop).toBe(100 + Math.ceil(DRAG_DEFAULTS.maxScrollSpeed * 38 / 48))
    expect(resolve.mock.calls.length).toBeGreaterThan(resolvedBefore + 1)
    vi.advanceTimersByTime(16)
    expect(box.scrollTop).toBe(100 + 2 * Math.ceil(DRAG_DEFAULTS.maxScrollSpeed * 38 / 48))

    move({ x: 0, y: -20 }) // beyond the top edge: full speed up
    const at = box.scrollTop
    vi.advanceTimersByTime(16)
    expect(box.scrollTop).toBe(at - DRAG_DEFAULTS.maxScrollSpeed)

    move({ x: 0, y: 150 })
    const still = box.scrollTop
    vi.advanceTimersByTime(100)
    expect(box.scrollTop).toBe(still)
  })

  it('stops at the end of the content and when the drag ends', () => {
    vi.stubGlobal('requestAnimationFrame', cb => setTimeout(cb, 16))
    vi.stubGlobal('cancelAnimationFrame', id => clearTimeout(id))
    let top = 0
    const box = {
      get scrollTop() { return top },
      set scrollTop(v) { top = Math.min(20, Math.max(0, v)) },
      getBoundingClientRect: () => ({ top: 0, bottom: 300, height: 300 })
    }
    setup({ scrollContainer: () => box })
    press({ x: 0, y: 150 })
    move({ x: 0, y: 299 })
    vi.advanceTimersByTime(16 * 10)
    expect(top).toBe(20)
    up({ x: 0, y: 299 })
    top = 0
    vi.advanceTimersByTime(100)
    expect(top).toBe(0)
  })
})
