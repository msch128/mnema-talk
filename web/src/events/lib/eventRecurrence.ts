// What a repeating event offers and says: the frequencies, the zone the server
// keeps the wall-clock time in, and the label of an occurrence.
import type { CommunityEvent } from '../../types/domain'

export const FREQUENCIES = ['weekly', 'biweekly', 'monthly'] as const
export type Frequency = typeof FREQUENCIES[number]
/** What an edit, a cancellation or a role list applies to: one occurrence, or it and the later ones. */
export type Scope = 'this' | 'following'

/** The zone of this browser, which the series keeps its wall-clock time in. */
export function browserZone(): string | undefined {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || undefined
  } catch {
    return undefined
  }
}

/** The end of the chosen day (a yyyy-mm-dd from a date input) in this browser's zone, as an ISO time; empty is none. */
export function untilIso(day: string): string | null {
  if (!day) return null
  const end = new Date(`${day}T23:59:59`)
  return Number.isNaN(end.getTime()) ? null : end.toISOString()
}

/** The i18n key that says how often the occurrence's series repeats, or null for a single event. */
export function repeatKey(event: Pick<CommunityEvent, 'series'>): string | null {
  const series = event.series
  return series ? `events.repeat.${series.frequency}` : null
}
