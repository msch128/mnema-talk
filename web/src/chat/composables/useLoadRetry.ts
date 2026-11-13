import { nextTick } from 'vue'
import { useChatStore } from '../../stores/chat'

/**
 * "Try again" on a chat whose messages failed to load. The button goes away
 * either way, so focus moves on with it: to the new retry button when the
 * load failed again, otherwise to the chat (the column the messages are in).
 */
export function useLoadRetry(column: () => HTMLElement | null) {
  const chat = useChatStore()
  return async function retryLoad() {
    const id = chat.activeChannel?.id
    if (!id) return
    await chat.fetchMessages(id)
    await nextTick()
    const again = document.querySelector<HTMLElement>('[data-testid="chat-load-failed"] [data-testid="state-retry"]')
    ;(again ?? column())?.focus()
  }
}
