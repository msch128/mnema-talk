// A shared one-second clock for the voice timers, corrected to the server's
// time so everyone sees the same durations whatever their own clock says. It
// ticks only while a component that shows it is mounted (useServerClock) and
// the page is visible; a signed-in tab with no timer on screen wakes nothing.
import { getCurrentInstance, onBeforeUnmount, ref, watch } from 'vue'
import { visibleInterval } from './pageVisibility'

/** Milliseconds to add to Date.now() to get the server's time. */
export const serverOffset = ref(0)
/** Current server time in ms, updated every second while someone watches it. */
export const now = ref(Date.now())

function tick() {
  now.value = Date.now() + serverOffset.value
}

let watchers = 0
let stopTicking: (() => void) | null = null

/** Starts the one-second tick for as long as the returned release is not called. */
export function retainClock(): () => void {
  if (watchers++ === 0) {
    tick()
    stopTicking = visibleInterval(tick, 1000)
  }
  let released = false
  return () => {
    if (released) return
    released = true
    if (--watchers === 0) {
      stopTicking?.()
      stopTicking = null
    }
  }
}

/** For a component that shows `now`: the clock runs while it is mounted and, if given, while `needed()` holds. */
export function useServerClock(needed: () => boolean = () => true): void {
  let release: (() => void) | null = null
  const sync = (on: boolean) => {
    if (on && !release) release = retainClock()
    else if (!on && release) { release(); release = null }
  }
  if (getCurrentInstance()) {
    watch(needed, sync, { immediate: true })
    onBeforeUnmount(() => sync(false))
  } else {
    sync(needed())
  }
}

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
