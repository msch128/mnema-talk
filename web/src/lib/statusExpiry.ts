// "Clear after" choices of a custom status. The absolute time comes from the
// member's own clock and time zone; the server only stores and enforces it.

export type ExpiryChoice = 'never' | 'hour' | 'tonight' | 'tomorrow' | 'custom'

export const EXPIRY_CHOICES: readonly ExpiryChoice[] = ['hour', 'tonight', 'tomorrow', 'never', 'custom']

/** Local wall-clock time of today's end (23:59) and of tomorrow morning (08:00). */
const TONIGHT = { hour: 23, minute: 59 }
const MORNING = { hour: 8, minute: 0 }

/**
 * The moment a choice stands for, or null for "never". `custom` is the value
 * of a datetime-local input ("2026-10-24T15:30", local time); an empty or
 * unparsable one gives an invalid Date, which the caller must reject.
 */
export function expiryTime(choice: ExpiryChoice, now: Date, custom = ''): Date | null {
  switch (choice) {
    case 'never':
      return null
    case 'hour':
      return new Date(now.getTime() + 60 * 60 * 1000)
    case 'tonight':
      return new Date(now.getFullYear(), now.getMonth(), now.getDate(), TONIGHT.hour, TONIGHT.minute, 0, 0)
    case 'tomorrow':
      return new Date(now.getFullYear(), now.getMonth(), now.getDate() + 1, MORNING.hour, MORNING.minute, 0, 0)
    case 'custom':
      return new Date(custom)
  }
}

/** True for a usable moment still ahead of `now`. */
export function isFuture(date: Date | null, now: Date): boolean {
  return date !== null && !Number.isNaN(date.getTime()) && date.getTime() > now.getTime()
}

/** Value for a datetime-local input: `date` in local time, minutes only. */
export function toLocalInput(date: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`
}
