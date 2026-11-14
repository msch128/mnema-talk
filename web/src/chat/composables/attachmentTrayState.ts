// Where the attachment trays live: one list of files per composer slot (a
// channel, a thread). Kept apart from the uploading so signing out can drop
// the files without loading the upload code.
import { reactive } from 'vue'
import type { TrayItem } from '../lib/attachmentTray'

export const trays = reactive(new Map<string, TrayItem[]>())
/** Running uploads by item key. */
export const aborts = new Map<string, AbortController>()

export const itemsOf = (slot: string): TrayItem[] => {
  if (!trays.has(slot)) trays.set(slot, [])
  return trays.get(slot) as TrayItem[]
}

/**
 * Reactive: true while any tray holds a file, whatever its state: one waiting
 * or uploading, and one uploaded but not sent yet, are all lost with the page
 * (trays live in memory only). Reads without creating a tray.
 */
export function anyAttachmentHeld(): boolean {
  for (const items of trays.values()) {
    if (items.length > 0) return true
  }
  return false
}

/** Lets go of an item's preview and cancels its upload. */
export function release(item: TrayItem) {
  if (item.previewUrl) URL.revokeObjectURL(item.previewUrl)
  aborts.get(item.key)?.abort()
  aborts.delete(item.key)
}

/** Forgets every file of a tray without touching the server: they were sent. */
export function clearSlot(slot: string) {
  for (const item of itemsOf(slot)) release(item)
  trays.set(slot, [])
}

/** Forgets every tray (sign-out, switching community): the files were never sent. */
export function clearAllTrays() {
  for (const slot of [...trays.keys()]) clearSlot(slot)
  trays.clear()
}
