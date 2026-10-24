import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import type { LaterReminder, LaterScheduled } from '../types/rest'
import {
  deleteReminder, deleteScheduled, fetchReminders, fetchScheduled, patchScheduled, postReminder, postScheduled,
  type ScheduleOptions
} from '../lib/laterApi'
import { noticeReminderDue, noticeScheduledFailed } from '../lib/laterNotices'

export type { ScheduleOptions }

/** The payloads of reminder_update and scheduled_message_update. */
export interface ReminderEventPayload { id: string; state: 'created' | 'due' | 'removed' }
export interface ScheduledEventPayload {
  id: string
  channel_id: string
  state: 'created' | 'updated' | 'sent' | 'failed' | 'deleted'
  reason?: string
}

const withEntry = <T extends { id: string }>(list: T[], entry: T) => [...list.filter(e => e.id !== entry.id), entry]
const without = <T extends { id: string }>(list: T[], id: string) => list.filter(e => e.id !== id)

// The member's reminders and scheduled messages (both private, both kept by
// the server). The server tells the member's sessions about every change, so
// the lists reload on those events instead of being patched by hand.
export const useLaterStore = defineStore('later', () => {
  const reminders = ref<LaterReminder[]>([])
  const scheduled = ref<LaterScheduled[]>([])
  const loading = ref(false)
  const failed = ref(false)
  const loaded = ref(false)

  /** Reminders that fell due and wait to be dismissed. */
  const dueCount = computed(() => reminders.value.filter(r => r.delivered_at !== null).length)
  /** Scheduled messages the server refused to send. */
  const failedCount = computed(() => scheduled.value.filter(s => s.state === 'failed').length)
  const attention = computed(() => dueCount.value + failedCount.value)

  const loadReminders = async () => { reminders.value = await fetchReminders() }
  const loadScheduled = async () => { scheduled.value = await fetchScheduled() }

  /** Loads the lists of the features the server has. */
  async function load(want: { reminders: boolean; scheduled: boolean }) {
    loading.value = true
    try {
      await Promise.all([want.reminders ? loadReminders() : null, want.scheduled ? loadScheduled() : null])
      loaded.value = true
      failed.value = false
    } catch (error) {
      console.warn('Failed to load reminders and scheduled messages:', error)
      failed.value = true
    } finally {
      loading.value = false
    }
  }

  async function remind(messageId: string, at: Date) {
    const entry = await postReminder(messageId, at)
    reminders.value = withEntry(reminders.value, entry)
    return entry
  }

  async function cancelReminder(id: string) {
    await deleteReminder(id)
    reminders.value = without(reminders.value, id)
  }

  async function schedule(channelId: string, content: string, at: Date, options: ScheduleOptions = {}) {
    const entry = await postScheduled(channelId, content, at, options)
    scheduled.value = withEntry(scheduled.value, entry)
    return entry
  }

  async function updateScheduled(id: string, patch: { content?: string; at?: Date }) {
    const entry = await patchScheduled(id, patch)
    scheduled.value = scheduled.value.map(s => (s.id === id ? entry : s))
    return entry
  }

  async function removeScheduled(id: string) {
    await deleteScheduled(id)
    scheduled.value = without(scheduled.value, id)
  }

  /** A reminder was made, fell due or was removed (possibly in another session). */
  async function onReminderEvent(p: ReminderEventPayload) {
    if (loaded.value) await loadReminders().catch(() => undefined)
    if (p.state !== 'due') return
    noticeReminderDue(reminders.value.find(r => r.id === p.id))
  }

  /** A scheduled message was made, changed, sent or refused. */
  async function onScheduledEvent(p: ScheduledEventPayload) {
    if (p.state === 'failed') noticeScheduledFailed(p.reason)
    if (loaded.value) await loadScheduled().catch(() => undefined)
  }

  const reset = () => { reminders.value = []; scheduled.value = []; loaded.value = false; failed.value = false }

  return {
    reminders, scheduled, loading, failed, loaded, dueCount, failedCount, attention,
    load, remind, cancelReminder, schedule, updateScheduled, removeScheduled, onReminderEvent, onScheduledEvent, reset
  }
})
