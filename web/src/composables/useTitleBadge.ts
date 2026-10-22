// The tab title carries what is waiting: "(3) Mnema Talk" for mentions,
// "• Mnema Talk" when only other channels have news.
import { computed, onScopeDispose, watchEffect } from 'vue'
import { useChatStore } from '../stores/chat'
import { aggregateActivity, titlePrefix } from '../lib/channelActivity'

export function useTitleBadge() {
  const chatStore = useChatStore()
  const base = document.title
  const prefix = computed(() => {
    const states = chatStore.readStates
    return titlePrefix(aggregateActivity(Object.keys(states).map(id => ({ id })), states))
  })
  watchEffect(() => { document.title = prefix.value + base })
  onScopeDispose(() => { document.title = base })
}
