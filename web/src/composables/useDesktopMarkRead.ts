// The server rail of the multi-instance desktop client has "Mark all as read".
// The host cannot reach the chat store, so it dispatches this event in the page
// of the server; the page marks every channel and conversation read through the
// same path as the sidebar. The new counts reach the host's rail by themselves:
// useAppBadge reports them whenever the read states change.
import { onBeforeUnmount, onMounted } from 'vue'
import { useChatStore } from '../stores/chat'
import { platform } from '../lib/platform'

export const MARK_ALL_READ_EVENT = 'mnema-desktop:mark-all-read'

export function useDesktopMarkRead() {
  const chat = useChatStore()
  // Only the page of a server in the desktop client listens; in a browser the event means nothing.
  const listen = () => platform.trustedApp()?.role === 'instance'
  const onRequest = () => { void chat.markAllRead() }
  onMounted(() => { if (listen()) window.addEventListener(MARK_ALL_READ_EVENT, onRequest) })
  onBeforeUnmount(() => window.removeEventListener(MARK_ALL_READ_EVENT, onRequest))
}
