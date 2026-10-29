// Guards for the microphone pipeline: a graph that never renders must not leave
// a member silent. Firefox without an audio output device (and any browser that
// holds an AudioContext back until a user gesture) leaves the context
// "suspended" for good, so nothing flowing through it ever reaches the call.

/** How long the context may take to start rendering before the pipeline stops relying on it. */
export const CONTEXT_START_MS = 3000
/** How long a running filter may take to deliver its first processed frame. */
export const FILTER_FRAMES_MS = 3000
const POLL_MS = 100

export type StallReason = 'context-not-running' | 'no-frames'

let unloading = false
if (typeof window !== 'undefined') {
  window.addEventListener('pagehide', event => { unloading = !event.persisted })
  window.addEventListener('pageshow', () => { unloading = false })
}

/** True while the page is being left (reload, navigation): workers and worklets are then cancelled by the browser, which is no failure. */
export function isPageUnloading(): boolean {
  return unloading
}

/** Runs the callback when the page is left for good. Returns the unsubscribe function. */
export function onPageLeave(callback: () => void): () => void {
  const handler = (event: PageTransitionEvent) => { if (!event.persisted) callback() }
  window.addEventListener('pagehide', handler)
  return () => window.removeEventListener('pagehide', handler)
}

function whenRunning(ctx: AudioContext, signal: AbortSignal | undefined, run: () => void): () => void {
  if (ctx.state === 'running') {
    run()
    return () => {}
  }
  const onChange = () => {
    if (ctx.state !== 'running') return
    ctx.removeEventListener('statechange', onChange)
    run()
  }
  ctx.addEventListener('statechange', onChange)
  const stop = () => ctx.removeEventListener('statechange', onChange)
  signal?.addEventListener('abort', stop, { once: true })
  return stop
}

/**
 * Calls onStall once when the context has not started rendering after timeoutMs.
 * Returns the function that ends the watch (also ended by the signal).
 */
export function watchContextStart(ctx: AudioContext, signal: AbortSignal | undefined, onStall: (reason: StallReason) => void, timeoutMs = CONTEXT_START_MS): () => void {
  if (ctx.state === 'running') return () => {}
  let stopped = false
  const stop = () => {
    if (stopped) return
    stopped = true
    clearTimeout(timer)
    ctx.removeEventListener('statechange', onChange)
    signal?.removeEventListener('abort', stop)
  }
  const onChange = () => { if (ctx.state === 'running') stop() }
  const timer = setTimeout(() => {
    if (stopped) return
    stop()
    if (ctx.state !== 'running' && ctx.state !== 'closed' && !signal?.aborted && !unloading) onStall('context-not-running')
  }, timeoutMs)
  ctx.addEventListener('statechange', onChange)
  signal?.addEventListener('abort', stop, { once: true })
  return stop
}

/**
 * Calls onStall once when a filter node has processed no frame within timeoutMs
 * of the context running. A context that does not run is the context watch's
 * business, not the filter's.
 */
export function watchFilterFrames(
  ctx: AudioContext,
  node: { filterStats?: { processedFrames: number } },
  signal: AbortSignal | undefined,
  onStall: (reason: StallReason) => void,
  timeoutMs = FILTER_FRAMES_MS
): () => void {
  let poll: ReturnType<typeof setInterval> | undefined
  let stopped = false
  const stop = () => {
    if (stopped) return
    stopped = true
    clearInterval(poll)
    stopWaiting()
    signal?.removeEventListener('abort', stop)
  }
  const stopWaiting = whenRunning(ctx, signal, () => {
    if (stopped) return
    const startedAt = Date.now()
    poll = setInterval(() => {
      if ((node.filterStats?.processedFrames ?? 0) > 0) { stop(); return }
      if (Date.now() - startedAt < timeoutMs) return
      stop()
      if (ctx.state !== 'closed' && !signal?.aborted && !unloading) onStall('no-frames')
    }, POLL_MS)
  })
  signal?.addEventListener('abort', stop, { once: true })
  return stop
}
