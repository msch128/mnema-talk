// The message a member is about to forward. A single <ForwardDialog> host in
// App.vue shows whatever is pending, like lib/editHistory.
import { shallowRef } from 'vue'

export interface ForwardRequest {
  messageId: string
  /** A short plain-text excerpt shown in the dialog. */
  excerpt: string
}

export const pendingForward = shallowRef<ForwardRequest | null>(null)

export function openForward(request: ForwardRequest) {
  pendingForward.value = request
}

export function closeForward() {
  pendingForward.value = null
}
