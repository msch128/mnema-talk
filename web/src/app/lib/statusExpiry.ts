// "Clear after" choices of a custom status. The absolute time comes from the
// member's own clock and time zone; the server only stores and enforces it.

export type ExpiryChoice = 'never' | 'min30' | 'hour' | 'hours4' | 'today' | 'week'

export const EXPIRY_CHOICES: readonly ExpiryChoice[] = ['never', 'min30', 'hour', 'hours4', 'today', 'week']

const MINUTE = 60 * 1000
/** Local wall-clock time at which "today" and "this week" end. */
const END_OF_DAY = { hour: 23, minute: 59 }

/**
 * The moment a choice stands for, or null for "never". "Today" ends at 23:59
 * and "this week" at 23:59 on the coming Sunday (the next one when it is
 * Sunday already), both on the local clock.
 */
export function expiryTime(choice: ExpiryChoice, now: Date): Date | null {
  const endOfDay = (daysAhead: number) => new Date(now.getFullYear(), now.getMonth(), now.getDate() + daysAhead, END_OF_DAY.hour, END_OF_DAY.minute, 0, 0)
  switch (choice) {
    case 'never':
      return null
    case 'min30':
      return new Date(now.getTime() + 30 * MINUTE)
    case 'hour':
      return new Date(now.getTime() + 60 * MINUTE)
    case 'hours4':
      return new Date(now.getTime() + 240 * MINUTE)
    case 'today':
      return endOfDay(0)
    case 'week': {
      const untilSunday = (7 - now.getDay()) % 7
      const sunday = endOfDay(untilSunday)
      return sunday.getTime() > now.getTime() ? sunday : endOfDay(untilSunday + 7)
    }
  }
}

/** True for a usable moment still ahead of `now`. */
export function isFuture(date: Date | null, now: Date): boolean {
  return date !== null && !Number.isNaN(date.getTime()) && date.getTime() > now.getTime()
}
