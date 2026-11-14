// A channel's auto-delete (internal/chat/channels/auto_delete.go): a period in
// hours, days or weeks, what it removes (the messages or only their files), or
// the community default. doc/timed-messages.md.
import { confirm } from './confirm'
import { t } from '../../i18n'
import type { Channel } from '../../types/domain'

export const AUTO_DELETE_DAYS = [1, 7, 30, 90] as const
export const MAX_AUTO_DELETE_HOURS = 8760

export type AutoDeleteScope = 'all' | 'attachments'
export type AutoDeleteUnit = 'hours' | 'days' | 'weeks'
export const UNIT_HOURS: Readonly<Record<AutoDeleteUnit, number>> = { hours: 1, days: 24, weeks: 168 }

/** A channel's own setting: the community default, off, or a period. */
export type AutoDeleteSetting =
  | { mode: 'default' }
  | { mode: 'off' }
  | { mode: 'period'; hours: number; scope: AutoDeleteScope }

/** What applies to a channel now; hours 0 is off. */
export interface AutoDeleteEffect { hours: number; scope: AutoDeleteScope }

/** The effective auto-delete of a channel, also from a server that answers whole days only. */
export function effectOf(ch: Pick<Channel, 'auto_delete_days' | 'auto_delete_hours' | 'auto_delete_scope'> | null | undefined): AutoDeleteEffect {
  if (!ch) return { hours: 0, scope: 'all' }
  if (typeof ch.auto_delete_hours === 'number') return { hours: ch.auto_delete_hours, scope: ch.auto_delete_scope === 'attachments' ? 'attachments' : 'all' }
  return { hours: (ch.auto_delete_days ?? 0) * 24, scope: 'all' }
}

/** The channel's own setting, for its settings form. */
export function settingOf(ch: Pick<Channel, 'auto_delete_days' | 'auto_delete_hours' | 'auto_delete_scope' | 'auto_delete_follows_default'> | null | undefined): AutoDeleteSetting {
  if (ch?.auto_delete_follows_default) return { mode: 'default' }
  const e = effectOf(ch)
  return e.hours > 0 ? { mode: 'period', hours: e.hours, scope: e.scope } : { mode: 'off' }
}

/** A period in its largest whole unit: "6 hours", "2 days", "3 weeks". */
export function periodLabel(hours: number): string {
  const unit = (key: string, n: number) => t(key, { n, count: n })
  if (hours >= 168 && hours % 168 === 0) return unit('autoDelete.units.weeks', hours / 168)
  if (hours >= 24 && hours % 24 === 0) return unit('autoDelete.units.days', hours / 24)
  return unit('autoDelete.units.hours', hours)
}

/** The line a channel shows while auto-delete is on, or '' when it is off. */
export function effectLine(e: AutoDeleteEffect): string {
  if (e.hours <= 0) return ''
  return t(e.scope === 'attachments' ? 'autoDelete.lineFiles' : 'autoDelete.line', { period: periodLabel(e.hours) })
}

/** The request body of PUT .../auto-delete (and of its dry run). */
export function requestBody(s: AutoDeleteSetting, v2: boolean): Record<string, unknown> {
  if (!v2) return { days: s.mode === 'period' ? Math.round(s.hours / 24) : 0 }
  if (s.mode === 'default') return { use_default: true }
  if (s.mode === 'off') return { hours: 0 }
  return { hours: s.hours, scope: s.scope }
}

/** Encodes a setting for a form draft field. */
export function encodeSetting(s: AutoDeleteSetting): string {
  return s.mode === 'period' ? `${s.hours}:${s.scope}` : s.mode
}

/** Reads a draft field back; anything unreadable is off. */
export function decodeSetting(value: string): AutoDeleteSetting {
  if (value === 'default') return { mode: 'default' }
  const [h, scope] = value.split(':')
  const hours = Number(h)
  if (!Number.isInteger(hours) || hours < 1 || hours > MAX_AUTO_DELETE_HOURS) return { mode: 'off' }
  return { mode: 'period', hours, scope: scope === 'attachments' ? 'attachments' : 'all' }
}

/** What a dry run found: confirm when it would remove anything. */
export interface AutoDeleteCounts { messages: number; attachments: number }

/** "450 messages and 3 files", "1 file": the counts in words, a zero left out. */
export function countsText(c: AutoDeleteCounts): string {
  const parts = [
    ...(c.messages > 0 ? [t('autoDelete.messagesCount', { n: c.messages, count: c.messages })] : []),
    ...(c.attachments > 0 ? [t('autoDelete.filesCount', { n: c.attachments, count: c.attachments })] : [])
  ]
  return parts.join(t('autoDelete.and'))
}

/** Asks before content goes: only when the dry run found something to remove. */
export function confirmAutoDelete(where: string, counts: AutoDeleteCounts, followers = false): Promise<boolean> {
  if (counts.messages <= 0 && counts.attachments <= 0) return Promise.resolve(true)
  return confirm({
    title: t('autoDelete.confirmTitle'),
    body: [
      t(followers ? 'autoDelete.confirmDefaultBody' : 'autoDelete.confirmCounts', { name: where, what: countsText(counts) }),
      t('autoDelete.confirmNoUndo')
    ].join(' '),
    confirmLabel: t('autoDelete.confirmAction'),
    danger: true
  })
}
