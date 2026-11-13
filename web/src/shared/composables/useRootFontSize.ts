import { ref, type Ref } from 'vue'

// The root font size in px, kept current: panel widths given in rem (the
// channel list) follow it, so a reader with a larger font gets a wider
// column instead of the same pixels with bigger letters.

const FALLBACK = 16
const rootPx = ref(FALLBACK)
let started = false

function read(): number {
  const px = parseFloat(getComputedStyle(document.documentElement).fontSize)
  return Number.isFinite(px) && px > 0 ? px : FALLBACK
}

function start() {
  if (started || typeof document === 'undefined' || typeof window === 'undefined') return
  started = true
  const update = () => { rootPx.value = read() }
  update()
  // The size changes with zoom and settings (a resize) or with the root's own style.
  window.addEventListener('resize', update)
  if (typeof MutationObserver !== 'undefined') {
    new MutationObserver(update).observe(document.documentElement, { attributes: true, attributeFilter: ['style', 'class'] })
  }
}

export function useRootFontSize(): Ref<number> {
  start()
  return rootPx
}

/** A size in rem as px at the given root font size, whole pixels. */
export function remToPx(rem: number, root: number): number {
  return Math.round(rem * root)
}
