import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { required } from '../store-test-support.fixture'
import type { SortableDragOptions, DragContext } from './useSortableDrag'
import type { Mock } from 'vitest'
import { effectScope } from 'vue'
import { useSortableDrag, DRAG_DEFAULTS } from './useSortableDrag'

interface TestItem { id: string }
interface TestTarget { y: number }
interface PointerInit { x?: number; y?: number; button?: number; ctrlKey?: boolean; metaKey?: boolean; altKey?: boolean; shiftKey?: boolean; pointerId?: number; pointerType?: string; isPrimary?: boolean; target?: EventTarget; item?: TestItem }
class TestPointerEvent extends MouseEvent implements PointerEvent {
  width = 1; height = 1; pressure = 0; tangentialPressure = 0; tiltX = 0; tiltY = 0; twist = 0; altitudeAngle = 0; azimuthAngle = 0; persistentDeviceId = 0
  pointerId = 1; pointerType = 'mouse'; isPrimary = true
  getCoalescedEvents(): PointerEvent[] { return [] }
  getPredictedEvents(): PointerEvent[] { return [] }
}
function pointer(type: string, init: PointerInit = {}) {
  const e = new TestPointerEvent(type, { bubbles: true, cancelable: true, clientX: init.x ?? 0, clientY: init.y ?? 0, button: init.button ?? 0, ctrlKey: !!init.ctrlKey, metaKey: !!init.metaKey, altKey: !!init.altKey, shiftKey: !!init.shiftKey })
  Object.defineProperty(e, 'pointerId', { value: init.pointerId ?? 1 })
  Object.defineProperty(e, 'pointerType', { value: init.pointerType ?? 'mouse' })
  Object.defineProperty(e, 'isPrimary', { value: init.isPrimary ?? true })
  return e
}

let scope: ReturnType<typeof effectScope> | undefined
let drag: ReturnType<typeof useSortableDrag<TestItem, TestTarget>>
let calls: { start: TestItem[]; drop: [TestItem, TestTarget][]; end: [TestItem, boolean][]; longPress: TestItem[] }
let el: HTMLDivElement
let resolve: Mock<(context: DragContext<TestItem>) => TestTarget | null>
function setup(extra: Partial<SortableDragOptions<TestItem, TestTarget>> = {}, nativeHitTest = false) {
  calls = { start: [], drop: [], end: [], longPress: [] }
  resolve = vi.fn(({ x, y }) => (x >= 0 && y >= 0 && y < 1000 ? { y } : null))
  scope = effectScope()
  drag = required(scope.run(() => useSortableDrag({
    resolve,
    ...(nativeHitTest ? {} : { hitTest: () => null }),
    onStart: item => { calls.start.push(item) },
    onDrop: (item, target) => { calls.drop.push([item, target]) },
    onEnd: (item, dropped) => { calls.end.push([item, dropped]) },
    onLongPress: item => { calls.longPress.push(item) },
    ...extra
  })))
  return drag
}

// Starts a press on `el` the way the template binding does.
function press(init: PointerInit = {}) {
  const e = pointer('pointerdown', init)
  Object.defineProperty(e, 'target', { value: init.target ?? el })
  drag.pointerDown(e, init.item ?? { id: 'a' })
}
const move = (init: PointerInit) => window.dispatchEvent(pointer('pointermove', init))
const up = (init: PointerInit) => window.dispatchEvent(pointer('pointerup', init))

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
  it('supports legacy pointer events without an identifier and swallows synthesized mouse events', () => {
    setup()
    const e = pointer('pointerdown')
    Object.defineProperty(e, 'pointerId', { value: undefined })
    drag.pointerDown(e, { id: 'a' })
    move({ y: 30 })
    up({ y: 30 })
    const down = new MouseEvent('mousedown', { bubbles: true, cancelable: true })
    const upEvent = new MouseEvent('mouseup', { bubbles: true, cancelable: true })
    el.dispatchEvent(down)
    el.dispatchEvent(upEvent)
    expect(down.defaultPrevented).toBe(true)
    expect(upEvent.defaultPrevented).toBe(true)
    el.click()
  })

  it('allows use without a scope and cleans up safely when the browser globals go away', () => {
    const standalone = useSortableDrag<TestItem, TestTarget>({ resolve: () => ({ y: 1 }), onDrop: vi.fn() })
    vi.stubGlobal('window', undefined)
    standalone.pointerDown(pointer('pointerdown'), { id: 'a' })
    standalone.cancel()
    vi.stubGlobal('document', undefined)
    standalone.cancel()
    vi.unstubAllGlobals()
  })
  it('rejects every modifier, a duplicate press, and a foreign pointer release', () => {
    setup()
    for (const init of [{ metaKey: true }, { altKey: true }, { shiftKey: true }]) {
      press(init)
      expect(drag.state.phase).toBe('idle')
    }
    press({ pointerType: '' })
    expect(drag.state.pointerType).toBe('mouse')
    press({ item: { id: 'b' } })
    expect(drag.state.item).toEqual({ id: 'a' })
    up({ pointerId: 2 })
    expect(drag.state.phase).toBe('pending')
    drag.cancel()
    drag.cancel()
  })

  it('tolerates pointer capture and selection APIs throwing and releases capture defensively', () => {
    setup()
    el.setPointerCapture = vi.fn(() => { throw new Error('gone') })
    el.hasPointerCapture = vi.fn(() => true)
    el.releasePointerCapture = vi.fn(() => { throw new Error('gone') })
    vi.stubGlobal('getSelection', () => { throw new Error('unavailable') })
    const e = pointer('pointerdown')
    Object.defineProperty(e, 'currentTarget', { value: el })
    drag.pointerDown(e, { id: 'a' })
    move({ y: 20 })
    expect(drag.state.phase).toBe('dragging')
    up({ y: 20 })
    expect(drag.state.phase).toBe('idle')
    expect(el.releasePointerCapture).toHaveBeenCalledOnce()
  })

  it('uses native hit testing when supplied and tolerates environments without it', () => {
    const hit = vi.fn(() => el)
    const spy = vi.spyOn(document, 'elementFromPoint').mockImplementation(hit)
    setup({}, true)
    press()
    move({ y: 20 })
    expect(resolve).toHaveBeenLastCalledWith(expect.objectContaining({ element: el }))
    spy.mockRestore()
    drag.cancel()
    Object.defineProperty(document, 'elementFromPoint', { value: undefined, configurable: true })
    press()
    move({ y: 20 })
    expect(resolve).toHaveBeenLastCalledWith(expect.objectContaining({ element: null }))
    Reflect.deleteProperty(document, 'elementFromPoint')
  })
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

  it('lets the next press click even when the browser sent no click after the drag', () => {
    // Chromium fires no click after a drag with pointer capture; a quick
    // click right after the drop (e.g. "Undo" in the toast) must still work.
    setup()
    const onClick = vi.fn()
    const onDown = vi.fn()
    el.addEventListener('click', onClick)
    el.addEventListener('mousedown', onDown)
    press({ x: 0, y: 0 })
    move({ x: 0, y: 30 })
    up({ x: 0, y: 30 })
    el.dispatchEvent(pointer('pointerdown'))
    el.dispatchEvent(pointer('mousedown'))
    el.click()
    expect(onDown).toHaveBeenCalledTimes(1)
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

  it('captures the pointer while dragging, so a release outside the window still ends it', () => {
    setup()
    el.setPointerCapture = vi.fn()
    el.releasePointerCapture = vi.fn()
    el.hasPointerCapture = vi.fn(() => true)
    const e = pointer('pointerdown', { x: 0, y: 0, pointerId: 7 })
    Object.defineProperty(e, 'target', { value: el })
    Object.defineProperty(e, 'currentTarget', { value: el })
    drag.pointerDown(e, { id: 'a' })
    expect(el.setPointerCapture).not.toHaveBeenCalled()
    move({ x: 0, y: 20, pointerId: 7 })
    expect(el.setPointerCapture).toHaveBeenCalledWith(7)
    up({ x: 0, y: 20, pointerId: 7 })
    expect(el.releasePointerCapture).toHaveBeenCalledWith(7)
  })

  it('ignores moves of other pointers', () => {
    setup()
    press({ x: 0, y: 0, pointerId: 1 })
    move({ x: 0, y: 50, pointerId: 2 })
    expect(drag.state.phase).toBe('pending')
  })
})

describe('cancelling', () => {
  it('ignores events and long-press timers already queued when cancellation removes listeners', () => {
    const add = vi.spyOn(window, 'addEventListener')
    const timer = vi.spyOn(globalThis, 'setTimeout')
    setup()
    press({ pointerType: 'touch' })
    const handlers = add.mock.calls.filter(call => ['pointermove', 'pointerup', 'keydown'].includes(call[0]))
    const begin = required(timer.mock.calls.find(call => call[1] === DRAG_DEFAULTS.longPressMs)?.[0])
    drag.cancel()
    begin()
    for (const [type, listener] of handlers) {
      const event = type === 'keydown' ? new KeyboardEvent(type, { key: 'Escape' }) : pointer(type, { y: 50 })
      if (typeof listener === 'function') listener(event)
      else listener?.handleEvent(event)
    }
    expect(drag.state.phase).toBe('idle')
    expect(calls.start).toEqual([])
    expect(calls.drop).toEqual([])
    add.mockRestore()
    timer.mockRestore()
  })
  it('ignores unrelated keys and cancels when the document becomes hidden', () => {
    setup()
    press()
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter' }))
    Object.defineProperty(document, 'visibilityState', { value: 'visible', configurable: true })
    document.dispatchEvent(new Event('visibilitychange'))
    expect(drag.state.phase).toBe('pending')
    move({ y: 30 })
    Object.defineProperty(document, 'visibilityState', { value: 'hidden', configurable: true })
    document.dispatchEvent(new Event('visibilitychange'))
    expect(drag.state.phase).toBe('idle')
    Reflect.deleteProperty(document, 'visibilityState')
  })
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
    required(scope).stop()
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
  it('suppresses a pending touch context menu and leaves noncancelable touchmove unchanged', () => {
    setup()
    press({ pointerType: 'touch' })
    const menu = new MouseEvent('contextmenu', { bubbles: true, cancelable: true })
    el.dispatchEvent(menu)
    expect(menu.defaultPrevented).toBe(true)
    vi.advanceTimersByTime(DRAG_DEFAULTS.longPressMs)
    const movement = new TouchEvent('touchmove')
    drag.touchMove(movement)
    expect(movement.defaultPrevented).toBe(false)
  })
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
    const before = new TouchEvent('touchmove', { cancelable: true })
    drag.touchMove(before)
    expect(before.defaultPrevented).toBe(false)

    press({ ...touch, x: 5, y: 5 })
    vi.advanceTimersByTime(DRAG_DEFAULTS.longPressMs)
    const during = new TouchEvent('touchmove', { cancelable: true })
    drag.touchMove(during)
    expect(during.defaultPrevented).toBe(true)

    const menu = new MouseEvent('contextmenu', { bubbles: true, cancelable: true })
    el.dispatchEvent(menu)
    expect(menu.defaultPrevented).toBe(true)
    expect(drag.state.phase).toBe('dragging')
  })
})

describe('auto-scroll', () => {
  it('cancels a scheduled animation frame when disposed during an edge drag', () => {
    const cancel = vi.fn()
    vi.stubGlobal('requestAnimationFrame', () => 42)
    vi.stubGlobal('cancelAnimationFrame', cancel)
    const box = document.createElement('div')
    box.getBoundingClientRect = () => new DOMRect(0, 0, 100, 300)
    setup({ scrollContainer: box })
    press({ y: 150 }); move({ y: 290 })
    required(scope).stop()
    expect(cancel).toHaveBeenCalledWith(42)
  })
  it('uses a timer when animation frames are absent and stops when the container disappears', () => {
    vi.stubGlobal('requestAnimationFrame', undefined)
    const box = document.createElement('div')
    box.getBoundingClientRect = () => new DOMRect(0, 0, 100, 300)
    let container: HTMLDivElement | null = box
    setup({ scrollContainer: () => container })
    press({ y: 150 })
    move({ y: 299 })
    vi.advanceTimersByTime(16)
    expect(box.scrollTop).toBeGreaterThan(0)
    container = null
    vi.advanceTimersByTime(16)
    expect(drag.state.phase).toBe('dragging')
    container = box
    move({ y: 0 })
    drag.cancel()
    const before = box.scrollTop
    vi.advanceTimersByTime(100)
    expect(box.scrollTop).toBe(before)
  })

  it('does not scroll zero-height containers and refresh is harmless while idle', () => {
    const box = document.createElement('div')
    setup({ scrollContainer: box })
    drag.refresh()
    expect(resolve).not.toHaveBeenCalled()
    press()
    move({ y: 20 })
    vi.advanceTimersByTime(100)
    expect(box.scrollTop).toBe(0)
    drag.refresh()
    expect(resolve).toHaveBeenCalledTimes(2)
  })
  it('scrolls the container near its edges, faster closer to them, and re-resolves', () => {
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => setTimeout(cb, 16))
    vi.stubGlobal('cancelAnimationFrame', (id: number) => clearTimeout(id))
    const box = document.createElement('div')
    box.scrollTop = 100
    box.getBoundingClientRect = () => new DOMRect(0, 0, 100, 300)
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
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => setTimeout(cb, 16))
    vi.stubGlobal('cancelAnimationFrame', (id: number) => clearTimeout(id))
    let top = 0
    const box = document.createElement('div')
    Object.defineProperty(box, 'scrollTop', {
      get() { return top },
      set(v: number) { top = Math.min(20, Math.max(0, v)) }
    })
    box.getBoundingClientRect = () => new DOMRect(0, 0, 100, 300)
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
