// The tab title carries what is waiting: "(3) Name" for mentions,
// "• Name" when only other channels have news.
import { computed, onScopeDispose, ref, watch, watchEffect } from 'vue'
import { useChatStore } from '../stores/chat'
import { aggregateActivity, titlePrefix } from '../lib/channelActivity'
import { useCommunityName } from './useCommunityName'

export function useTitleBadge() {
  const chatStore = useChatStore()
  // The server already put the community's name into the page; a rename while the tab is open follows.
  const original = document.title
  const base = ref(original)
  const name = useCommunityName(original)
  watch(name, next => { base.value = next })
  const prefix = computed(() => {
    const states = chatStore.readStates
    return titlePrefix(aggregateActivity(Object.keys(states).map(id => ({ id })), states))
  })
  watchEffect(() => { document.title = prefix.value + base.value })
  onScopeDispose(() => { document.title = original })
}
