// The in-app notices of reminders and scheduled messages.
import { t } from '../i18n'
import type { LaterMessageRef, LaterReminder } from '../types/rest'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { useToastStore } from '../stores/toast'
import { isTabVisible } from '../stores/chat/readState'
import { platform } from './platform'
import { pushOnThisDevice } from './webPush'
import { isQuiet } from './presence'
import { previewText } from './replies'

const DUE_TOAST_MS = 12000
const KNOWN_REASONS = ['slow_mode', 'not_allowed', 'unavailable']

/** Who wrote it and where, in plain words: "Alice in #general: call mum", or "Alice wrote to you: call mum" in a conversation. */
export function reminderText(ref: LaterMessageRef | undefined | null): string {
  const text = ref ? previewText(ref.snippet) : ''
  if (!ref || !text) return t('later.reminderDue')
  if (ref.dm) return t('later.reminderDueDm', { author: ref.author, text })
  return t('later.reminderDueChannel', { author: ref.author, channel: ref.channel_name, text })
}

/**
 * A reminder fell due: say what it is about (author, place, text) while the
 * member may still see the message, and offer to open it. A page nobody looks
 * at, on a device without push, gets an operating system notification instead.
 */
export function noticeReminderDue(entry: LaterReminder | undefined) {
  const message = entry?.available ? entry.message : undefined
  const text = reminderText(message)
  const target = message ? { id: message.id, channel_id: message.channel_id, ...(message.parent_id ? { parent_id: message.parent_id } : {}) } : null
  const open = target ? () => { useChatStore().goToMessage(target) } : null
  // Opens the message; the panel under Later offers done and snooze.
  useToastStore().info(text, {
    duration: DUE_TOAST_MS,
    action: open ? { label: t('later.openMessage'), onClick: open } : null
  })
  if (target && open && !isTabVisible() && !pushOnThisDevice.value && !isQuiet(useAuthStore().user?.presence) && platform.notificationPermission() === 'granted') {
    platform.notify({ title: t('later.reminderTitle'), body: text, channelId: target.channel_id, messageId: target.id, onClick: open })
  }
}

/** A scheduled message was refused. An unknown reason is shown as a plain refusal, never as raw text. */
export function noticeScheduledFailed(reason: string | undefined) {
  const known = reason && KNOWN_REASONS.includes(reason) ? reason : 'refused'
  useToastStore().error(t('later.scheduledFailed'), { detail: t(`later.reason.${known}`) })
}
