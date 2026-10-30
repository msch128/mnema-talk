import { ref } from 'vue'
import type { Category, Channel, ChannelHierarchy } from '../types/domain'
import { moveChannel, moveCategory } from '../lib/channelLayout'

/**
 * Drag and drop in the layout editor. `reorder` takes the reordered tree; a
 * drop that changes nothing hands it back unchanged, which is no change.
 */
export function useLayoutDrag(layout: () => ChannelHierarchy, reorder: (next: ChannelHierarchy) => void) {
  const draggedCategoryId = ref<string | null>(null)
  const draggedChannelId = ref<string | null>(null)

  // Only one kind of drag is ever in progress; a drag cancelled with Escape or
  // dropped outside ends in dragend, which forgets it.
  function onDragEnd() {
    draggedCategoryId.value = null
    draggedChannelId.value = null
  }

  function onCategoryDragStart(e: DragEvent, cat: Category) {
    draggedChannelId.value = null
    draggedCategoryId.value = cat.id
    if (e?.dataTransfer) e.dataTransfer.effectAllowed = 'move'
  }

  function onCategoryDrop(_e: DragEvent, targetIndex: number) {
    const id = draggedCategoryId.value
    draggedCategoryId.value = null
    if (id !== null) reorder(moveCategory(layout(), id, targetIndex))
  }

  function onChannelDragStart(e: DragEvent, channel: Channel) {
    draggedCategoryId.value = null
    draggedChannelId.value = channel.id
    if (e?.dataTransfer) e.dataTransfer.effectAllowed = 'move'
  }

  // The dropped channel takes the target's place (or goes last for a drop on
  // the category's list itself).
  function onChannelDrop(_e: DragEvent, targetCategoryId: string | null, targetIndex: number) {
    const id = draggedChannelId.value
    draggedChannelId.value = null
    if (id !== null) reorder(moveChannel(layout(), id, targetCategoryId, targetIndex))
  }

  return { onDragEnd, onCategoryDragStart, onCategoryDrop, onChannelDragStart, onChannelDrop }
}
