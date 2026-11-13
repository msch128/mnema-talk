// Touch has no hover. On a tooltip over visible text (v-tooltip.visual, which
// carries the full text of a cut name), a long press shows it for a moment,
// and the tap that ends the press does not also trigger the element.

const HOLD_MS = 450
const SLOP_PX = 8
const SHOWN_MS = 2500

/** Wires the long press; returns what removes it again. */
export function holdToShow(el: HTMLElement, show: () => void, hide: () => void): () => void {
  let timer: ReturnType<typeof setTimeout> | undefined
  let hideTimer: ReturnType<typeof setTimeout> | undefined
  let start: { x: number; y: number } | null = null
  let held = false

  const cancel = () => { clearTimeout(timer); start = null }
  const swallowClick = (e: Event) => { e.preventDefault(); e.stopPropagation() }
  const down = (e: PointerEvent) => {
    if (e.pointerType !== 'touch') return
    start = { x: e.clientX, y: e.clientY }
    held = false
    clearTimeout(timer)
    timer = setTimeout(() => {
      held = true
      show()
      clearTimeout(hideTimer)
      hideTimer = setTimeout(hide, SHOWN_MS)
    }, HOLD_MS)
  }
  const move = (e: PointerEvent) => {
    if (start && Math.hypot(e.clientX - start.x, e.clientY - start.y) > SLOP_PX) cancel()
  }
  const up = () => {
    cancel()
    if (!held) return
    held = false
    el.addEventListener('click', swallowClick, { capture: true, once: true })
    // A click that never comes (the finger slid off) must not swallow the next real one.
    setTimeout(() => el.removeEventListener('click', swallowClick, { capture: true }), 600)
  }
  const menu = (e: Event) => { if (held) e.preventDefault() }

  el.addEventListener('pointerdown', down)
  el.addEventListener('pointermove', move)
  el.addEventListener('pointerup', up)
  el.addEventListener('pointercancel', cancel)
  el.addEventListener('contextmenu', menu)
  return () => {
    cancel()
    clearTimeout(hideTimer)
    el.removeEventListener('pointerdown', down)
    el.removeEventListener('pointermove', move)
    el.removeEventListener('pointerup', up)
    el.removeEventListener('pointercancel', cancel)
    el.removeEventListener('contextmenu', menu)
  }
}
