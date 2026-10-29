// Zoom and pan for one picture inside a stage: wheel and double-click zoom
// toward the pointer, buttons and keys zoom around the middle, and a zoomed
// picture can be dragged. Pointer events only, no dependency.
import { computed, ref } from 'vue'
import type { Ref } from 'vue'

export const MIN_SCALE = 1
export const MAX_SCALE = 8
const STEP = 1.5
const DOUBLE_CLICK_SCALE = 2.5

export function useZoomPan(stage: Ref<HTMLElement | null>) {
  const scale = ref(1)
  const x = ref(0)
  const y = ref(0)
  const dragging = ref(false)
  // True right after a drag, so the click that ends it is not taken as a backdrop click.
  let moved = false
  let start: { px: number; py: number; x: number; y: number; id: number } | null = null

  const zoomed = computed(() => scale.value > MIN_SCALE)
  const style = computed(() => ({ transform: `translate3d(${x.value}px, ${y.value}px, 0) scale(${scale.value})` }))

  function reset() {
    scale.value = 1
    x.value = 0
    y.value = 0
    dragging.value = false
    start = null
  }

  // Zoom to `next`, keeping the point under (cx, cy), measured from the stage middle, in place.
  function zoomTo(next: number, cx = 0, cy = 0) {
    const clamped = Math.min(MAX_SCALE, Math.max(MIN_SCALE, next))
    if (clamped === MIN_SCALE) return reset()
    const ratio = clamped / scale.value
    x.value = cx - (cx - x.value) * ratio
    y.value = cy - (cy - y.value) * ratio
    scale.value = clamped
  }

  function fromMiddle(e: { clientX: number; clientY: number }) {
    const r = stage.value?.getBoundingClientRect()
    if (!r) return { cx: 0, cy: 0 }
    return { cx: e.clientX - r.left - r.width / 2, cy: e.clientY - r.top - r.height / 2 }
  }

  const zoomIn = () => zoomTo(scale.value * STEP)
  const zoomOut = () => zoomTo(scale.value / STEP)

  function onWheel(e: WheelEvent) {
    const { cx, cy } = fromMiddle(e)
    zoomTo(scale.value * Math.exp(-e.deltaY * 0.0015), cx, cy)
  }

  function onDoubleClick(e: MouseEvent) {
    if (zoomed.value) return reset()
    const { cx, cy } = fromMiddle(e)
    zoomTo(DOUBLE_CLICK_SCALE, cx, cy)
  }

  function onPointerDown(e: PointerEvent) {
    moved = false
    if (!zoomed.value || e.button !== 0 || (e.target as HTMLElement).closest('button, video')) return
    start = { px: e.clientX, py: e.clientY, x: x.value, y: y.value, id: e.pointerId }
    dragging.value = true
    ;stage.value?.setPointerCapture?.(e.pointerId)
  }

  function onPointerMove(e: PointerEvent) {
    if (!start || e.pointerId !== start.id) return
    const dx = e.clientX - start.px
    const dy = e.clientY - start.py
    if (Math.abs(dx) + Math.abs(dy) > 3) moved = true
    x.value = start.x + dx
    y.value = start.y + dy
  }

  function onPointerEnd(e: PointerEvent) {
    if (!start || e.pointerId !== start.id) return
    start = null
    dragging.value = false
  }

  /** Was the pointer dragged since it went down? Read by the click that follows. */
  const wasDragged = () => moved

  return { scale, zoomed, style, dragging, reset, zoomIn, zoomOut, onWheel, onDoubleClick, onPointerDown, onPointerMove, onPointerEnd, wasDragged }
}
