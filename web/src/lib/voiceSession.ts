// Remembers the voice channel across page reloads: while in a
// call a heartbeat refreshes a timestamp; after a reload the client rejoins if
// that heartbeat is younger than RESUME_WINDOW_MS. Leaving on purpose clears it.
// The server keeps the user listed for the same window (ws.DefaultVoiceGrace).

import { visibleInterval } from './pageVisibility'

export const STORAGE_KEY = 'mnema.voice.session'
export const RESUME_WINDOW_MS = 30_000
const HEARTBEAT_MS = 5_000
// Well inside RESUME_WINDOW_MS, and a hidden page stamps less often.
const HIDDEN_HEARTBEAT_MS = 15_000

function storage() {
  try {
    return window.localStorage
  } catch {
    return null
  }
}

function storageBlocked(): void {
  // Storage full or blocked: resuming simply won't happen.
}

export function save(channelId: string, now = Date.now()) {
  try {
    storage()?.setItem(STORAGE_KEY, JSON.stringify({ channelId, ts: now }))
  } catch {
    storageBlocked()
  }
}

export function clear() {
  try {
    storage()?.removeItem(STORAGE_KEY)
  } catch {
    storageBlocked()
  }
}

function readSession(): unknown {
  try {
    return JSON.parse(storage()?.getItem(STORAGE_KEY) || 'null')
  } catch {
    return null
  }
}

function sessionFields(entry: object): { channelId: string; ts: number } | null {
  if (!('channelId' in entry) || !('ts' in entry)) return null
  const channelId = Reflect.get(entry, 'channelId')
  const ts = Reflect.get(entry, 'ts')
  if (typeof channelId !== 'string' || typeof ts !== 'number') return null
  return { channelId, ts }
}

/** Returns the channel to resume, or null if none or too old. Stale entries are removed. */
export function recent(now = Date.now(), windowMs = RESUME_WINDOW_MS) {
  const entry = readSession()
  if (!entry || typeof entry !== 'object') {
    clear()
    return null
  }
  const fields = sessionFields(entry)
  if (!fields) {
    clear()
    return null
  }
  const age = now - fields.ts
  if (age < 0 || age > windowMs) {
    clear()
    return null
  }
  return fields.channelId
}

let stopHeartbeat: (() => void) | null = null
let onPageHide: (() => void) | null = null

/** Starts remembering channelId until stop() or clear(). */
export function track(channelId: string) {
  untrack()
  save(channelId)
  stopHeartbeat = visibleInterval(() => save(channelId), HEARTBEAT_MS, { hiddenMs: HIDDEN_HEARTBEAT_MS, catchUp: false })
  // Stamp the exact moment the page goes away, so the window starts there.
  onPageHide = () => save(channelId)
  window.addEventListener('pagehide', onPageHide)
}

/** Stops the heartbeat but keeps the last timestamp (e.g. connection loss). */
export function untrack() {
  stopHeartbeat?.()
  stopHeartbeat = null
  if (onPageHide) window.removeEventListener('pagehide', onPageHide)
  onPageHide = null
}

/** Explicit leave: forget the session entirely. */
export function forget() {
  untrack()
  clear()
}
