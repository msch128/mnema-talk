// How the later store (later.ts) reads its two lists: which answer is kept
// when requests cross, which lists a reconnect reads again, and what a failed
// read shows. The lists themselves stay in the store.
import { ref, type Ref } from 'vue'
import type { LaterReminder, LaterScheduled } from '../types/rest'
import { fetchReminders, fetchScheduled } from '../chat/lib/laterApi'

export interface LaterWant { reminders: boolean; scheduled: boolean }

/**
 * A loader that applies an answer only when its request was made after the
 * one whose answer is shown: an older answer that lands last would bring back
 * what a newer one (asked after an event or a reconnect) already left out. No
 * caller waits for anyone else's request, so a newer request that fails or
 * never answers neither holds up nor throws away an older good answer.
 * account() names the signed-in account's state (it changes on sign-out): an
 * answer to a request made for another one is never applied.
 */
export function newestOf<T>(fetch: () => Promise<T>, apply: (list: T) => void, account: () => number = () => 0) {
  let seq = 0
  let applied = 0
  return async () => {
    const mine = account()
    const request = ++seq
    const list = await fetch()
    if (mine !== account() || request < applied) return
    applied = request
    apply(list)
  }
}

/** The loading side of the later store over its two lists. */
export function createLaterLoading(reminders: Ref<LaterReminder[]>, scheduled: Ref<LaterScheduled[]>) {
  const loading = ref(false)
  const failed = ref(false)
  const loaded = ref(false)
  // Bumped by reset(): a list asked for before must not fill the next account's store.
  let generation = 0
  // The lists asked for so far: a reconnect reads those again, and only those.
  let wanted: LaterWant = { reminders: false, scheduled: false }

  const account = () => generation
  const loadReminders = newestOf(fetchReminders, list => { reminders.value = list }, account)
  const loadScheduled = newestOf(fetchScheduled, list => { scheduled.value = list }, account)

  /**
   * Loads the lists of the features the server has. With keepShown a failure
   * keeps lists that were shown (a read again after a reconnect): they are
   * still what was last known, and the error panel would hide them.
   */
  async function load(want: LaterWant, keepShown = false) {
    const mine = generation
    wanted = { reminders: wanted.reminders || want.reminders, scheduled: wanted.scheduled || want.scheduled }
    loading.value = true
    try {
      await Promise.all([want.reminders ? loadReminders() : null, want.scheduled ? loadScheduled() : null])
      if (mine !== generation) return
      loaded.value = true
      failed.value = false
    } catch (error) {
      if (mine !== generation) return
      console.warn('Failed to load reminders and scheduled messages:', error)
      failed.value = !(keepShown && loaded.value)
    } finally {
      if (mine === generation) loading.value = false
    }
  }

  /**
   * The socket opened: events from before it are lost, so the lists asked
   * for so far are read again. Lists never asked for stay unread.
   */
  async function refresh() {
    if (wanted.reminders || wanted.scheduled) await load(wanted, true)
  }

  /** Forgets the state of the signed-in account (the store clears its lists). */
  function reset() {
    ++generation
    loading.value = false; loaded.value = false; failed.value = false
    wanted = { reminders: false, scheduled: false }
  }

  return { loading, failed, loaded, load, refresh, reset, loadReminders, loadScheduled }
}
