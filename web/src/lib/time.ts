// One place for how a time of day is written. A 12-hour clock gets
// `hour: 'numeric'`: no leading zero on the hour ("1:13 PM", like most chat
// apps; '2-digit' would force "01:13 PM"). A 24-hour locale keeps two digits
// ("09:05", "13:13"), as before.
export function clockTimeOptions(locale?: string): Intl.DateTimeFormatOptions {
  let twelveHour = false
  try {
    twelveHour = new Intl.DateTimeFormat(locale || undefined, { hour: 'numeric' }).resolvedOptions().hour12 === true
  } catch {
    // Unknown locale tag: fall back to the 24-hour form.
  }
  return { hour: twelveHour ? 'numeric' : '2-digit', minute: '2-digit' }
}

// The shared ways to write a moment. Every popover, card and list uses these,
// so the same instant reads the same everywhere: "12:19", "Yesterday",
// "Fri", "24 Oct", "Yesterday, 12:19".
type Moment = string | number | Date

const DAY_MS = 86_400_000

function asDate(input: Moment): Date {
  return input instanceof Date ? input : new Date(input)
}

function startOfDay(d: Date): number {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime()
}

function capitalise(text: string, locale?: string): string {
  return text ? text.charAt(0).toLocaleUpperCase(locale || undefined) + text.slice(1) : text
}

/** A usable date, or null for a missing or invalid value. */
function parse(input: Moment | null | undefined): Date | null {
  if (input === null || input === undefined || input === '') return null
  const d = asDate(input)
  return Number.isNaN(d.getTime()) ? null : d
}

/** Time of day only ("12:19", "1:19 PM"). Empty for a missing or invalid value. */
export function timeShort(input: Moment | null | undefined, locale?: string): string {
  const d = parse(input)
  return d ? d.toLocaleTimeString(locale || undefined, clockTimeOptions(locale)) : ''
}

/**
 * The day on its own: today and its neighbours by name, the rest of the week by
 * weekday, anything further by date (with the year when it is not this one).
 */
export function dayLabel(input: Moment | null | undefined, locale?: string, now: Date = new Date()): string {
  const d = parse(input)
  if (!d) return ''
  const apart = Math.round((startOfDay(now) - startOfDay(d)) / DAY_MS)
  if (Math.abs(apart) <= 1) {
    const words = new Intl.RelativeTimeFormat(locale || undefined, { numeric: 'auto' })
    return capitalise(words.format(-apart, 'day'), locale)
  }
  if (Math.abs(apart) < 7) return d.toLocaleDateString(locale || undefined, { weekday: 'short' })
  const sameYear = d.getFullYear() === now.getFullYear()
  return d.toLocaleDateString(locale || undefined, sameYear ? { day: 'numeric', month: 'short' } : { day: 'numeric', month: 'short', year: 'numeric' })
}

/** Day and time together ("Yesterday, 12:19"). */
export function dateTime(input: Moment | null | undefined, locale?: string, now: Date = new Date()): string {
  const day = dayLabel(input, locale, now)
  return day ? `${day}, ${timeShort(input, locale)}` : ''
}

const RELATIVE_STEPS: [Intl.RelativeTimeFormatUnit, number][] = [['day', DAY_MS], ['hour', 3_600_000], ['minute', 60_000]]

/** How long ago (or from now) in the largest whole unit ("5 min. ago", "in 2 hr."). */
export function relative(input: Moment | null | undefined, locale?: string, now: Date = new Date()): string {
  const d = parse(input)
  if (!d) return ''
  const words = new Intl.RelativeTimeFormat(locale || undefined, { numeric: 'auto', style: 'short' })
  const diff = d.getTime() - now.getTime()
  for (const [unit, size] of RELATIVE_STEPS) {
    if (Math.abs(diff) >= size) return words.format(Math.trunc(diff / size), unit)
  }
  return words.format(0, 'second')
}
