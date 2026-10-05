// Drag and drop for sortable lists, built on pointer events so one code path
// serves mouse, pen and touch (native HTML5 drag and drop does nothing on
// touch screens).
//
//   mouse / pen  the drag starts once the pointer moved a few pixels, so a
//                plain click still clicks
//   touch        the drag starts after a long press; moving the finger before
//                that is a scroll and gives up. While dragging, the page does
//                not scroll (bind touchMove). Lifting the finger without
//                having moved is a "long press" (e.g. open a context menu).
//
// Escape, pointercancel, losing the window and a drop without a target all
// cancel. The click that follows a drag is swallowed so it doesn't navigate.
// Near the top or bottom edge of the scroll container the container scrolls.
//
// The caller decides what a position means: resolve({ x, y, item, element })
// returns a drop target (any object) or null, element being what is under the
// pointer.
import { reactive, toValue, getCurrentScope, onScopeDispose } from 'vue'

export const DRAG_DEFAULTS = Object.freeze({
  mouseThreshold: 4, // px of movement before a mouse/pen drag starts
  longPressMs: 350, // touch: hold this long to pick an item up
  touchSlop: 8, // touch: moving further than this before the long press = scrolling
  edge: 48, // px from the scroll container's edge where auto-scroll kicks in
  maxScrollSpeed: 14, // px per frame at (or beyond) the very edge
  clickGuardMs: 400 // how long to wait for the click that follows a drag
})

const CLICK_EVENTS = ['click', 'mousedown', 'mouseup']

/**
 * options:
 *   resolve(ctx) → target | null       required
 *   onDrop(item, target)               required
 *   onStart(item), onEnd(item, dropped) optional
 *   onLongPress(item, event)           touch: lifted without moving after pick-up
 *   scrollContainer                    ref/getter/element auto-scrolled at its edges
 *   enabled                            ref/getter (default true)
 *   hitTest(x, y) → Element | null     default document.elementFromPoint
 *   ...DRAG_DEFAULTS overrides
 */
export function useSortableDrag(options) {
  const opts = { ...DRAG_DEFAULTS, ...options }
  const state = reactive({
    phase: 'idle', // 'idle' | 'pending' | 'dragging'
    item: null,
    target: null,
    pointerType: '',
    x: 0,
    y: 0
  })

  let pointerId = null
  let startX = 0
  let startY = 0
  let pickX = 0
  let pickY = 0
  let moved = false
  let timer = null
  let frame = null
  let listening = false
  let releaseClickGuard = null

  const raf = cb => (globalThis.requestAnimationFrame ? globalThis.requestAnimationFrame(cb) : setTimeout(cb, 16))
  const cancelRaf = id => (globalThis.cancelAnimationFrame ? globalThis.cancelAnimationFrame(id) : clearTimeout(id))

  function hitTest(x, y) {
    if (opts.hitTest) return opts.hitTest(x, y)
    return typeof document !== 'undefined' && document.elementFromPoint ? document.elementFromPoint(x, y) : null
  }

  function isTouch() {
    return state.pointerType === 'touch'
  }

  /** Bind to pointerdown of a draggable element: @pointerdown="drag.pointerDown($event, item)". */
  function pointerDown(e, item) {
    if (!toValue(opts.enabled ?? true) || state.phase !== 'idle') return
    if (e.isPrimary === false) return
    const type = e.pointerType || 'mouse'
    if (type !== 'touch' && (e.button !== 0 || e.ctrlKey || e.metaKey || e.altKey || e.shiftKey)) return
    if (e.target?.closest?.('[data-no-drag]')) return

    pointerId = e.pointerId ?? null
    startX = state.x = e.clientX
    startY = state.y = e.clientY
    moved = false
    Object.assign(state, { phase: 'pending', item, target: null, pointerType: type })
    listen()
    if (type === 'touch') timer = setTimeout(begin, opts.longPressMs)
  }

  function begin() {
    clearTimeout(timer)
    timer = null
    if (state.phase !== 'pending') return
    state.phase = 'dragging'
    pickX = state.x
    pickY = state.y
    if (isTouch()) navigator.vibrate?.(10)
    else moved = true
    try {
      globalThis.getSelection?.()?.removeAllRanges?.()
    } catch {
      // nothing selected
    }
    document.documentElement.classList.add('mnema-dragging')
    opts.onStart?.(state.item)
    update()
  }

  function update() {
    if (state.phase !== 'dragging') return
    state.target = opts.resolve({ x: state.x, y: state.y, item: state.item, element: hitTest(state.x, state.y) }) ?? null
    scheduleScroll()
  }

  function onPointerMove(e) {
    if (pointerId !== null && e.pointerId !== undefined && e.pointerId !== pointerId) return
    state.x = e.clientX
    state.y = e.clientY
    if (state.phase === 'pending') {
      const dist = Math.hypot(e.clientX - startX, e.clientY - startY)
      if (isTouch()) {
        if (dist > opts.touchSlop) reset() // a scroll, not a long press
      } else if (dist >= opts.mouseThreshold) {
        begin()
      }
      return
    }
    if (state.phase !== 'dragging') return
    if (!moved && Math.hypot(e.clientX - pickX, e.clientY - pickY) > opts.touchSlop) moved = true
    if (e.cancelable) e.preventDefault()
    update()
  }

  function onPointerUp(e) {
    if (pointerId !== null && e.pointerId !== undefined && e.pointerId !== pointerId) return
    if (state.phase === 'pending') {
      reset() // a click or a tap
      return
    }
    if (state.phase !== 'dragging') return
    const { item, target } = state
    guardClick()
    if (isTouch() && !moved) {
      reset()
      opts.onEnd?.(item, false)
      opts.onLongPress?.(item, e)
      return
    }
    reset()
    if (target) opts.onDrop(item, target)
    opts.onEnd?.(item, !!target)
  }

  /** Ends a drag (or a pending press) without dropping. */
  function cancel() {
    if (state.phase === 'idle') return
    const { item, phase } = state
    if (phase === 'dragging') guardClick()
    reset()
    if (phase === 'dragging') opts.onEnd?.(item, false)
  }

  function onKeydown(e) {
    if (e.key !== 'Escape' || state.phase === 'idle') return
    e.preventDefault()
    e.stopPropagation()
    cancel()
  }

  // While a finger drags, the page must not scroll; while it only presses,
  // scrolling stays possible (and gives up the press, see onPointerMove).
  // Bind it as a (non-passive) touchmove listener on the list element:
  // browsers decide whether a touch may scroll when it starts, so a listener
  // added once the drag began would come too late.
  function touchMove(e) {
    if (state.phase === 'dragging' && e.cancelable) e.preventDefault()
  }

  // A long press on touch fires contextmenu on some platforms; while an item
  // is held that belongs to the drag (see onLongPress).
  function onContextMenu(e) {
    if (state.phase === 'idle') return
    if (state.phase === 'pending' && !isTouch()) {
      reset()
      return
    }
    e.preventDefault()
    e.stopPropagation()
  }

  function onVisibility() {
    if (document.visibilityState === 'hidden') cancel()
  }

  // ---- Auto-scroll ----

  function scrollSpeed() {
    const el = toValue(opts.scrollContainer)
    if (!el || state.phase !== 'dragging') return 0
    const r = el.getBoundingClientRect()
    if (!r.height) return 0
    const edge = Math.min(opts.edge, r.height / 3)
    const ratio = d => Math.min(1, Math.max(0, d) / edge)
    if (state.y < r.top + edge) return -Math.ceil(opts.maxScrollSpeed * ratio(r.top + edge - state.y))
    if (state.y > r.bottom - edge) return Math.ceil(opts.maxScrollSpeed * ratio(state.y - (r.bottom - edge)))
    return 0
  }

  function scheduleScroll() {
    if (frame === null && scrollSpeed() !== 0) frame = raf(scrollStep)
  }

  function scrollStep() {
    frame = null
    const speed = scrollSpeed()
    if (!speed) return
    const el = toValue(opts.scrollContainer)
    const before = el.scrollTop
    el.scrollTop = before + speed
    if (el.scrollTop === before) return // at the end already
    update() // the content moved under the pointer
  }

  // ---- Click guard ----

  // The click (and on some touch browsers the synthesized mouse events) that
  // follows the drag must not reach the row below the pointer.
  function guardClick() {
    if (typeof window === 'undefined') return
    releaseClickGuard?.()
    const swallow = e => {
      e.preventDefault()
      e.stopPropagation()
      e.stopImmediatePropagation?.()
      if (e.type === 'click') done()
    }
    const stopTimer = setTimeout(done, opts.clickGuardMs)
    function done() {
      clearTimeout(stopTimer)
      releaseClickGuard = null
      for (const type of CLICK_EVENTS) window.removeEventListener(type, swallow, true)
    }
    for (const type of CLICK_EVENTS) window.addEventListener(type, swallow, true)
    releaseClickGuard = done
  }

  // ---- Listener bookkeeping ----

  function listen() {
    if (listening || typeof window === 'undefined') return
    listening = true
    window.addEventListener('pointermove', onPointerMove, { passive: false })
    window.addEventListener('pointerup', onPointerUp)
    window.addEventListener('pointercancel', cancel)
    window.addEventListener('keydown', onKeydown, true)
    window.addEventListener('contextmenu', onContextMenu, true)
    window.addEventListener('blur', cancel)
    document.addEventListener('visibilitychange', onVisibility)
  }

  function unlisten() {
    if (!listening) return
    listening = false
    window.removeEventListener('pointermove', onPointerMove, { passive: false })
    window.removeEventListener('pointerup', onPointerUp)
    window.removeEventListener('pointercancel', cancel)
    window.removeEventListener('keydown', onKeydown, true)
    window.removeEventListener('contextmenu', onContextMenu, true)
    window.removeEventListener('blur', cancel)
    document.removeEventListener('visibilitychange', onVisibility)
  }

  function reset() {
    clearTimeout(timer)
    timer = null
    if (frame !== null) cancelRaf(frame)
    frame = null
    unlisten()
    pointerId = null
    moved = false
    if (typeof document !== 'undefined') document.documentElement.classList.remove('mnema-dragging')
    Object.assign(state, { phase: 'idle', item: null, target: null, pointerType: '' })
  }

  if (getCurrentScope()) {
    onScopeDispose(() => {
      reset()
      releaseClickGuard?.()
    })
  }

  /** Re-resolves the target at the current pointer, e.g. after the list changed. */
  function refresh() {
    update()
  }

  return { state, pointerDown, touchMove, cancel, refresh }
}
