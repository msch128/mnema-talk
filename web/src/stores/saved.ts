import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api } from '../shared/lib/api'
import { decodeChatSavedMessage, type ChatSavedMessage } from '../types/rest'
import { createSavedList } from './savedList'

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
  // What changed here while a page was on its way (an entry, or null for a
  // removal), numbered: the page was read before those changes, so they are
  // applied to it again. Kept only while a page is on its way.
  const pending = new Map<string, { at: number; entry: ChatSavedMessage | null }>()
  let changes = 0
  let loads = 0

  function remember(messageId: string, entry: ChatSavedMessage | null) {
    if (loads > 0) pending.set(messageId, { at: ++changes, entry })
  }

  /** A save is put back on top only by a replacing load; an appended older page only must not bring back a removal. */
  function reapply(from: number, saves: boolean) {
    for (const [id, change] of pending) {
      if (change.at <= from) continue
      if (change.entry === null) {
        known.value.delete(id)
        items.value = items.value.filter(e => e.message_id !== id)
      } else if (saves) {
        items.value = [change.entry, ...items.value.filter(e => e.message_id !== id)]
      }
    }
  }

  async function tracked(read: () => Promise<boolean>, saves: boolean) {
    const from = changes
    loads++
    try {
      const applied = await read()
      if (applied) reapply(from, saves)
      return applied
    } finally {
      if (--loads === 0) pending.clear()
    }
  }

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
    known.value.add(messageId)
    const at = items.value.findIndex(e => e.message_id === messageId)
    if (at >= 0) items.value.splice(at, 1, entry)
    else items.value.unshift(entry)
    remember(messageId, entry)
    return entry
  }

  async function remove(messageId: string) {
    await api(`/api/me/saved/${messageId}`, { method: 'DELETE' })
    drop(messageId)
  }

  function drop(messageId: string) {
    known.value.delete(messageId)
    items.value = items.value.filter(e => e.message_id !== messageId)
    remember(messageId, null)
  }

  /**
   * The socket opened: saves and removals made elsewhere before it sent no
   * event here, so a list asked for is read again (as far as it was shown).
   */
  async function refresh() {
    if (requested) await load()
  }

  /** A message was deleted: the server forgets its save (and those of its thread replies). */
  function onMessageDeleted(messageId: string) {
    for (const e of items.value) {
      if (e.message_id !== messageId && e.message?.parent_id === messageId) drop(e.message_id)
    }
    // Only a message saved, or one a page on its way may still bring back, is dropped.
    if (known.value.has(messageId) || loads > 0 || items.value.some(e => e.message_id === messageId)) drop(messageId)
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
    pending.clear()
    requested = false
    hintPending.value = false
    known.value = new Set()
  }

  return { ...list, load, loadMore, hintPending, ensureLoaded, refresh, reset, isSaved, save, remove, onUpdated, onMessageDeleted }
})
