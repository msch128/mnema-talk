import { ref } from 'vue'
import { api, ApiError } from '../shared/lib/api'
import { decodeChatSavedPage, type ChatSavedMessage } from '../types/rest'

const PAGE = 50
// The largest page the server answers (GET /api/me/saved clamps limit to it).
const MAX_PAGE = 100

/**
 * The newest page in place of what was shown: as many entries as were shown
 * are read again (up to MAX_PAGE), and older entries shown behind the page's
 * last one stay until a later page replaces them, so a reload does not cut a
 * list the member scrolled back to its first page.
 */
function replaceShown(shown: ChatSavedMessage[], page: { items: ChatSavedMessage[]; has_more: boolean }, hadMore: boolean) {
  const last = page.items[page.items.length - 1]
  const at = last ? shown.findIndex(e => e.message_id === last.message_id) : -1
  if (!page.has_more || at < 0 || at === shown.length - 1) return { items: page.items, hasMore: page.has_more }
  const fresh = new Set(page.items.map(e => e.message_id))
  return { items: [...page.items, ...shown.slice(at + 1).filter(e => !fresh.has(e.message_id))], hasMore: hadMore }
}

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

  /** Loads the newest entries again, as many as were shown (see replaceShown); true when this answer is the one shown. */
  async function load(): Promise<boolean> {
    const mine = ++seq
    loading.value = true
    failed.value = false
    const limit = Math.min(Math.max(items.value.length, PAGE), MAX_PAGE)
    try {
      const page = await api(`/api/me/saved?limit=${limit}`, { decode: decodeChatSavedPage })
      if (mine !== seq) return false
      const next = replaceShown(items.value, page, hasMore.value)
      items.value = next.items
      hasMore.value = next.hasMore
      loaded = true
      onEntries(next.items)
      return true
    } catch (error) {
      if (mine !== seq) return false
      console.warn('Failed to load saved messages:', error)
      // A list already on screen stays: a refresh that failed does not make it wrong.
      if (!loaded) {
        items.value = []
        hasMore.value = false
      }
      failed.value = true
      return false
    } finally {
      if (mine === seq) loading.value = false
    }
  }

  /** Appends the next older page; true when it was appended. */
  async function loadMore(): Promise<boolean> {
    const last = items.value[items.value.length - 1]
    if (!last || loadingMore.value || !hasMore.value) return false
    const mine = seq
    loadingMore.value = true
    try {
      const page = await api(`/api/me/saved?limit=${PAGE}&before=${last.message_id}`, { decode: decodeChatSavedPage })
      if (mine !== seq) return false
      const shown = new Set(items.value.map(e => e.message_id))
      items.value = [...items.value, ...page.items.filter(e => !shown.has(e.message_id))]
      hasMore.value = page.has_more
      onEntries(page.items)
      return true
    } catch (error) {
      console.warn('Failed to load more saved messages:', error)
      // The entry the next page starts behind is gone: start over from the newest page.
      if (error instanceof ApiError && error.status === 404) {
        loadingMore.value = false
        await load()
      }
      return false
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
