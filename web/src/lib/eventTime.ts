// How an event's start is written: the weekday and the date with the time,
// and a short relative hint next to it ("today", "tomorrow", "in 3 days").
import { clockTimeOptions } from './time'

const DAY_MS = 86_400_000

function startOfDay(ms: number): number {
  const d = new Date(ms)
  d.setHours(0, 0, 0, 0)
  return d.getTime()
}

/** "Sat, 1 Nov, 18:00" in the member's language. */
export function formatEventStart(iso: string, locale: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  return d.toLocaleString([locale], { weekday: 'short', day: 'numeric', month: 'short', ...clockTimeOptions(locale) })
}

/** "today", "tomorrow", "in 3 days", "2 days ago": whole calendar days from now, so a late evening event is not "in 0 days". */
export function relativeEventDay(iso: string, now: number, locale: string): string {
  const at = new Date(iso).getTime()
  if (Number.isNaN(at)) return ''
  const days = Math.round((startOfDay(at) - startOfDay(now)) / DAY_MS)
  return new Intl.RelativeTimeFormat([locale], { numeric: 'auto' }).format(days, 'day')
}
