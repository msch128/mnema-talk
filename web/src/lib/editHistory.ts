// The edit history a moderator can open from the message menu. A single
// <EditHistoryDialog> host in App.vue shows whatever is pending, like
// lib/messageModeration.
import { shallowRef } from 'vue'

export interface EditHistoryRequest {
  channelId: string
  messageId: string
  /** The message as it reads now, shown above its earlier versions. */
  current: string
  editedAt: string
}

export const pendingEditHistory = shallowRef<EditHistoryRequest | null>(null)

export function openEditHistory(request: EditHistoryRequest) {
  pendingEditHistory.value = request
}

export function closeEditHistory() {
  pendingEditHistory.value = null
}
