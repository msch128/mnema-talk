import { defineStore } from 'pinia'
import { computed, ref, type Ref } from 'vue'
import type { LaterReminder, LaterScheduled } from '../types/rest'
import {
  deleteReminder, deleteScheduled, fetchReminders, fetchScheduled, patchScheduled, postReminder, postScheduled,
  type ScheduleOptions
} from '../lib/laterApi'
import { isApiError } from '../lib/api'
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

/** Reminders that fell due and wait to be dismissed, scheduled messages the server refused to send, and both together. */
function attentionCounts(reminders: Ref<LaterReminder[]>, scheduled: Ref<LaterScheduled[]>) {
  const dueCount = computed(() => reminders.value.filter(r => r.delivered_at !== null).length)
  const failedCount = computed(() => scheduled.value.filter(s => s.state === 'failed').length)
  return { dueCount, failedCount, attention: computed(() => dueCount.value + failedCount.value) }
}

/** Puts an entry the server returned into a list, replacing an older copy of it. */
function keep<T extends { id: string }>(list: Ref<T[]>, entry: T): T {
  list.value = withEntry(list.value, entry)
  return entry
}

/**
 * Moves a reminder to a later time: the new one is made first, so a failure never loses it.
 * An old one already gone counts as removed; when it cannot be removed it is dropped from
 * the list and `cleanupFailed` says so, instead of reporting a failure that a retry would duplicate.
 */
async function snoozeReminder(list: Ref<LaterReminder[]>, id: string, messageId: string, at: Date) {
  const next = keep(list, await postReminder(messageId, at))
  let cleanupFailed = false
  try {
    await deleteReminder(id)
  } catch (error) {
    cleanupFailed = !(isApiError(error) && error.status === 404)
  }
  list.value = without(list.value, id)
  return { next, cleanupFailed }
}

// Bumped by reset(): a list asked for before must not fill the next account's store.
let generation = 0

async function latest<T>(fetch: () => Promise<T>, apply: (list: T) => void) {
  const mine = generation
  const list = await fetch()
  if (mine === generation) apply(list)
}

// The member's reminders and scheduled messages (both private, both kept by
// the server). The server tells the member's sessions about every change, so
// the lists reload on those events instead of being patched by hand.
export const useLaterStore = defineStore('later', () => {
  const reminders = ref<LaterReminder[]>([])
  const scheduled = ref<LaterScheduled[]>([])
  const loading = ref(false)
  const failed = ref(false)
  const loaded = ref(false)

  const { dueCount, failedCount, attention } = attentionCounts(reminders, scheduled)

  const loadReminders = () => latest(fetchReminders, list => { reminders.value = list })
  const loadScheduled = () => latest(fetchScheduled, list => { scheduled.value = list })

  /** Loads the lists of the features the server has. */
  async function load(want: { reminders: boolean; scheduled: boolean }) {
    const mine = generation
    loading.value = true
    try {
      await Promise.all([want.reminders ? loadReminders() : null, want.scheduled ? loadScheduled() : null])
      if (mine !== generation) return
      loaded.value = true
      failed.value = false
    } catch (error) {
      if (mine !== generation) return
      console.warn('Failed to load reminders and scheduled messages:', error)
      failed.value = true
    } finally {
      if (mine === generation) loading.value = false
    }
  }

  async function remind(messageId: string, at: Date) {
    return keep(reminders, await postReminder(messageId, at))
  }

  async function cancelReminder(id: string) {
    await deleteReminder(id)
    reminders.value = without(reminders.value, id)
  }

  const snooze = (id: string, messageId: string, at: Date) => snoozeReminder(reminders, id, messageId, at)

  async function schedule(channelId: string, content: string, at: Date, options: ScheduleOptions = {}) {
    return keep(scheduled, await postScheduled(channelId, content, at, options))
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

  const reset = () => {
    ++generation; reminders.value = []; scheduled.value = []
    loading.value = false; loaded.value = false; failed.value = false
  }

  return {
    reminders, scheduled, loading, failed, loaded, dueCount, failedCount, attention,
    load, remind, snooze, cancelReminder, schedule, updateScheduled, removeScheduled, onReminderEvent, onScheduledEvent, reset
  }
})
