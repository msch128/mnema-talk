import { reactive, ref, computed, onScopeDispose } from 'vue'

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

export function clamp(value, min, max) {
  const upper = Math.max(min, max)
  return Math.min(upper, Math.max(min, value))
}

// Turns whatever came out of storage into a valid width for this panel.
export function sanitizeWidth(raw, { defaultWidth, min, max }) {
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

export function storageKey(name, dimension = 'width') {
  return `${STORAGE_PREFIX}${name}.${dimension}`
}

export function loadWidth(name, config, storage = defaultStorage(), dimension = 'width') {
  try {
    return sanitizeWidth(storage ? storage.getItem(storageKey(name, dimension)) : null, config)
  } catch {
    return config.defaultWidth
  }
}

export function saveWidth(name, width, storage = defaultStorage(), dimension = 'width') {
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
export function fitPanels(panels, viewportWidth, centerMin) {
  const widths = panels.map(p => (p.visible ? p.width : 0))
  let overflow = widths.reduce((sum, w) => sum + w, 0) + centerMin - viewportWidth
  for (let i = panels.length - 1; i >= 0 && overflow > 0; i--) {
    if (!panels[i].visible) continue
    const give = Math.min(overflow, widths[i] - panels[i].min)
    if (give > 0) {
      widths[i] -= give
      overflow -= give
    }
  }
  return widths
}

// Largest width panel `index` may take right now without squeezing the center.
export function availableMax(index, effectiveWidths, panel, viewportWidth, centerMin) {
  const others = effectiveWidths.reduce((sum, w, i) => (i === index ? sum : sum + w), 0)
  return clamp(viewportWidth - centerMin - others, panel.min, panel.max)
}

// Panels at the start of the axis (left, top) grow when the separator moves
// forward (right, down); panels at the end (right, bottom) when it moves back.
function growsForward(side) {
  return side === 'left' || side === 'top'
}

function isVertical(side) {
  return side === 'top' || side === 'bottom'
}

// A left panel grows when the pointer moves right, a right panel when it moves
// left; likewise a top panel downwards and a bottom panel upwards.
export function dragWidth(startWidth, startX, currentX, side) {
  const dx = currentX - startX
  return startWidth + (growsForward(side) ? dx : -dx)
}

// Arrow keys move the separator; returns the width change for the panel.
// Left/Right for side panels, Up/Down for top/bottom panels.
export function keyboardDelta(key, shiftKey, side) {
  const step = shiftKey ? KEY_STEP_LARGE : KEY_STEP
  const forward = isVertical(side) ? 'ArrowDown' : 'ArrowRight'
  const back = isVertical(side) ? 'ArrowUp' : 'ArrowLeft'
  if (key === forward) return growsForward(side) ? step : -step
  if (key === back) return growsForward(side) ? -step : step
  return 0
}

function currentViewportSize(axis) {
  if (typeof window === 'undefined') return axis === 'y' ? 800 : 1280
  return axis === 'y' ? window.innerHeight : window.innerWidth
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
export function useResizable(defs, { centerMin = 400, storage = defaultStorage(), axis = 'x' } = {}) {
  const dimension = axis === 'y' ? 'height' : 'width'
  const viewport = ref(currentViewportSize(axis))
  const dragging = ref(null)
  const preferred = reactive(Object.fromEntries(defs.map(d => [d.name, loadWidth(d.name, d, storage, dimension)])))

  const visibility = () => defs.map(d => (d.visible ? !!d.visible() : true))

  const effective = computed(() => {
    const vis = visibility()
    return fitPanels(
      defs.map((d, i) => ({ width: preferred[d.name], min: d.min, visible: vis[i] })),
      viewport.value,
      centerMin
    )
  })

  function maxFor(index) {
    return availableMax(index, effective.value, defs[index], viewport.value, centerMin)
  }

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

  function setWidth(def, width, persist) {
    preferred[def.name] = width
    if (persist) saveWidth(def.name, width, storage, dimension)
  }

  function startDrag(index, e) {
    if (e.button !== 0) return
    const def = defs[index]
    const handle = e.currentTarget
    const pointerId = e.pointerId
    e.preventDefault()
    try {
      handle.setPointerCapture(pointerId)
    } catch {
      // Capture is best effort (e.g. synthetic events).
    }

    const coord = ev => (axis === 'y' ? ev.clientY : ev.clientX)
    const startX = coord(e)
    const startWidth = effective.value[index]
    const max = maxFor(index)
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
      setWidth(def, clamp(Math.round(dragWidth(startWidth, startX, lastX, def.side)), def.min, max), false)
    }
    const onMove = ev => {
      if (ev.pointerId !== pointerId) return
      lastX = coord(ev)
      if (!frame) frame = requestAnimationFrame(apply)
    }
    const onEnd = ev => {
      if (ended || ev.pointerId !== pointerId) return
      ended = true
      if (frame) cancelAnimationFrame(frame)
      if (ev.type === 'pointerup') lastX = coord(ev)
      apply()
      saveWidth(def.name, preferred[def.name], storage, dimension)
      dragging.value = null
      body.style.userSelect = prevUserSelect
      body.style.cursor = prevCursor
      handle.removeEventListener('pointermove', onMove)
      handle.removeEventListener('pointerup', onEnd)
      handle.removeEventListener('pointercancel', onEnd)
      handle.removeEventListener('lostpointercapture', onEnd)
      try {
        if (handle.hasPointerCapture?.(pointerId)) handle.releasePointerCapture(pointerId)
      } catch {
        // already released
      }
    }
    handle.addEventListener('pointermove', onMove)
    handle.addEventListener('pointerup', onEnd)
    handle.addEventListener('pointercancel', onEnd)
    handle.addEventListener('lostpointercapture', onEnd)
  }

  function onKeydown(index, e) {
    const def = defs[index]
    let next
    if (e.key === 'Home') next = def.min
    else if (e.key === 'End') next = maxFor(index)
    else {
      const delta = keyboardDelta(e.key, e.shiftKey, def.side)
      if (!delta) return
      next = effective.value[index] + delta
    }
    e.preventDefault()
    setWidth(def, clamp(next, def.min, maxFor(index)), true)
  }

  function reset(index) {
    const def = defs[index]
    setWidth(def, def.defaultWidth, true)
  }

  return Object.fromEntries(defs.map((def, index) => [def.name, reactive({
    name: def.name,
    side: def.side,
    axis,
    min: def.min,
    defaultWidth: def.defaultWidth,
    width: computed(() => effective.value[index]),
    maxNow: computed(() => maxFor(index)),
    dragging: computed(() => dragging.value === def.name),
    startDrag: e => startDrag(index, e),
    onKeydown: e => onKeydown(index, e),
    reset: () => reset(index)
  })]))
}
