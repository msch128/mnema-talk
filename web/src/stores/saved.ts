import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api } from '../lib/api'
import { decodeChatSavedMessage } from '../types/rest'
import { createSavedList } from './savedList'

// The member's private saved list. Whether a message is saved is known for the
// entries loaded so far and for what this session changed; saving an already
// saved message keeps its note, so not knowing is harmless. The server tells
// the member's other sessions (saved_updated).
export const useSavedStore = defineStore('saved', () => {
  const known = ref(new Set<string>())
  const list = createSavedList(entries => { for (const e of entries) known.value.add(e.message_id) })
  const { items } = list

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
    return entry
  }

  async function remove(messageId: string) {
    await api(`/api/me/saved/${messageId}`, { method: 'DELETE' })
    drop(messageId)
  }

  function drop(messageId: string) {
    known.value.delete(messageId)
    items.value = items.value.filter(e => e.message_id !== messageId)
  }

  /** Another session of mine saved or removed a message. */
  function onUpdated(p: { message_id: string; saved: boolean }) {
    if (!p.saved) return drop(p.message_id)
    known.value.add(p.message_id)
    void list.load()
  }

  return { ...list, isSaved, save, remove, onUpdated }
})
