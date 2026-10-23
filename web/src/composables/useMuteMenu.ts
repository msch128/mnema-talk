// Menu rows for a channel's or category's notification level, with timed
// mutes (15 minutes to "until I turn it back on"). Shared by the sidebar menus
// and the channel header's bell.
import { BellOff, Bell } from '@lucide/vue'
import type { NotifyLevel } from '../types/domain'
import type { ContextMenuItem } from '../components/menuTypes'
import { useChatStore } from '../stores/chat'
import { t } from '../i18n'
import { clockTimeOptions } from '../lib/time'

export type NotifyTarget = { kind: 'channel' | 'category'; id: string }

const MINUTE = 60_000

/** How long each timed mute lasts; `forever` mutes until it is turned back on. */
export const MUTE_DURATIONS = [
  { id: '15m', ms: 15 * MINUTE },
  { id: '1h', ms: 60 * MINUTE },
  { id: '8h', ms: 8 * 60 * MINUTE },
  { id: '24h', ms: 24 * 60 * MINUTE },
  { id: 'forever', ms: null }
] as const

/** When a mute ends, as a short local time ("14:30", or with the date on another day). */
export function formatMuteEnd(iso: string, now = new Date()): string {
  const end = new Date(iso)
  const sameDay = end.toDateString() === now.toDateString()
  return sameDay
    ? end.toLocaleTimeString([], clockTimeOptions())
    : end.toLocaleString([], { dateStyle: 'short', timeStyle: 'short' })
}

/** The stored setting of a target; 'inherit' when it has none. */
export function storedSetting(target: NotifyTarget): { level: NotifyLevel; muted_until: string | null } {
  const { channels, categories } = useChatStore().notifySettings
  const found = target.kind === 'channel'
    ? channels.find(s => s.channel_id === target.id)
    : categories.find(s => s.category_id === target.id)
  return { level: found?.level ?? 'inherit', muted_until: found?.muted_until ?? null }
}

function apply(target: NotifyTarget, level: NotifyLevel, until: string | null) {
  const chatStore = useChatStore()
  if (target.kind === 'channel') return chatStore.setNotificationLevel(target.id, level, until)
  return chatStore.setCategoryNotificationLevel(target.id, level, until)
}

function levelRows(target: NotifyTarget, current: NotifyLevel): ContextMenuItem[] {
  const inheritLabel = target.kind === 'channel' ? t('notifications.inheritChannel') : t('notifications.inheritCategory')
  const rows: Array<[NotifyLevel, string]> = [['inherit', inheritLabel], ['all', t('notifications.all')], ['mentions', t('notifications.mentions')]]
  return rows.map(([level, label]): ContextMenuItem => ({
    id: `notify-${level}`,
    type: 'radio',
    label,
    checked: current === level,
    action: () => { void apply(target, level, null) }
  }))
}

function muteSubmenu(target: NotifyTarget, muted: boolean, until: string | null): ContextMenuItem {
  return {
    type: 'submenu',
    id: 'notify-mute',
    label: t(target.kind === 'channel' ? 'notifications.muteChannel' : 'notifications.muteCategory'),
    icon: BellOff,
    value: muted ? (until ? t('notifications.mutedUntil', { time: formatMuteEnd(until) }) : t('notifications.mutedIndefinitely')) : null,
    items: MUTE_DURATIONS.map((d): ContextMenuItem => ({
      id: `mute-${d.id}`,
      label: t(`notifications.mute_${d.id}`),
      action: () => { void apply(target, 'mute', d.ms === null ? null : new Date(Date.now() + d.ms).toISOString()) }
    }))
  }
}

/** The "Notifications" block of a channel or category menu. */
export function notifyMenuItems(target: NotifyTarget): ContextMenuItem[] {
  const { level, muted_until } = storedSetting(target)
  const muted = level === 'mute'
  const items: ContextMenuItem[] = [
    { type: 'label', label: t('notifications.title') },
    ...levelRows(target, level),
    muteSubmenu(target, muted, muted_until)
  ]
  if (muted) {
    items.push({ id: 'notify-unmute', label: t('notifications.unmute'), icon: Bell, action: () => { void apply(target, 'inherit', null) } })
  }
  return items
}
