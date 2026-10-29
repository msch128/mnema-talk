// The REST calls of reminders and scheduled messages (see stores/later).
import { api, isApiError } from './api'
import {
  decodeLaterReminder, decodeLaterReminderList, decodeLaterScheduled, decodeLaterScheduledList
} from '../types/rest'

export interface ScheduleOptions { parentId?: string | null; replyToId?: string | null }

export const fetchReminders = async () => (await api('/api/me/reminders', { decode: decodeLaterReminderList })).items

export const fetchScheduled = async () => (await api('/api/me/scheduled', { decode: decodeLaterScheduledList })).items

export const postReminder = (messageId: string, at: Date) => api('/api/me/reminders', {
  method: 'POST', json: { message_id: messageId, remind_at: at.toISOString() }, decode: decodeLaterReminder
})

export const deleteReminder = (id: string) => api(`/api/me/reminders/${id}`, { method: 'DELETE' })

export const postScheduled = (channelId: string, content: string, at: Date, options: ScheduleOptions = {}) => api(`/api/channels/${channelId}/scheduled`, {
  method: 'POST',
  json: {
    content, send_at: at.toISOString(),
    ...(options.parentId ? { parent_id: options.parentId } : {}),
    ...(options.replyToId ? { reply_to_id: options.replyToId } : {})
  },
  decode: decodeLaterScheduled
})

export const patchScheduled = (id: string, patch: { content?: string; at?: Date }) => api(`/api/me/scheduled/${id}`, {
  method: 'PATCH',
  json: { ...(patch.content === undefined ? {} : { content: patch.content }), ...(patch.at ? { send_at: patch.at.toISOString() } : {}) },
  decode: decodeLaterScheduled
})

// Already gone (another tab, a double click) counts as deleted.
export const deleteScheduled = (id: string) => api(`/api/me/scheduled/${id}`, { method: 'DELETE' }).catch((e: unknown) => {
  if (!(isApiError(e) && e.status === 404)) throw e
})
