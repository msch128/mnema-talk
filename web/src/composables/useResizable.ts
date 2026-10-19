import type { Ref } from 'vue'
import { reactive, ref, computed, onScopeDispose } from 'vue'

export type PanelSide = 'left' | 'right' | 'top' | 'bottom'
export type PanelAxis = 'x' | 'y'
export interface PanelConfig { defaultWidth: number; min: number; max: number }
export interface PanelDefinition<Name extends string = string> extends PanelConfig { name: Name; side: PanelSide; visible?: () => boolean }
interface FittedPanel { width: number; min: number; visible: boolean }
export interface ResizePanel { name: string; side: PanelSide; axis: PanelAxis; min: number; defaultWidth: number; width: number; maxNow: number; dragging: boolean; startDrag(event: PointerEvent): void; onKeydown(event: KeyboardEvent): void; reset(): void }

// Resizable side panels.
//
// Each panel keeps a *preferred* width (what the user dragged to, persisted in
// localStorage) and an *effective* width (preferred width squeezed so the
// center column never drops below its minimum on narrow windows). Shrinking
// the window therefore never overwrites the user's choice: widening it again
// restores the panels.
//
// The same works vertically (axis 'y'): a panel at the 'bottom' (or 'top') of
// a column, e.g. the chat under a Talk's stage. "Width" then means height and
// the window's height is what gets shared.

export const STORAGE_PREFIX = 'mnema.panel.'
export const KEY_STEP = 8
export const KEY_STEP_LARGE = 32

export function clamp(value: number, min: number, max: number) {
  const upper = Math.max(min, max)
  return Math.min(upper, Math.max(min, value))
}

// Turns whatever came out of storage into a valid width for this panel.
export function sanitizeWidth(raw: unknown, { defaultWidth, min, max }: PanelConfig) {
  if (raw === null || raw === undefined || raw === '') return defaultWidth
  const n = Number(raw)
  if (!Number.isFinite(n)) return defaultWidth
  return clamp(Math.round(n), min, max)
}

function defaultStorage() {
  try {
    return globalThis.localStorage ?? null
  } catch {
    return null
  }
}

export function storageKey(name: string, dimension = 'width') {
  return `${STORAGE_PREFIX}${name}.${dimension}`
}

export function loadWidth(name: string, config: PanelConfig, storage: Storage | null = defaultStorage(), dimension = 'width') {
  try {
    return sanitizeWidth(storage ? storage.getItem(storageKey(name, dimension)) : null, config)
  } catch {
    return config.defaultWidth
  }
}

export function saveWidth(name: string, width: number, storage: Storage | null = defaultStorage(), dimension = 'width') {
  try {
    if (!storage) return false
    storage.setItem(storageKey(name, dimension), String(Math.round(width)))
    return true
  } catch {
    return false
  }
}

// Squeezes visible panels so `centerMin` px remain for the center column.
// Panels are shrunk from the end of the list first (thread, then member list,
// then the left sidebar), never below their own minimum. Hidden panels get 0.
export function fitPanels(panels: FittedPanel[], viewportWidth: number, centerMin: number) {
  const widths = panels.map(p => (p.visible ? p.width : 0))
  let overflow = widths.reduce((sum, w) => sum + w, 0) + centerMin - viewportWidth
  for (let i = panels.length - 1; i >= 0 && overflow > 0; i--) {
    const panel = panels[i]
    const width = widths[i]
    if (!panel?.visible || width === undefined) continue
    const give = Math.min(overflow, width - panel.min)
    if (give > 0) {
      widths[i] = width - give
      overflow -= give
    }
  }
  return widths
}

// Largest width panel `index` may take right now without squeezing the center.
export function availableMax(index: number, effectiveWidths: number[], panel: PanelConfig, viewportWidth: number, centerMin: number) {
  const others = effectiveWidths.reduce((sum, w, i) => (i === index ? sum : sum + w), 0)
  return clamp(viewportWidth - centerMin - others, panel.min, panel.max)
}

// Panels at the start of the axis (left, top) grow when the separator moves
// forward (right, down); panels at the end (right, bottom) when it moves back.
function growsForward(side: PanelSide) {
  return side === 'left' || side === 'top'
}

function isVertical(side: PanelSide) {
  return side === 'top' || side === 'bottom'
}

// A left panel grows when the pointer moves right, a right panel when it moves
// left; likewise a top panel downwards and a bottom panel upwards.
export function dragWidth(startWidth: number, startX: number, currentX: number, side: PanelSide) {
  const dx = currentX - startX
  return startWidth + (growsForward(side) ? dx : -dx)
}

// Arrow keys move the separator; returns the width change for the panel.
// Left/Right for side panels, Up/Down for top/bottom panels.
export function keyboardDelta(key: string, shiftKey: boolean, side: PanelSide) {
  const step = shiftKey ? KEY_STEP_LARGE : KEY_STEP
  const forward = isVertical(side) ? 'ArrowDown' : 'ArrowRight'
  const back = isVertical(side) ? 'ArrowUp' : 'ArrowLeft'
  if (key === forward) return growsForward(side) ? step : -step
  if (key === back) return growsForward(side) ? -step : step
  return 0
}

function currentViewportSize(axis: PanelAxis) {
  if (typeof window === 'undefined') return axis === 'y' ? 800 : 1280
  return axis === 'y' ? window.innerHeight : window.innerWidth
}

/** The window's width or height, updated once per frame while it is resized. */
function useViewportSize(axis: PanelAxis) {
  const viewport = ref(currentViewportSize(axis))
  let resizeFrame = 0
  function onWindowResize() {
    if (resizeFrame) return
    resizeFrame = requestAnimationFrame(() => {
      resizeFrame = 0
      viewport.value = currentViewportSize(axis)
    })
  }
  if (typeof window !== 'undefined') {
    window.addEventListener('resize', onWindowResize)
    onScopeDispose(() => {
      window.removeEventListener('resize', onWindowResize)
      if (resizeFrame) cancelAnimationFrame(resizeFrame)
    })
  }
  return viewport
}

// The width a key press asks for (Home, End or an arrow), or null for any other key.
function keyboardWidth(e: KeyboardEvent, def: PanelDefinition, current: number, max: number) {
  if (e.key === 'Home') return def.min
  if (e.key === 'End') return max
  const delta = keyboardDelta(e.key, e.shiftKey, def.side)
  return delta ? current + delta : null
}

interface PanelDrag {
  event: PointerEvent
  handle: Element
  def: PanelDefinition
  axis: PanelAxis
  startWidth: number
  max: number
  dragging: Ref<string | null>
  /** Shows a width without persisting it. */
  setWidth: (width: number) => void
  /** Persists the width the drag ended on. */
  commit: () => void
  onFinish: () => void
}

/** Follows one pointer on a separator until it is released. Returns the function that ends the drag. */
function beginPanelDrag({ event: e, handle, def, axis, startWidth, max, dragging, setWidth, commit, onFinish }: PanelDrag) {
  const pointerId = e.pointerId
  e.preventDefault()
  try {
    handle.setPointerCapture(pointerId)
  } catch {
    // Capture is best effort (e.g. synthetic events).
  }

  const coord = (ev: PointerEvent) => (axis === 'y' ? ev.clientY : ev.clientX)
  const startX = coord(e)
  let lastX = startX
  let frame = 0
  let ended = false

  const body = document.body
  const prevUserSelect = body.style.userSelect
  const prevCursor = body.style.cursor
  body.style.userSelect = 'none'
  body.style.cursor = axis === 'y' ? 'row-resize' : 'col-resize'
  dragging.value = def.name

  const apply = () => {
    frame = 0
    setWidth(clamp(Math.round(dragWidth(startWidth, startX, lastX, def.side)), def.min, max))
  }
  const onMove = (ev: PointerEvent) => {
    if (ev.pointerId !== pointerId) return
    lastX = coord(ev)
    if (!frame) frame = requestAnimationFrame(apply)
  }
  const finish = () => {
    if (ended) return
    ended = true
    onFinish()
    if (frame) cancelAnimationFrame(frame)
    apply()
    commit()
    dragging.value = null
    body.style.userSelect = prevUserSelect
    body.style.cursor = prevCursor
    handle.removeEventListener('pointermove', onMove as EventListener)
    handle.removeEventListener('pointerup', onEnd as EventListener)
    handle.removeEventListener('pointercancel', onEnd as EventListener)
    handle.removeEventListener('lostpointercapture', onEnd as EventListener)
    try {
      if (handle.hasPointerCapture?.(pointerId)) handle.releasePointerCapture(pointerId)
    } catch {
      // already released
    }
  }
  const onEnd = (ev: PointerEvent) => {
    if (ev.pointerId !== pointerId) return
    if (ev.type === 'pointerup') lastX = coord(ev)
    finish()
  }
  handle.addEventListener('pointermove', onMove as EventListener)
  handle.addEventListener('pointerup', onEnd as EventListener)
  handle.addEventListener('pointercancel', onEnd as EventListener)
  handle.addEventListener('lostpointercapture', onEnd as EventListener)
  return finish
}

/**
 * @param {Array<{name: string, side: 'left'|'right'|'top'|'bottom', defaultWidth: number, min: number, max: number, visible?: () => boolean}>} defs
 *   Panels in shrink-priority order (the last one is squeezed first). For
 *   axis 'y' the sides are 'top'/'bottom' and the numbers are heights.
 * @param {{ centerMin?: number, storage?: Storage|null, axis?: 'x'|'y' }} options
 *   centerMin: what stays for the rest (the center column, or the area above
 *   a bottom panel) on small windows.
 * @returns {Record<string, object>} one reactive handle state per panel name
 */
export function useResizable<const Name extends string>(defs: readonly PanelDefinition<Name>[], { centerMin = 400, storage = defaultStorage(), axis = 'x' }: { centerMin?: number; storage?: Storage | null; axis?: PanelAxis } = {}): Record<Name, ResizePanel> {
  const dimension = axis === 'y' ? 'height' : 'width'
  const viewport = useViewportSize(axis)
  const dragging = ref<string | null>(null)
  let finishDrag: (() => void) | null = null
  onScopeDispose(() => finishDrag?.())
  const preferred = reactive(Object.fromEntries(defs.map(d => [d.name, loadWidth(d.name, d, storage, dimension)])))

  const visibility = () => defs.map(d => (d.visible ? !!d.visible() : true))

  const effective = computed(() => {
    const vis = visibility()
    return fitPanels(
      defs.map((d, i) => ({ width: preferred[d.name] ?? d.defaultWidth, min: d.min, visible: vis[i] ?? false })),
      viewport.value,
      centerMin
    )
  })

  function maxFor(index: number) {
    const def = defs[index]
    return def ? availableMax(index, effective.value, def, viewport.value, centerMin) : 0
  }

  function setWidth(def: PanelDefinition, width: number, persist: boolean) {
    preferred[def.name] = width
    if (persist) saveWidth(def.name, width, storage, dimension)
  }

  function startDrag(index: number, e: PointerEvent) {
    if (e.button !== 0) return
    const def = defs[index]
    const handle = e.currentTarget
    if (!def || !(handle instanceof Element)) return
    finishDrag?.()
    finishDrag = beginPanelDrag({
      event: e,
      handle,
      def,
      axis,
      startWidth: effective.value[index] ?? def.defaultWidth,
      max: maxFor(index),
      dragging,
      setWidth: width => setWidth(def, width, false),
      commit: () => saveWidth(def.name, preferred[def.name] ?? def.defaultWidth, storage, dimension),
      onFinish: () => { finishDrag = null }
    })
  }

  function onKeydown(index: number, e: KeyboardEvent) {
    const def = defs[index]
    if (!def) return
    const max = maxFor(index)
    const next = keyboardWidth(e, def, effective.value[index] ?? def.defaultWidth, max)
    if (next === null) return
    e.preventDefault()
    setWidth(def, clamp(next, def.min, max), true)
  }

  function reset(index: number) {
    const def = defs[index]
    if (def) setWidth(def, def.defaultWidth, true)
  }

  return Object.fromEntries(defs.map((def, index) => [def.name, reactive({
    name: String(def.name),
    side: def.side,
    axis,
    min: def.min,
    defaultWidth: def.defaultWidth,
    width: computed(() => effective.value[index] ?? def.defaultWidth),
    maxNow: computed(() => maxFor(index)),
    dragging: computed(() => dragging.value === def.name),
    startDrag: (e: PointerEvent) => startDrag(index, e),
    onKeydown: (e: KeyboardEvent) => onKeydown(index, e),
    reset: () => reset(index)
  })])) as Record<Name, ResizePanel>
}
