// Moderator dialogs for messages: delete one with a reason, or delete the
// latest messages of a member in a channel. A single <MessageModerationDialog>
// host in App.vue renders whatever is pending, like lib/moderation.
import { shallowRef } from 'vue'

export const MAX_BULK_DELETE = 100
export const DEFAULT_BULK_DELETE = 10

export type MessageModerationRequest =
  | { kind: 'delete'; channelId: string; messageId: string; excerpt: string }
  | { kind: 'bulk'; channelId: string; userId: string; name: string }

export interface PendingMessageModeration {
  request: MessageModerationRequest
  resolve: (done: boolean) => void
}

export const pendingMessageModeration = shallowRef<PendingMessageModeration | null>(null)

/** Opens the dialog; resolves true once the action went through. */
export function moderateMessages(request: MessageModerationRequest): Promise<boolean> {
  pendingMessageModeration.value?.resolve(false)
  return new Promise<boolean>(resolve => {
    const entry: PendingMessageModeration = {
      request,
      resolve: done => {
        if (pendingMessageModeration.value === entry) pendingMessageModeration.value = null
        resolve(done)
      }
    }
    pendingMessageModeration.value = entry
  })
}
