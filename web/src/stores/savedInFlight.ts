// The saved store's bookkeeping for pages on their way (saved.ts): a page was
// read by the server before the changes made here while it travelled, so
// those changes are applied to it again when it lands.
import type { Ref } from 'vue'
import type { ChatSavedMessage } from '../types/rest'

export function createInFlightChanges(items: Ref<ChatSavedMessage[]>, known: Ref<Set<string>>) {
  // What changed here while a page was on its way (an entry, or null for a
  // removal), numbered. Kept only while a page is on its way.
  const pending = new Map<string, { at: number; entry: ChatSavedMessage | null }>()
  // Thread roots deleted while a page was on its way: the page may hold saved
  // replies of theirs (saved elsewhere, not shown here yet), which went with them.
  const deletedRoots = new Map<string, number>()
  let changes = 0
  let loads = 0

  /** Notes a save (the entry) or a removal (null) made here. */
  function remember(messageId: string, entry: ChatSavedMessage | null) {
    if (loads > 0) pending.set(messageId, { at: ++changes, entry })
  }

  /** Notes a deleted message whose saved replies a page on its way may still hold. */
  function rememberDeletedRoot(messageId: string) {
    if (loads > 0) deletedRoots.set(messageId, ++changes)
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
    for (const [root, at] of deletedRoots) {
      if (at <= from) continue
      for (const e of items.value) if (e.message?.parent_id === root) known.value.delete(e.message_id)
      items.value = items.value.filter(e => e.message?.parent_id !== root)
    }
  }

  function clear() {
    pending.clear()
    deletedRoots.clear()
  }

  /** Runs a page read (true when its answer was shown) and applies the changes made meanwhile to it. */
  async function tracked(read: () => Promise<boolean>, saves: boolean) {
    const from = changes
    loads++
    try {
      const applied = await read()
      if (applied) reapply(from, saves)
      return applied
    } finally {
      if (--loads === 0) clear()
    }
  }

  return { remember, rememberDeletedRoot, tracked, clear, inFlight: () => loads > 0 }
}
