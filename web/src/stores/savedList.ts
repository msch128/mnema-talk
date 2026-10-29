import { ref } from 'vue'
import { api, ApiError } from '../lib/api'
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
  // Whether a page has been shown since the last reset.
  let loaded = false

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
      loaded = true
      onEntries(page.items)
    } catch (error) {
      if (mine !== seq) return
      console.warn('Failed to load saved messages:', error)
      // A list already on screen stays: a refresh that failed does not make it wrong.
      if (!loaded) {
        items.value = []
        hasMore.value = false
      }
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
      // The entry the next page starts behind is gone: start over from the newest page.
      if (error instanceof ApiError && error.status === 404) {
        loadingMore.value = false
        await load()
      }
    } finally {
      loadingMore.value = false
    }
  }

  /** Forgets the list (sign-out); a page still on its way is dropped. */
  function reset() {
    ++seq
    loaded = false
    items.value = []
    hasMore.value = false
    loading.value = false
    loadingMore.value = false
    failed.value = false
  }

  return { items, hasMore, loading, loadingMore, failed, load, loadMore, reset }
}
