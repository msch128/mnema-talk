import { reactive, ref, computed, onScopeDispose } from 'vue'

// Resizable side panels.
//
// Each panel keeps a *preferred* width (what the user dragged to, persisted in
// localStorage) and an *effective* width (preferred width squeezed so the
// center column never drops below its minimum on narrow windows). Shrinking
// the window therefore never overwrites the user's choice: widening it again
// restores the panels.

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

export function storageKey(name) {
  return `${STORAGE_PREFIX}${name}.width`
}

export function loadWidth(name, config, storage = defaultStorage()) {
  try {
    return sanitizeWidth(storage ? storage.getItem(storageKey(name)) : null, config)
  } catch {
    return config.defaultWidth
  }
}

export function saveWidth(name, width, storage = defaultStorage()) {
  try {
    if (!storage) return false
    storage.setItem(storageKey(name), String(Math.round(width)))
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

// A left panel grows when the pointer moves right, a right panel when it moves left.
export function dragWidth(startWidth, startX, currentX, side) {
  const dx = currentX - startX
  return startWidth + (side === 'left' ? dx : -dx)
}

// Arrow keys move the separator; returns the width change for the panel.
export function keyboardDelta(key, shiftKey, side) {
  const step = shiftKey ? KEY_STEP_LARGE : KEY_STEP
  if (key === 'ArrowRight') return side === 'left' ? step : -step
  if (key === 'ArrowLeft') return side === 'left' ? -step : step
  return 0
}

function currentViewportWidth() {
  return typeof window !== 'undefined' ? window.innerWidth : 1280
}

/**
 * @param {Array<{name: string, side: 'left'|'right', defaultWidth: number, min: number, max: number, visible?: () => boolean}>} defs
 *   Panels in shrink-priority order (the last one is squeezed first).
 * @param {{ centerMin?: number, storage?: Storage|null }} options
 * @returns {Record<string, object>} one reactive handle state per panel name
 */
export function useResizable(defs, { centerMin = 400, storage = defaultStorage() } = {}) {
  const viewport = ref(currentViewportWidth())
  const dragging = ref(null)
  const preferred = reactive(Object.fromEntries(defs.map(d => [d.name, loadWidth(d.name, d, storage)])))

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
      viewport.value = currentViewportWidth()
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
    if (persist) saveWidth(def.name, width, storage)
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

    const startX = e.clientX
    const startWidth = effective.value[index]
    const max = maxFor(index)
    let lastX = startX
    let frame = 0
    let ended = false

    const body = document.body
    const prevUserSelect = body.style.userSelect
    const prevCursor = body.style.cursor
    body.style.userSelect = 'none'
    body.style.cursor = 'col-resize'
    dragging.value = def.name

    const apply = () => {
      frame = 0
      setWidth(def, clamp(Math.round(dragWidth(startWidth, startX, lastX, def.side)), def.min, max), false)
    }
    const onMove = ev => {
      if (ev.pointerId !== pointerId) return
      lastX = ev.clientX
      if (!frame) frame = requestAnimationFrame(apply)
    }
    const onEnd = ev => {
      if (ended || ev.pointerId !== pointerId) return
      ended = true
      if (frame) cancelAnimationFrame(frame)
      if (ev.type === 'pointerup') lastX = ev.clientX
      apply()
      saveWidth(def.name, preferred[def.name], storage)
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
