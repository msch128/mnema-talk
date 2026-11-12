import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api } from '../shared/lib/api'
import { decodeChatSavedMessage } from '../types/rest'
import { createSavedList } from './savedList'
import { createInFlightChanges } from './savedInFlight'
import { createSavedEdits } from './savedEdits'

// The member's private saved list. Whether a message is saved is known for the
// entries loaded so far and for what this session changed; saving an already
// saved message keeps its note, so not knowing is harmless. The server tells
// the member's other sessions (saved_updated).
export const useSavedStore = defineStore('saved', () => {
  const known = ref(new Set<string>())
  const list = createSavedList(entries => { for (const e of entries) known.value.add(e.message_id) })
  const { items } = list
  // Set by a save in this session: the header shows its one-time hint about where saved messages live.
  const hintPending = ref(false)
  let requested = false
  const inFlight = createInFlightChanges(items, known)
  const { tracked } = inFlight
  const edits = createSavedEdits(items, known, inFlight)
  const { drop, onMessageDeleted } = edits

  /**
   * Loads the newest entries again, replacing what was shown; changes made
   * meanwhile stay. The saved marks then follow the list: whichever load lands
   * last decides them, so a mark of a message removed elsewhere goes too.
   */
  async function load() {
    const applied = await tracked(list.load, true)
    if (applied) known.value = new Set(items.value.map(e => e.message_id))
    return applied
  }

  /** Appends the next older page; a removal made while it was on its way stays removed. */
  const loadMore = () => tracked(list.loadMore, false)

  /** Loads the newest page once, so messages already saved carry their mark before the list is opened. */
  async function ensureLoaded() {
    if (requested) return
    requested = true
    await load()
    if (list.failed.value) requested = false
  }

  function isSaved(messageId: string): boolean {
    return known.value.has(messageId)
  }

  /** Saves a message (the note stays as it is unless one is given). */
  async function save(messageId: string, note?: string) {
    const entry = await api(`/api/me/saved/${messageId}`, {
      method: 'PUT',
      ...(note === undefined ? {} : { json: { note } }),
      decode: decodeChatSavedMessage
    })
    edits.put(entry)
    return entry
  }

  async function remove(messageId: string) {
    await api(`/api/me/saved/${messageId}`, { method: 'DELETE' })
    drop(messageId)
  }

  /**
   * The socket opened: saves and removals made elsewhere before it sent no
   * event here, so a list asked for is read again (as far as it was shown).
   */
  async function refresh() {
    if (requested) await load()
  }

  /** Another session of mine saved or removed a message. */
  function onUpdated(p: { message_id: string; saved: boolean }) {
    if (!p.saved) return drop(p.message_id)
    known.value.add(p.message_id)
    void load()
  }

  /** Forgets everything of the signed-in member's list (sign-out). */
  function reset() {
    list.reset()
    inFlight.clear()
    requested = false
    hintPending.value = false
    known.value = new Set()
  }

  return { ...list, load, loadMore, hintPending, ensureLoaded, refresh, reset, isSaved, save, remove, onUpdated, onMessageDeleted }
})
