// Mirrors the unread mention count onto the taskbar/dock/app icon of the host,
// the same number the tab title shows.
import { computed, watch } from 'vue'
import { useChatStore } from '../stores/chat'
import { platform } from '../lib/platform'
import { aggregateActivity } from '../lib/channelActivity'

export function useAppBadge() {
  const chat = useChatStore()
  const mentions = computed(() => {
    const states = chat.readStates
    return aggregateActivity(Object.keys(states).map(id => ({ id })), states).mentions
  })
  watch(mentions, count => platform.setBadge(count), { immediate: true })
}
