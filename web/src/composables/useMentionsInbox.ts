// The mentions inbox: messages that mention the reader (by name, @all/@here
// or through a role) or reply to them, newest first, in the channels they can
// view (GET /api/mentions). Paged backwards with the id of the oldest entry.
import { ref } from 'vue'
import type { Message } from '../types/domain'
import { decodeChatMentionsPage } from '../types/rest'
import { api } from '../lib/api'

const PAGE = 20

export function useMentionsInbox() {
  const items = ref<Message[]>([])
  const hasMore = ref(false)
  const loading = ref(false)
  const loadingMore = ref(false)
  const failed = ref(false)
  const loadMoreFailed = ref(false)
  let seq = 0

  async function fetchPage(before?: string) {
    const params = new URLSearchParams({ limit: String(PAGE) })
    if (before) params.set('before', before)
    return api(`/api/mentions?${params.toString()}`, { decode: decodeChatMentionsPage })
  }

  /** Loads the newest page, replacing what was shown. */
  async function load() {
    const mine = ++seq
    loading.value = true
    failed.value = false
    loadMoreFailed.value = false
    try {
      const page = await fetchPage()
      if (mine !== seq) return
      items.value = page.messages
      hasMore.value = page.has_more
    } catch (e) {
      if (mine !== seq) return
      console.warn('Failed to load mentions:', e)
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
    loadMoreFailed.value = false
    try {
      const page = await fetchPage(last.id)
      if (mine !== seq) return
      items.value = [...items.value, ...page.messages]
      hasMore.value = page.has_more
    } catch (e) {
      console.warn('Failed to load more mentions:', e)
      if (mine === seq) loadMoreFailed.value = true
    } finally {
      loadingMore.value = false
    }
  }

  return { items, hasMore, loading, loadingMore, failed, loadMoreFailed, load, loadMore }
}
