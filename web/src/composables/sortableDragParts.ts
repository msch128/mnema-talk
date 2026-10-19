// The pieces of useSortableDrag that need no drag state: frames, the click
// guard after a drop, window listeners and edge auto-scroll speed.

const CLICK_EVENTS = ['click', 'mousedown', 'mouseup']

export type FrameHandle = { kind: 'raf'; id: number } | { kind: 'timer'; id: ReturnType<typeof setTimeout> }

export const requestFrame = (cb: () => void): FrameHandle => globalThis.requestAnimationFrame ? { kind: 'raf', id: globalThis.requestAnimationFrame(cb) } : { kind: 'timer', id: setTimeout(cb, 16) }
export const cancelFrame = (handle: FrameHandle) => handle.kind === 'raf' ? globalThis.cancelAnimationFrame(handle.id) : clearTimeout(handle.id)

/** A press that may start a drag: any touch, or a plain primary-button mouse press, not on a no-drag element. */
export function isDragPress(e: PointerEvent, type: string) {
  if (type !== 'touch' && (e.button !== 0 || e.ctrlKey || e.metaKey || e.altKey || e.shiftKey)) return false
  return !(e.target instanceof Element && e.target.closest('[data-no-drag]'))
}

/** px per frame to scroll `el` (negative = up) for a pointer at `y`, 0 away from its edges. */
export function edgeScrollSpeed(el: HTMLElement, y: number, edgeSize: number, maxSpeed: number) {
  const r = el.getBoundingClientRect()
  if (!r.height) return 0
  const edge = Math.min(edgeSize, r.height / 3)
  const ratio = (d: number) => Math.min(1, Math.max(0, d) / edge)
  if (y < r.top + edge) return -Math.ceil(maxSpeed * ratio(r.top + edge - y))
  if (y > r.bottom - edge) return Math.ceil(maxSpeed * ratio(y - (r.bottom - edge)))
  return 0
}

// The click (and on some touch browsers the synthesized mouse events) that
// follows the drag must not reach the row below the pointer. Not every
// browser sends that click (Chromium doesn't after a captured drag), so the
// guard also ends with the next press: that is a new click of its own, e.g.
// on the toast's "Undo" right after the drop.
export function createClickGuard(clickGuardMs: number) {
  let releaseClickGuard: (() => void) | null = null

  function arm() {
    if (typeof window === 'undefined') return
    releaseClickGuard?.()
    const swallow = (e: Event) => {
      e.preventDefault()
      e.stopPropagation()
      e.stopImmediatePropagation?.()
      if (e.type === 'click') done()
    }
    const stopTimer = setTimeout(done, clickGuardMs)
    function done() {
      clearTimeout(stopTimer)
      releaseClickGuard = null
      for (const type of CLICK_EVENTS) window.removeEventListener(type, swallow, true)
      window.removeEventListener('pointerdown', done, true)
    }
    for (const type of CLICK_EVENTS) window.addEventListener(type, swallow, true)
    window.addEventListener('pointerdown', done, true)
    releaseClickGuard = done
  }

  return { arm, release: () => releaseClickGuard?.() }
}

export interface DragHandlers {
  move: (e: PointerEvent) => void
  up: (e: PointerEvent) => void
  cancel: () => void
  keydown: (e: KeyboardEvent) => void
  contextmenu: (e: MouseEvent) => void
  visibility: () => void
}

/** Adds the window listeners of a press or drag once, and removes them again. */
export function createWindowListeners(h: DragHandlers) {
  let listening = false

  function listen() {
    if (listening || typeof window === 'undefined') return
    listening = true
    window.addEventListener('pointermove', h.move, { passive: false })
    window.addEventListener('pointerup', h.up)
    window.addEventListener('pointercancel', h.cancel)
    window.addEventListener('keydown', h.keydown, true)
    window.addEventListener('contextmenu', h.contextmenu, true)
    window.addEventListener('blur', h.cancel)
    document.addEventListener('visibilitychange', h.visibility)
  }

  function unlisten() {
    if (!listening) return
    listening = false
    window.removeEventListener('pointermove', h.move)
    window.removeEventListener('pointerup', h.up)
    window.removeEventListener('pointercancel', h.cancel)
    window.removeEventListener('keydown', h.keydown, true)
    window.removeEventListener('contextmenu', h.contextmenu, true)
    window.removeEventListener('blur', h.cancel)
    document.removeEventListener('visibilitychange', h.visibility)
  }

  return { listen, unlisten }
}
