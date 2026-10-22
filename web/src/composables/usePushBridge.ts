// Connects the app to its service worker's push: a click on a notification
// opens the message, and the page knows whether this device receives push.
import { onBeforeUnmount, onMounted, watch } from 'vue'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { listenForOpenRequests, refreshPushState } from '../lib/webPush'
import type { Message } from '../types/domain'

export function usePushBridge() {
  const auth = useAuthStore()
  const chat = useChatStore()
  let stop: () => void = () => {}

  onMounted(() => {
    stop = listenForOpenRequests(request => {
      // goToMessage needs only where the message lives; a thread reply opens at its root.
      const target = { id: request.message_id, channel_id: request.channel_id, ...(request.parent_id ? { parent_id: request.parent_id } : {}) }
      chat.goToMessage(target as Message)
    })
  })
  onBeforeUnmount(() => stop())

  // Whether this device receives push is asked once the account is known.
  watch(() => auth.user?.id, id => { if (id) void refreshPushState() }, { immediate: true })
}
