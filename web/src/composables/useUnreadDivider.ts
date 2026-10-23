// The "new since" divider of a channel and the Esc shortcut that clears it.
import { ref, computed, watch } from 'vue'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { firstUnreadId } from '../lib/chatLogic'
import { t, locale } from '../i18n'
import { clockTimeOptions } from '../lib/time'

export function useUnreadDivider() {
  const chatStore = useChatStore()
  const authStore = useAuthStore()

  // Last-read time captured when the channel was opened: the divider stays put
  // while the channel is open, even though the server marks it read meanwhile.
  const dividerSince = ref<string | null>(null)
  watch(
    () => [chatStore.activeChannel?.id, chatStore.activeChannelLastReadAt],
    () => { dividerSince.value = chatStore.activeChannelLastReadAt },
    { immediate: true }
  )
  const dividerBeforeId = computed(() =>
    firstUnreadId(chatStore.messages, dividerSince.value, authStore.user?.id, chatStore.hasMoreBefore)
  )

  function dividerLabel() {
    const d = new Date(dividerSince.value ?? 0)
    const sameDay = d.toDateString() === new Date().toDateString()
    const time = sameDay
      ? d.toLocaleTimeString([locale.value], clockTimeOptions(locale.value))
      : d.toLocaleString([locale.value], { day: 'numeric', month: 'short', ...clockTimeOptions(locale.value) })
    return t('chat.newSinceDivider', { time })
  }

  // Esc in the channel: mark everything read and drop the divider.
  function markAllRead() {
    const id = chatStore.activeChannel?.id
    if (!id) return false
    const st = chatStore.readStates[id]
    if (!dividerBeforeId.value && !((st?.unread_count ?? 0) > 0) && !((st?.mention_count ?? 0) > 0)) return false
    dividerSince.value = null
    chatStore.markChannelRead(id)
    return true
  }

  return { dividerSince, dividerBeforeId, dividerLabel, markAllRead }
}
