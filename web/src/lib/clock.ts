// A shared one-second clock for the voice timers, corrected to the server's
// time so everyone sees the same durations whatever their own clock says.
import { ref } from 'vue'

/** Milliseconds to add to Date.now() to get the server's time. */
export const serverOffset = ref(0)
/** Current server time in ms, updated every second. */
export const now = ref(Date.now())

function tick() {
  now.value = Date.now() + serverOffset.value
}
if (typeof window !== 'undefined') setInterval(tick, 1000)

/** Aligns the clock to a server timestamp (ISO string) received just now. */
export function syncServerClock(serverNow: string): void {
  const t = Date.parse(serverNow)
  if (!Number.isFinite(t)) return
  serverOffset.value = t - Date.now()
  tick()
}

/** Whole seconds from an ISO timestamp until at (ms); 0 when unknown. */
export function secondsSince(iso: string | null | undefined, at = now.value): number {
  const t = Date.parse(iso || '')
  if (!Number.isFinite(t)) return 0
  return Math.max(0, Math.floor((at - t) / 1000))
}

/** A running timer: "4:05", "1:02:03". */
export function formatClock(seconds: number | null | undefined): string {
  const s = Math.max(0, Math.floor(seconds || 0))
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  const sec = String(s % 60).padStart(2, '0')
  return h ? `${h}:${String(m).padStart(2, '0')}:${sec}` : `${m}:${sec}`
}

/** A total: { key, params } for $t, e.g. "12 h 4 min", "45 min", "< 1 min". */
export function totalParts(seconds: number | null | undefined): { key: string; params: Record<string, number> } {
  const s = Math.max(0, Math.floor(seconds || 0))
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  if (h) return { key: 'activity.hoursMinutes', params: { h, m } }
  if (m) return { key: 'activity.minutes', params: { m } }
  return { key: 'activity.underMinute', params: {} }
}
