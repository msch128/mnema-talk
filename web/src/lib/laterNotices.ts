// The in-app notices of reminders and scheduled messages.
import { t } from '../i18n'
import type { LaterReminder } from '../types/rest'
import { useToastStore } from '../stores/toast'
import { useChatStore } from '../stores/chat'
import { previewText } from './replies'

const DUE_TOAST_MS = 12000
const KNOWN_REASONS = ['slow_mode', 'not_allowed', 'unavailable']

/** A reminder fell due: say what it is about, when the member may still see the message. */
export function noticeReminderDue(entry: LaterReminder | undefined) {
  const text = entry?.message ? previewText(entry.message.snippet) : ''
  const message = entry?.available ? entry.message : undefined
  const open = message ? () => {
    const { id, channel_id, parent_id } = message
    useChatStore().goToMessage({ id, channel_id, ...(parent_id ? { parent_id } : {}) })
  } : null
  // Opens the message; the panel under Later offers done and snooze.
  useToastStore().info(text ? t('later.reminderDueWith', { text }) : t('later.reminderDue'), {
    duration: DUE_TOAST_MS,
    action: open ? { label: t('later.openMessage'), onClick: open } : null
  })
}

/** A scheduled message was refused. An unknown reason is shown as a plain refusal, never as raw text. */
export function noticeScheduledFailed(reason: string | undefined) {
  const known = reason && KNOWN_REASONS.includes(reason) ? reason : 'refused'
  useToastStore().error(t('later.scheduledFailed'), { detail: t(`later.reason.${known}`) })
}
