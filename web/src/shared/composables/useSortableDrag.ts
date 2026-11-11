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
import { shallowReactive, toValue, getCurrentScope, onScopeDispose } from 'vue'
import type { MaybeRefOrGetter } from 'vue'
import type { FrameHandle } from './sortableDragParts'
import { createClickGuard, createWindowListeners, cancelFrame, edgeScrollSpeed, isDragPress, requestFrame } from './sortableDragParts'

export interface DragContext<Item> { x: number; y: number; item: Item; element: Element | null }
export interface SortableDragOptions<Item, Target> {
  resolve(context: DragContext<Item>): Target | null | undefined
  onDrop(item: Item, target: Target): void | Promise<void>
  onStart?: (item: Item) => void
  onEnd?: (item: Item, dropped: boolean) => void
  onLongPress?: (item: Item, event: PointerEvent) => void
  scrollContainer?: MaybeRefOrGetter<HTMLElement | null>
  enabled?: MaybeRefOrGetter<boolean>
  hitTest?: (x: number, y: number) => Element | null
  mouseThreshold?: number; longPressMs?: number; touchSlop?: number; edge?: number; maxScrollSpeed?: number; clickGuardMs?: number
}
interface DragState<Item, Target> { phase: 'idle' | 'pending' | 'dragging'; item: Item | null; target: Target | null; pointerType: string; x: number; y: number }

export const DRAG_DEFAULTS = Object.freeze({
  mouseThreshold: 4, // px of movement before a mouse/pen drag starts
  longPressMs: 350, // touch: hold this long to pick an item up
  touchSlop: 8, // touch: moving further than this before the long press = scrolling
  edge: 48, // px from the scroll container's edge where auto-scroll kicks in
  maxScrollSpeed: 14, // px per frame at (or beyond) the very edge
  clickGuardMs: 400 // how long to wait for the click that follows a drag
})

type Options<Item, Target> = SortableDragOptions<Item, Target> & Record<keyof typeof DRAG_DEFAULTS, number>

/** What the one press or drag in progress remembers. */
interface Session {
  pointerId: number | null
  source: Element | null
  startX: number
  startY: number
  pickX: number
  pickY: number
  moved: boolean
  timer: ReturnType<typeof setTimeout> | undefined
  frame: FrameHandle | null
}

interface DragCtx<Item, Target> {
  opts: Options<Item, Target>
  state: DragState<Item, Target>
  s: Session
  clickGuard: ReturnType<typeof createClickGuard>
  listeners: ReturnType<typeof createWindowListeners>
}

function isTouch(c: { state: { pointerType: string } }) {
  return c.state.pointerType === 'touch'
}

function hitTest<Item, Target>({ opts }: DragCtx<Item, Target>, x: number, y: number) {
  if (opts.hitTest) return opts.hitTest(x, y)
  return typeof document !== 'undefined' && document.elementFromPoint ? document.elementFromPoint(x, y) : null
}

/** Events of another pointer than the one that started the press are not ours. */
function foreignPointer<Item, Target>(c: DragCtx<Item, Target>, e: PointerEvent) {
  return c.s.pointerId !== null && e.pointerId !== undefined && e.pointerId !== c.s.pointerId
}

function pointerDown<Item, Target>(c: DragCtx<Item, Target>, e: PointerEvent, item: Item) {
  const { state, s, opts } = c
  if (!toValue(opts.enabled ?? true) || state.phase !== 'idle') return
  if (e.isPrimary === false) return
  const type = e.pointerType || 'mouse'
  if (!isDragPress(e, type)) return

  s.pointerId = e.pointerId ?? null
  s.source = e.currentTarget instanceof Element ? e.currentTarget : null
  s.startX = state.x = e.clientX
  s.startY = state.y = e.clientY
  s.moved = false
  Object.assign(state, { phase: 'pending', item, target: null, pointerType: type })
  c.listeners.listen()
  if (type === 'touch') s.timer = setTimeout(() => begin(c), opts.longPressMs)
}

function begin<Item, Target>(c: DragCtx<Item, Target>) {
  const { state, s } = c
  clearTimeout(s.timer)
  s.timer = undefined
  if (state.phase !== 'pending' || state.item === null) return
  state.phase = 'dragging'
  s.pickX = state.x
  s.pickY = state.y
  if (isTouch(c)) navigator.vibrate?.(10)
  else s.moved = true
  // Keeps pointerup coming when the mouse is released outside the window.
  try {
    if (s.pointerId !== null) s.source?.setPointerCapture?.(s.pointerId)
  } catch (err) {
    void err
  }
  try {
    globalThis.getSelection?.()?.removeAllRanges?.()
  } catch (err) {
    void err
  }
  document.documentElement.classList.add('mnema-dragging')
  c.opts.onStart?.(state.item)
  update(c)
}

function update<Item, Target>(c: DragCtx<Item, Target>) {
  const { state, opts } = c
  if (state.phase !== 'dragging' || state.item === null) return
  state.target = opts.resolve({ x: state.x, y: state.y, item: state.item, element: hitTest(c, state.x, state.y) }) ?? null
  scheduleScroll(c)
}

/** A pending press either turns into a drag (mouse moved enough) or gives up (touch scrolled). */
function movePending<Item, Target>(c: DragCtx<Item, Target>, e: PointerEvent) {
  const dist = Math.hypot(e.clientX - c.s.startX, e.clientY - c.s.startY)
  if (isTouch(c)) {
    if (dist > c.opts.touchSlop) reset(c) // a scroll, not a long press
  } else if (dist >= c.opts.mouseThreshold) {
    begin(c)
  }
}

function onPointerMove<Item, Target>(c: DragCtx<Item, Target>, e: PointerEvent) {
  if (foreignPointer(c, e)) return
  const { state, s } = c
  state.x = e.clientX
  state.y = e.clientY
  if (state.phase === 'pending') {
    movePending(c, e)
    return
  }
  if (state.phase !== 'dragging') return
  if (!s.moved && Math.hypot(e.clientX - s.pickX, e.clientY - s.pickY) > c.opts.touchSlop) s.moved = true
  if (e.cancelable) e.preventDefault()
  update(c)
}

function onPointerUp<Item, Target>(c: DragCtx<Item, Target>, e: PointerEvent) {
  if (foreignPointer(c, e)) return
  const { state, opts } = c
  if (state.phase === 'pending') {
    reset(c) // a click or a tap
    return
  }
  if (state.phase !== 'dragging') return
  const { item, target } = state
  if (item === null) return
  c.clickGuard.arm()
  if (isTouch(c) && !c.s.moved) {
    reset(c)
    opts.onEnd?.(item, false)
    opts.onLongPress?.(item, e)
    return
  }
  reset(c)
  if (target) opts.onDrop(item, target)
  opts.onEnd?.(item, !!target)
}

/** Ends a drag (or a pending press) without dropping. */
function cancel<Item, Target>(c: DragCtx<Item, Target>) {
  const { item, phase } = c.state
  if (phase === 'idle') return
  if (phase === 'dragging') c.clickGuard.arm()
  reset(c)
  if (phase === 'dragging' && item !== null) c.opts.onEnd?.(item, false)
}

function onKeydown<Item, Target>(c: DragCtx<Item, Target>, e: KeyboardEvent) {
  if (e.key !== 'Escape' || c.state.phase === 'idle') return
  e.preventDefault()
  e.stopPropagation()
  cancel(c)
}

// A long press on touch fires contextmenu on some platforms; while an item
// is held that belongs to the drag (see onLongPress).
function onContextMenu<Item, Target>(c: DragCtx<Item, Target>, e: MouseEvent) {
  if (c.state.phase === 'idle') return
  if (c.state.phase === 'pending' && !isTouch(c)) {
    reset(c)
    return
  }
  e.preventDefault()
  e.stopPropagation()
}

// ---- Auto-scroll ----

function scrollSpeed<Item, Target>({ opts, state }: DragCtx<Item, Target>) {
  const el = toValue(opts.scrollContainer)
  if (!el || state.phase !== 'dragging') return 0
  return edgeScrollSpeed(el, state.y, opts.edge, opts.maxScrollSpeed)
}

function scheduleScroll<Item, Target>(c: DragCtx<Item, Target>) {
  if (c.s.frame === null && scrollSpeed(c) !== 0) c.s.frame = requestFrame(() => scrollStep(c))
}

function scrollStep<Item, Target>(c: DragCtx<Item, Target>) {
  c.s.frame = null
  const speed = scrollSpeed(c)
  if (!speed) return
  const el = toValue(c.opts.scrollContainer)
  if (!el) return
  const before = el.scrollTop
  el.scrollTop = before + speed
  if (el.scrollTop === before) return // at the end already
  update(c) // the content moved under the pointer
}

function reset<Item, Target>(c: DragCtx<Item, Target>) {
  const { s } = c
  clearTimeout(s.timer)
  s.timer = undefined
  if (s.frame !== null) cancelFrame(s.frame)
  s.frame = null
  c.listeners.unlisten()
  try {
    if (s.pointerId !== null && s.source?.hasPointerCapture?.(s.pointerId)) s.source.releasePointerCapture(s.pointerId)
  } catch (err) {
    void err
  }
  s.pointerId = null
  s.source = null
  s.moved = false
  if (typeof document !== 'undefined') document.documentElement.classList.remove('mnema-dragging')
  Object.assign(c.state, { phase: 'idle', item: null, target: null, pointerType: '' })
}

/**
 * options:
 *   resolve(ctx) → target | null       required
 *   onDrop(item, target)               required
 *   onStart(item), onEnd(item, dropped) optional
 *   onLongPress(item, event)           touch: lifted without moving after pick-up
 *   scrollContainer                    ref/getter/element auto-scrolled at its edges
 *   enabled                            ref/getter (default true)
 *   hitTest(x: number, y: number) → Element | null     default document.elementFromPoint
 *   ...DRAG_DEFAULTS overrides
 */
export function useSortableDrag<Item, Target>(options: SortableDragOptions<Item, Target>) {
  // Targets and items are replaced as a unit; preserve their original identities.
  const state = shallowReactive<DragState<Item, Target>>({
    phase: 'idle', // 'idle' | 'pending' | 'dragging'
    item: null,
    target: null,
    pointerType: '',
    x: 0,
    y: 0
  })
  const opts = { ...DRAG_DEFAULTS, ...options }
  const c: DragCtx<Item, Target> = {
    opts,
    state,
    s: { pointerId: null, source: null, startX: 0, startY: 0, pickX: 0, pickY: 0, moved: false, timer: undefined, frame: null },
    clickGuard: createClickGuard(opts.clickGuardMs),
    listeners: createWindowListeners({
      move: e => onPointerMove(c, e),
      up: e => onPointerUp(c, e),
      cancel: () => cancel(c),
      keydown: e => onKeydown(c, e),
      contextmenu: e => onContextMenu(c, e),
      visibility: () => { if (document.visibilityState === 'hidden') cancel(c) }
    })
  }

  if (getCurrentScope()) {
    onScopeDispose(() => {
      reset(c)
      c.clickGuard.release()
    })
  }

  return {
    state,
    /** Bind to pointerdown of a draggable element: @pointerdown="drag.pointerDown($event, item)". */
    pointerDown: (e: PointerEvent, item: Item) => pointerDown(c, e, item),
    // While a finger drags, the page must not scroll; while it only presses,
    // scrolling stays possible (and gives up the press, see onPointerMove).
    // Bind it as a (non-passive) touchmove listener on the list element:
    // browsers decide whether a touch may scroll when it starts, so a listener
    // added once the drag began would come too late.
    touchMove(e: TouchEvent) {
      if (state.phase === 'dragging' && e.cancelable) e.preventDefault()
    },
    cancel: () => cancel(c),
    /** Re-resolves the target at the current pointer, e.g. after the list changed. */
    refresh: () => update(c)
  }
}
