// Opening a forum post marks it read. What was unread in it (the replies of
// others since the member last read it) is kept so the post can offer a jump to
// the first of them. New replies that arrive while the post is open are read.
import { ref, computed, watch, onBeforeUnmount } from 'vue'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { markForumPostRead } from '../lib/forumApi'

const REMARK_DELAY_MS = 1000

export function useForumPostRead() {
  const chatStore = useChatStore()
  const authStore = useAuthStore()
  // When the member had read the open post, as the server said on opening it.
  const since = ref<{ postId: string; at: number } | null>(null)
  const dismissed = ref(false)
  let remarkTimer: ReturnType<typeof setTimeout> | undefined

  const postId = computed(() => chatStore.activeThread?.title ? chatStore.activeThread.id : null)

  async function mark(id: string, remember: boolean) {
    const channelId = chatStore.activeThread?.channel_id ?? chatStore.activeChannel?.id
    if (!channelId) return
    try {
      const previous = await markForumPostRead(channelId, id)
      if (remember && postId.value === id) since.value = { postId: id, at: Date.parse(previous) }
    } catch (err) {
      console.warn('Failed to mark the forum post read:', err)
    }
  }

  watch(postId, id => {
    since.value = null
    dismissed.value = false
    clearTimeout(remarkTimer)
    if (id) void mark(id, true)
  }, { immediate: true })

  // Replies that come in while the post is open are seen.
  watch(() => chatStore.threadReplies.length, () => {
    const id = postId.value
    if (!id) return
    clearTimeout(remarkTimer)
    remarkTimer = setTimeout(() => { void mark(id, false) }, REMARK_DELAY_MS)
  })

  onBeforeUnmount(() => clearTimeout(remarkTimer))

  const unreadReplies = computed(() => {
    const marker = since.value
    if (!marker || marker.postId !== postId.value || dismissed.value) return []
    return chatStore.threadReplies.filter(r => r.user_id !== authStore.user?.id && Date.parse(r.created_at) > marker.at)
  })

  return {
    unreadReplies,
    firstUnreadId: computed(() => unreadReplies.value[0]?.id ?? null),
    dismiss: () => { dismissed.value = true }
  }
}
