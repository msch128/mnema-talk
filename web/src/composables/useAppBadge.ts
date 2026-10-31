// Mirrors what is waiting onto the taskbar/dock/app icon of the host: the
// unread mention count, the same number the tab title shows. The multi-instance
// desktop client takes the unread channels as well and shows both per server on
// its rail, so there the pair goes to `reportUnread` instead of the plain badge.
import { computed, watch } from 'vue'
import { useChatStore } from '../stores/chat'
import { platform } from '../lib/platform'
import { aggregateActivity } from '../lib/channelActivity'
import { useDesktopMarkRead } from './useDesktopMarkRead'

export function useAppBadge() {
  const chat = useChatStore()
  // The rail's "Mark all as read" clears the same counts this reports.
  useDesktopMarkRead()
  const activity = computed(() => {
    const states = chat.readStates
    return aggregateActivity(Object.keys(states).map(id => ({ id })), states)
  })
  const mentions = computed(() => activity.value.mentions)
  const unread = computed(() => activity.value.unreadChannels)
  // Two numbers, so a change of either (and nothing else) reaches the host.
  watch([unread, mentions], ([unreadChannels, mentionCount]) => {
    if (platform.supportsUnreadReport()) platform.reportUnread({ unread: unreadChannels, mentions: mentionCount })
    else platform.setBadge(mentionCount)
  }, { immediate: true })
}
