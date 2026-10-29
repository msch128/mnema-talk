// Helpers for the admin Bots tab: intents, risky roles, relative time and the
// curl example. The server stays the authority on every rule.
import { ADMIN_AREA_KEYS, PERMISSION_BITS } from './permissions'
import { initialOf } from './initial'

/** Gateway intents a bot may be allowed to ask for (Discord bit numbers). */
export const BOT_INTENTS = [
  { key: 'guilds', bit: 1 },
  { key: 'members', bit: 2 },
  { key: 'messages', bit: 512 },
  { key: 'reactions', bit: 1024 }
] as const

export function hasIntent(set: number, bit: number): boolean {
  return (set & bit) === bit
}

export function toggleIntent(set: number, bit: number, on: boolean): number {
  return on ? set | bit : set & ~bit
}

const POWER_BITS = ADMIN_AREA_KEYS.reduce<number>((sum, key) => sum | PERMISSION_BITS[key], PERMISSION_BITS.administrator)

/** True when a role gives administrator power or opens a part of the admin area. */
export function isPowerfulRole(permissions: number): boolean {
  return (permissions & POWER_BITS) !== 0
}

/** "3 minutes ago" style text; '' for a missing or unparsable time. */
export function relativeTime(iso: string | undefined, locale: string, now = Date.now()): string {
  if (!iso) return ''
  const then = Date.parse(iso)
  if (!Number.isFinite(then)) return ''
  const seconds = Math.round((then - now) / 1000)
  const rtf = new Intl.RelativeTimeFormat(locale, { numeric: 'auto' })
  const steps: Array<[Intl.RelativeTimeFormatUnit, number]> = [['day', 86400], ['hour', 3600], ['minute', 60]]
  for (const [unit, size] of steps) {
    if (Math.abs(seconds) >= size) return rtf.format(Math.round(seconds / size), unit)
  }
  return rtf.format(seconds, 'second')
}

/** Short curl call that proves a token works; the secret is a placeholder. */
export function curlExample(origin: string): string {
  return `curl -H "Authorization: Bot <token>" ${origin}/api/bot/v1/users/@me`
}

/** Initial for a bot without an avatar. */
export function botInitial(name: string): string {
  return initialOf(name)
}

/** End of the chosen day (local) as RFC3339 UTC, for a token expiry. */
export function endOfDayIso(date: string): string | undefined {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(date)) return undefined
  const [y, m, d] = date.split('-').map(Number) as [number, number, number]
  return new Date(y, m - 1, d, 23, 59, 59).toISOString().replace(/\.\d+Z$/, 'Z')
}
