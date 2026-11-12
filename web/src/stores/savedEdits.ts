// The changes this session makes to the saved list it shows (saved.ts): a
// save, a removal and a deleted message, each noted for a page on its way
// (savedInFlight.ts).
import type { Ref } from 'vue'
import type { ChatSavedMessage } from '../types/rest'
import type { createInFlightChanges } from './savedInFlight'

export function createSavedEdits(items: Ref<ChatSavedMessage[]>, known: Ref<Set<string>>, inFlight: ReturnType<typeof createInFlightChanges>) {
  /** Shows a saved entry the server returned, in place of an older copy or on top. */
  function put(entry: ChatSavedMessage) {
    known.value.add(entry.message_id)
    const at = items.value.findIndex(e => e.message_id === entry.message_id)
    if (at >= 0) items.value.splice(at, 1, entry)
    else items.value.unshift(entry)
    inFlight.remember(entry.message_id, entry)
  }

  /** Takes a message off the list and its mark away. */
  function drop(messageId: string) {
    known.value.delete(messageId)
    items.value = items.value.filter(e => e.message_id !== messageId)
    inFlight.remember(messageId, null)
  }

  /** A message was deleted: the server forgets its save (and those of its thread replies). */
  function onMessageDeleted(messageId: string) {
    for (const e of items.value) {
      if (e.message_id !== messageId && e.message?.parent_id === messageId) drop(e.message_id)
    }
    // Only a message saved, or one a page on its way may still bring back, is dropped.
    if (known.value.has(messageId) || inFlight.inFlight() || items.value.some(e => e.message_id === messageId)) drop(messageId)
    inFlight.rememberDeletedRoot(messageId)
  }

  return { put, drop, onMessageDeleted }
}
