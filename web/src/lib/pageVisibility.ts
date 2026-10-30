// One place that knows whether anyone can see the page. A hidden tab (a locked
// phone, a minimised window, another tab) has no use for counters, polling or
// animation, and every wake-up costs battery; visibleInterval is the setInterval
// that sleeps meanwhile and catches up when the page is shown again.
import { ref } from 'vue'

function hiddenNow(): boolean {
  return typeof document !== 'undefined' && document.visibilityState === 'hidden'
}

/** True while the page cannot be seen (hidden tab, or frozen by the browser). */
export const pageHidden = ref(hiddenNow())

const listeners = new Set<(hidden: boolean) => void>()

function set(hidden: boolean) {
  if (pageHidden.value === hidden) return
  pageHidden.value = hidden
  for (const listener of [...listeners]) listener(hidden)
}

if (typeof document !== 'undefined') {
  document.addEventListener('visibilitychange', () => set(hiddenNow()))
  // Page Lifecycle: a frozen page runs no timers at all, and resume says it is back.
  document.addEventListener('freeze', () => set(true))
  document.addEventListener('resume', () => set(hiddenNow()))
}
if (typeof window !== 'undefined') {
  window.addEventListener('pagehide', () => set(true))
  window.addEventListener('pageshow', () => set(hiddenNow()))
}

/** Calls fn whenever the page becomes hidden (true) or visible (false); returns the unsubscribe. */
export function onVisibilityChange(fn: (hidden: boolean) => void): () => void {
  listeners.add(fn)
  return () => { listeners.delete(fn) }
}

export interface VisibleIntervalOptions {
  /** Run fn once as soon as the page is shown again after a pause (default true). */
  catchUp?: boolean
  /** While hidden, keep running at this slower period instead of pausing. */
  hiddenMs?: number
}

/**
 * setInterval that does not run while the page is hidden. Returns the stop
 * function. With hiddenMs the interval slows down instead of pausing.
 */
export function visibleInterval(fn: () => void, ms: number, options: VisibleIntervalOptions = {}): () => void {
  const { catchUp = true, hiddenMs } = options
  let timer: ReturnType<typeof setInterval> | undefined
  let paused = false
  const arm = (period: number) => {
    clearInterval(timer)
    timer = setInterval(fn, period)
  }
  const apply = (hidden: boolean) => {
    if (hidden) {
      if (hiddenMs === undefined) {
        clearInterval(timer)
        timer = undefined
        paused = true
      } else {
        arm(hiddenMs)
      }
      return
    }
    const wasPaused = paused
    paused = false
    arm(ms)
    if (wasPaused && catchUp) fn()
  }
  const unsubscribe = onVisibilityChange(apply)
  if (pageHidden.value) apply(true)
  else arm(ms)
  return () => {
    unsubscribe()
    clearInterval(timer)
    timer = undefined
  }
}

function pipActive(): boolean {
  return typeof document !== 'undefined' && !!document.pictureInPictureElement
}

/**
 * True when nobody can see video: the page is hidden and no Picture-in-Picture
 * window is showing it (PiP is used exactly when the tab is in the background).
 */
export function videoUnseen(): boolean {
  return pageHidden.value && !pipActive()
}

/** Calls fn when videoUnseen() changes: on hiding or showing the page and on entering or leaving Picture-in-Picture. */
export function onVideoSeenChange(fn: (unseen: boolean) => void): () => void {
  let last = videoUnseen()
  const check = () => {
    const next = videoUnseen()
    if (next === last) return
    last = next
    fn(next)
  }
  const stop = onVisibilityChange(check)
  // No document (a store under test, server-side): nothing to watch beyond the page state.
  if (typeof document === 'undefined') return stop
  // These bubble from the video element; the page state is already updated by then.
  document.addEventListener('enterpictureinpicture', check, true)
  document.addEventListener('leavepictureinpicture', check, true)
  return () => {
    stop()
    document.removeEventListener('enterpictureinpicture', check, true)
    document.removeEventListener('leavepictureinpicture', check, true)
  }
}
