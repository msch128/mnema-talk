// The times offered by "Remind me" and "Send later". They are worked out here,
// in the member's own time zone and clock, and sent to the server as absolute
// times: the server never needs to know the time zone.

export type LaterPreset = 'in20m' | 'in30m' | 'in1h' | 'in3h' | 'tomorrow'

/** What the server accepts: at least 10 seconds ahead, at most a year. */
export const MIN_LEAD_MS = 10_000
export const MAX_LEAD_DAYS = 366

const MINUTE = 60_000

/** Tomorrow at 08:00 on the local clock (also right across a change of summer time). */
export function tomorrowMorning(now: Date): Date {
  return new Date(now.getFullYear(), now.getMonth(), now.getDate() + 1, 8, 0, 0, 0)
}

/** The moment a preset stands for, counted from now. */
export function presetTime(preset: LaterPreset, now: Date = new Date()): Date {
  switch (preset) {
    case 'in20m': return new Date(now.getTime() + 20 * MINUTE)
    case 'in30m': return new Date(now.getTime() + 30 * MINUTE)
    case 'in1h': return new Date(now.getTime() + 60 * MINUTE)
    case 'in3h': return new Date(now.getTime() + 180 * MINUTE)
    case 'tomorrow': return tomorrowMorning(now)
  }
}

function pad(n: number): string {
  return String(n).padStart(2, '0')
}

/** The value of a datetime-local input (local time, minute precision). */
export function toLocalInputValue(date: Date): string {
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`
}

/** Reads a datetime-local value as local time; null when it is not a date. */
export function fromLocalInputValue(value: string): Date | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})$/.exec(value)
  if (!m) return null
  const [, y, mo, d, h, mi] = m.map(Number)
  const date = new Date(y!, mo! - 1, d!, h!, mi!, 0, 0)
  // new Date() rolls 31 February over to March; refuse such a value.
  return date.getFullYear() === y && date.getMonth() === mo! - 1 && date.getDate() === d ? date : null
}

/** Whether a chosen time can be sent to the server now. */
export function isAcceptableTime(date: Date, now: Date = new Date()): boolean {
  const ahead = date.getTime() - now.getTime()
  return ahead >= MIN_LEAD_MS && ahead <= MAX_LEAD_DAYS * 24 * 60 * MINUTE
}

/** The earliest value a custom picker offers: the next full minute. */
export function earliestInputValue(now: Date = new Date()): string {
  return toLocalInputValue(new Date(now.getTime() + 2 * MINUTE))
}
