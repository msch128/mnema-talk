// Remembers the voice channel across page reloads: while in a
// call a heartbeat refreshes a timestamp; after a reload the client rejoins if
// that heartbeat is younger than RESUME_WINDOW_MS. Leaving on purpose clears it.
// The server keeps the user listed for the same window (ws.DefaultVoiceGrace).

export const STORAGE_KEY = 'mnema.voice.session'
export const RESUME_WINDOW_MS = 30_000
const HEARTBEAT_MS = 5_000

function storage() {
  try {
    return window.localStorage
  } catch {
    return null
  }
}

export function save(channelId: string, now = Date.now()) {
  try {
    storage()?.setItem(STORAGE_KEY, JSON.stringify({ channelId, ts: now }))
  } catch {
    // Storage full or blocked: resuming simply won't happen.
  }
}

export function clear() {
  try {
    storage()?.removeItem(STORAGE_KEY)
  } catch {
    // ignore
  }
}

/** Returns the channel to resume, or null if none or too old. Stale entries are removed. */
export function recent(now = Date.now(), windowMs = RESUME_WINDOW_MS) {
  let entry: unknown
  try {
    entry = JSON.parse(storage()?.getItem(STORAGE_KEY) || 'null')
  } catch {
    entry = null
  }
  if (!entry || typeof entry !== 'object' || !('channelId' in entry) || !('ts' in entry) || typeof entry.channelId !== 'string' || typeof entry.ts !== 'number') {
    clear()
    return null
  }
  const age = now - entry.ts
  if (age < 0 || age > windowMs) {
    clear()
    return null
  }
  return entry.channelId
}

let heartbeat: ReturnType<typeof setInterval> | null = null
let onPageHide: (() => void) | null = null

/** Starts remembering channelId until stop() or clear(). */
export function track(channelId: string) {
  untrack()
  save(channelId)
  heartbeat = setInterval(() => save(channelId), HEARTBEAT_MS)
  // Stamp the exact moment the page goes away, so the window starts there.
  onPageHide = () => save(channelId)
  window.addEventListener('pagehide', onPageHide)
}

/** Stops the heartbeat but keeps the last timestamp (e.g. connection loss). */
export function untrack() {
  if (heartbeat) clearInterval(heartbeat)
  heartbeat = null
  if (onPageHide) window.removeEventListener('pagehide', onPageHide)
  onPageHide = null
}

/** Explicit leave: forget the session entirely. */
export function forget() {
  untrack()
  clear()
}
