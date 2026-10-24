// The in-app notices of reminders and scheduled messages.
import { t } from '../i18n'
import type { LaterReminder } from '../types/rest'
import { useToastStore } from '../stores/toast'
import { previewText } from './replies'

const KNOWN_REASONS = ['slow_mode', 'not_allowed', 'unavailable']

/** A reminder fell due: say what it is about, when the member may still see the message. */
export function noticeReminderDue(entry: LaterReminder | undefined) {
  const text = entry?.message ? previewText(entry.message.snippet) : ''
  useToastStore().info(text ? t('later.reminderDueWith', { text }) : t('later.reminderDue'))
}

/** A scheduled message was refused. An unknown reason is shown as a plain refusal, never as raw text. */
export function noticeScheduledFailed(reason: string | undefined) {
  const known = reason && KNOWN_REASONS.includes(reason) ? reason : 'refused'
  useToastStore().error(t('later.scheduledFailed'), { detail: t(`later.reason.${known}`) })
}
