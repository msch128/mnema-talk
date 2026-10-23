import { ref } from 'vue'
import { api } from '../lib/api'
import { decodeChatSavedPage, type ChatSavedMessage } from '../types/rest'

const PAGE = 50

/** The paged saved list (GET /api/me/saved): newest first, older pages behind the last entry shown. */
export function createSavedList(onEntries: (entries: ChatSavedMessage[]) => void) {
  const items = ref<ChatSavedMessage[]>([])
  const hasMore = ref(false)
  const loading = ref(false)
  const loadingMore = ref(false)
  const failed = ref(false)
  let seq = 0

  /** Loads the newest page, replacing what was shown. */
  async function load() {
    const mine = ++seq
    loading.value = true
    failed.value = false
    try {
      const page = await api(`/api/me/saved?limit=${PAGE}`, { decode: decodeChatSavedPage })
      if (mine !== seq) return
      items.value = page.items
      hasMore.value = page.has_more
      onEntries(page.items)
    } catch (error) {
      if (mine !== seq) return
      console.warn('Failed to load saved messages:', error)
      items.value = []
      hasMore.value = false
      failed.value = true
    } finally {
      if (mine === seq) loading.value = false
    }
  }

  /** Appends the next older page. */
  async function loadMore() {
    const last = items.value[items.value.length - 1]
    if (!last || loadingMore.value || !hasMore.value) return
    const mine = seq
    loadingMore.value = true
    try {
      const page = await api(`/api/me/saved?limit=${PAGE}&before=${last.message_id}`, { decode: decodeChatSavedPage })
      if (mine !== seq) return
      items.value = [...items.value, ...page.items]
      hasMore.value = page.has_more
      onEntries(page.items)
    } catch (error) {
      console.warn('Failed to load more saved messages:', error)
    } finally {
      loadingMore.value = false
    }
  }

  return { items, hasMore, loading, loadingMore, failed, load, loadMore }
}
