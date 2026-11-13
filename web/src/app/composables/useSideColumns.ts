import { computed, onScopeDispose, ref, watch, watchEffect } from 'vue'
import { useChatStore } from '../../stores/chat'
import { useColumnRoom } from '../../shared/composables/useColumnRoom'
import { membersOverlay, membersSteppedAside, threadOverlay } from '../../shared/lib/sideColumns'

// The narrowest the chat column may get beside an open thread, and the
// thread's own narrowest width. Below their sum (plus the channel list) the
// thread opens over the chat instead of squeezing it to a sliver.
export const CHAT_MIN = 360
export const THREAD_MIN = 320

/**
 * Decides how the thread and the member list sit beside the chat on the
 * current window: as columns, the member list stepped aside for a thread, or
 * either one opened over the chat.
 */
export function useSideColumns(opts: { leftWidth: () => number; compact: () => boolean }) {
  const chatStore = useChatStore()
  const roomForAllColumns = useColumnRoom()
  const width = ref(typeof window === 'undefined' ? 1920 : window.innerWidth)
  const onResize = () => { width.value = window.innerWidth }
  if (typeof window !== 'undefined') {
    window.addEventListener('resize', onResize)
    onScopeDispose(() => window.removeEventListener('resize', onResize))
  }

  const threadOver = computed(() => !opts.compact() && !!chatStore.activeThread && width.value < opts.leftWidth() + THREAD_MIN + CHAT_MIN)
  const steppedAside = computed(() => !opts.compact() && !!chatStore.activeThread && !threadOver.value && !roomForAllColumns.value)
  watchEffect(() => {
    threadOverlay.value = threadOver.value
    membersSteppedAside.value = steppedAside.value
  })
  // Asking for the list over the chat lasts until the list or the thread closes.
  watch(() => !!chatStore.activeThread && chatStore.showMemberList, open => { if (!open) membersOverlay.value = false })

  return {
    threadOver,
    /** The member list takes a column of its own. */
    membersColumn: computed(() => chatStore.showMemberList && !steppedAside.value),
    /** The member list is on screen over the chat. */
    membersOver: computed(() => chatStore.showMemberList && steppedAside.value && membersOverlay.value),
  }
}
