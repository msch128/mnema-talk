// What the member is about to remind themselves of or to schedule. A single
// <LaterDialog> host in App.vue shows whatever is pending, like lib/forwardDialog.
import { shallowRef } from 'vue'

export interface RemindRequest {
  kind: 'remind'
  messageId: string
  /** A short plain-text excerpt shown in the dialog. */
  excerpt: string
}

export interface ScheduleRequest {
  kind: 'schedule'
  channelId: string
  content: string
  parentId?: string | null
  replyToId?: string | null
  /** Called after the server accepted the message (the composer empties itself). */
  onScheduled?: () => void
}

export type LaterRequest = RemindRequest | ScheduleRequest

export const pendingLater = shallowRef<LaterRequest | null>(null)

export function openLater(request: LaterRequest) {
  pendingLater.value = request
}

export function closeLater() {
  pendingLater.value = null
}
