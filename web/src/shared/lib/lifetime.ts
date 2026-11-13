// Messages with a lifetime (doc/timed-messages.md): the presets of the
// composer's timer, how a lifetime and the time left are written, and the
// schedule that hides a message on this client's clock at its end.
import { t } from '../../i18n'

export const MIN_LIFETIME_SECONDS = 60
export const MAX_LIFETIME_SECONDS = 30 * 24 * 3600

/** The composer's presets, in seconds: 1 minute, 5 minutes, 1 hour, 8 hours, 1 day, 1 week. */
export const LIFETIME_PRESETS = [60, 300, 3600, 8 * 3600, 24 * 3600, 7 * 24 * 3600] as const

export type LifetimeUnit = 'minutes' | 'hours' | 'days'
export const UNIT_SECONDS: Readonly<Record<LifetimeUnit, number>> = { minutes: 60, hours: 3600, days: 24 * 3600 }

/** A lifetime the server takes: whole seconds within the bounds and the community's ceiling. */
export function validLifetime(seconds: number, max = MAX_LIFETIME_SECONDS): boolean {
  return Number.isInteger(seconds) && seconds >= MIN_LIFETIME_SECONDS && seconds <= Math.min(max, MAX_LIFETIME_SECONDS)
}

/** A lifetime in its largest whole unit, for the chip and the menu ("5 min", "8 h", "1 week"). */
export function lifetimeLabel(seconds: number): string {
  if (seconds >= 7 * 24 * 3600 && seconds % (7 * 24 * 3600) === 0) return t('lifetime.weeks', { n: seconds / (7 * 24 * 3600) })
  if (seconds >= 24 * 3600 && seconds % (24 * 3600) === 0) return t('lifetime.days', { n: seconds / (24 * 3600) })
  if (seconds >= 3600 && seconds % 3600 === 0) return t('lifetime.hours', { n: seconds / 3600 })
  return t('lifetime.minutes', { n: Math.max(1, Math.round(seconds / 60)) })
}

/** What is left of a lifetime, rounded up to its unit: "45 s", "5 min", "3 h", "2 d". */
export function remainingLabel(ms: number): string {
  const s = Math.max(0, Math.ceil(ms / 1000))
  if (s < 60) return t('lifetime.leftSeconds', { n: s })
  if (s < 3600) return t('lifetime.leftMinutes', { n: Math.ceil(s / 60) })
  if (s < 24 * 3600) return t('lifetime.leftHours', { n: Math.ceil(s / 3600) })
  return t('lifetime.leftDays', { n: Math.ceil(s / (24 * 3600)) })
}

/** How often a remaining-time mark needs to change: every second under a minute, every minute under an hour, and so on. */
export function remainingStep(ms: number): number {
  if (ms < 60_000) return 1000
  if (ms < 3_600_000) return 60_000
  return 3_600_000
}

/**
 * The ends this client waits for: message id to the end in ms (server time).
 * `due` takes out every entry whose end is not after now and answers them;
 * `next` is the earliest end left, or null.
 */
export class EndSchedule<T> {
  private ends = new Map<string, { at: number; value: T }>()

  /** Adds or replaces the end of id. An end that is not a number is ignored. */
  set(id: string, at: number, value: T): void {
    if (Number.isFinite(at)) this.ends.set(id, { at, value })
  }

  delete(id: string): void {
    this.ends.delete(id)
  }

  get size(): number {
    return this.ends.size
  }

  due(now: number): T[] {
    const out: T[] = []
    for (const [id, e] of this.ends) {
      if (e.at <= now) {
        out.push(e.value)
        this.ends.delete(id)
      }
    }
    return out
  }

  next(): number | null {
    let min: number | null = null
    for (const e of this.ends.values()) if (min === null || e.at < min) min = e.at
    return min
  }

  clear(): void {
    this.ends.clear()
  }
}
