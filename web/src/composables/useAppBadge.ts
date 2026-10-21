// Mirrors the unread mention count onto the taskbar/dock/app icon of the host.
import { computed, watch } from 'vue'
import { useChatStore } from '../stores/chat'
import { platform } from '../lib/platform'

export function useAppBadge() {
  const chat = useChatStore()
  // Muted channels do not count: the badge asks for attention the user asked for.
  const mentions = computed(() => Object.values(chat.readStates)
    .filter(state => chat.notificationLevel(state.channel_id) !== 'mute')
    .reduce((sum, state) => sum + state.mention_count, 0))
  watch(mentions, count => platform.setBadge(count), { immediate: true })
}
