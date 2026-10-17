// Moderation helpers: the dialog request (a single <ModerationDialog> host in
// App.vue renders whatever is pending, like lib/confirm), the duration
// choices and the notice a member gets.
import { shallowRef } from 'vue'
import { t } from '../i18n'

export type ModerationKind = 'timeout' | 'ban'

export interface ModerationTarget { id: string; name: string }

export interface PendingModeration {
  kind: ModerationKind
  target: ModerationTarget
  resolve: (done: boolean) => void
}

export const pendingModeration = shallowRef<PendingModeration | null>(null)

/** Opens the timeout or ban dialog; resolves true once the action went through. */
export function moderate(kind: ModerationKind, target: ModerationTarget): Promise<boolean> {
  pendingModeration.value?.resolve(false)
  return new Promise<boolean>(resolve => {
    const entry: PendingModeration = {
      kind,
      target,
      resolve: done => {
        if (pendingModeration.value === entry) pendingModeration.value = null
        resolve(done)
      }
    }
    pendingModeration.value = entry
  })
}

/** Server limits (internal/auth/moderation.go). */
export const MIN_TIMEOUT_SECONDS = 5
export const MAX_TIMEOUT_SECONDS = 28 * 24 * 3600
export const MIN_BAN_SECONDS = 60
export const MAX_BAN_SECONDS = 3650 * 24 * 3600
export const MAX_REASON_LENGTH = 200

/** The ready-made durations, in seconds. */
export const DURATION_CHOICES = [60, 300, 3600, 86400, 604800] as const

export const CUSTOM_UNITS = { seconds: 1, minutes: 60, hours: 3600, days: 86400 } as const
export type CustomUnit = keyof typeof CUSTOM_UNITS

/** Seconds for a custom amount and unit; null when it is not a positive whole number. */
export function customSeconds(amount: number | string, unit: CustomUnit): number | null {
  const n = typeof amount === 'number' ? amount : Number(amount)
  if (!Number.isInteger(n) || n < 1) return null
  return n * CUSTOM_UNITS[unit]
}

/** "1 min", "5 min", "1 h", "1 d", "1 w" for the ready-made choices; otherwise the largest exact unit. */
export function durationLabel(seconds: number): string {
  if (seconds % 604800 === 0) return t('moderation.units.w', { n: seconds / 604800 })
  if (seconds % 86400 === 0) return t('moderation.units.d', { n: seconds / 86400 })
  if (seconds % 3600 === 0) return t('moderation.units.h', { n: seconds / 3600 })
  if (seconds % 60 === 0) return t('moderation.units.min', { n: seconds / 60 })
  return t('moderation.units.s', { n: seconds })
}

/** A countdown: "0:42", "12:05", "1:02:09", "3 d 4 h". */
export function formatRemaining(ms: number): string {
  const total = Math.max(0, Math.ceil(ms / 1000))
  const d = Math.floor(total / 86400)
  const h = Math.floor((total % 86400) / 3600)
  const m = Math.floor((total % 3600) / 60)
  const s = total % 60
  if (d > 0) return `${d} d ${h} h`
  const mm = String(m).padStart(2, '0')
  const ss = String(s).padStart(2, '0')
  return h > 0 ? `${h}:${mm}:${ss}` : `${m}:${ss}`
}

export interface ModerationNotice {
  kind: 'timeout' | 'timeout_removed' | 'ban'
  until?: string
  duration_seconds?: number
  reason: string
}

/** The sentence a member reads about a moderation action against them. */
export function noticeText(n: ModerationNotice): string {
  const reason = n.reason ? `: ${n.reason}` : ''
  if (n.kind === 'timeout_removed') return t('moderation.noticeTimeoutRemoved')
  if (n.kind === 'ban') return t('moderation.noticeBan') + reason
  const duration = n.duration_seconds ? durationLabel(n.duration_seconds) : ''
  return t('moderation.noticeTimeout', { duration }) + reason
}
