// The post list of a forum: filters, search, paging, live refresh and which
// posts are unread. ForumArea shows it; this owns the requests.
import { ref, watch, toValue, onMounted, onBeforeUnmount } from 'vue'
import type { MaybeRefOrGetter } from 'vue'
import type { Message } from '../types/domain'
import { fetchForumPosts, fetchUnreadForumPosts } from '../lib/forumApi'
import { caughtErrorMessage } from '../lib/api'
import { useToastStore } from '../stores/toast'
import { useChatStore } from '../stores/chat'
import { t } from '../i18n'

// The server's default page size; a full page of unpinned posts means there may be older ones.
const PAGE_SIZE = 50
const SEARCH_DEBOUNCE_MS = 300
const REFRESH_DEBOUNCE_MS = 400

export type ForumSort = 'activity' | 'newest'

interface ListQuery { sort: ForumSort; tagId: string | undefined; search: string | undefined }

// Pinned posts lead the first page, so only the others say whether a page was full.
const unpinned = (page: Message[]) => page.filter(p => !p.is_pinned).length

/** Which posts of the forum the member has not read (the open post counts as read). */
function useForumUnread(channelId: MaybeRefOrGetter<string>, openPostId: MaybeRefOrGetter<string | undefined>) {
  const unread = ref<ReadonlySet<string>>(new Set())

  async function load() {
    const id = toValue(channelId)
    if (!id) return
    try {
      const ids = await fetchUnreadForumPosts(id)
      if (id === toValue(channelId)) unread.value = new Set(ids.filter(postId => postId !== toValue(openPostId)))
    } catch (err) {
      console.warn('Failed to load unread forum posts:', err)
    }
  }

  function markRead(postId: string) {
    if (!unread.value.has(postId)) return
    const next = new Set(unread.value)
    next.delete(postId)
    unread.value = next
  }

  return { unread, load, markRead }
}

/** The pages of the list: first page, refresh, older pages. An answer to a request that was superseded is dropped. */
function useForumPages(channelId: MaybeRefOrGetter<string>, query: () => ListQuery) {
  const toasts = useToastStore()
  const posts = ref<Message[]>([])
  const isLoading = ref(false)
  const isLoadingMore = ref(false)
  const hasMore = ref(false)
  const error = ref('')
  let seq = 0
  let loadedSearch = ''

  async function load() {
    const id = toValue(channelId)
    if (!id) return
    const mine = ++seq
    loadedSearch = query().search ?? ''
    isLoading.value = true
    isLoadingMore.value = false
    error.value = ''
    try {
      const page = await fetchForumPosts(id, query())
      if (mine !== seq) return
      posts.value = page
      hasMore.value = unpinned(page) >= PAGE_SIZE
    } catch (err) {
      if (mine !== seq) return
      posts.value = []
      hasMore.value = false
      error.value = caughtErrorMessage(err, t('forum.loadFailed'))
    } finally {
      if (mine === seq) isLoading.value = false
    }
  }

  /** Reloads what is on screen without the loading state, keeping the pages already loaded. */
  async function refresh() {
    const id = toValue(channelId)
    if (!id || isLoading.value) return
    const mine = ++seq
    // A load more that this refresh supersedes will not clear its own flag.
    isLoadingMore.value = false
    try {
      const limit = Math.min(100, Math.max(PAGE_SIZE, unpinned(posts.value)))
      const page = await fetchForumPosts(id, { ...query(), limit })
      if (mine !== seq) return
      posts.value = page
      hasMore.value = unpinned(page) >= limit
    } catch (err) {
      console.warn('Failed to refresh forum posts:', err)
    }
  }

  async function loadMore() {
    const id = toValue(channelId)
    const last = [...posts.value].reverse().find(p => !p.is_pinned)
    if (!id || !last || isLoadingMore.value) return
    const mine = seq
    isLoadingMore.value = true
    try {
      const q = query()
      const page = await fetchForumPosts(id, { ...q, before: last.id, beforeActivity: q.sort === 'activity' ? last.last_activity_at : undefined })
      if (mine !== seq) return
      const known = new Set(posts.value.map(p => p.id))
      posts.value = [...posts.value, ...page.filter(p => !known.has(p.id))]
      hasMore.value = unpinned(page) >= PAGE_SIZE
    } catch (err) {
      if (mine !== seq) return
      toasts.error(caughtErrorMessage(err, t('forum.loadFailed')))
    } finally {
      if (mine === seq) isLoadingMore.value = false
    }
  }

  const add = (post: Message) => { posts.value = [post, ...posts.value.filter(p => p.id !== post.id)] }
  return { posts, isLoading, isLoadingMore, hasMore, error, load, refresh, loadMore, add, searchLoaded: () => loadedSearch }
}

export function useForumPosts(channelId: MaybeRefOrGetter<string>, openPostId: MaybeRefOrGetter<string | undefined>) {
  const chatStore = useChatStore()
  const sort = ref<ForumSort>('activity')
  const tagId = ref<string | null>(null)
  const search = ref('')
  const trimmed = () => search.value.trim()
  const pages = useForumPages(channelId, () => ({ sort: sort.value, tagId: tagId.value ?? undefined, search: trimmed() || undefined }))
  const unread = useForumUnread(channelId, openPostId)
  let searchTimer: ReturnType<typeof setTimeout> | undefined
  let refreshTimer: ReturnType<typeof setTimeout> | undefined

  const load = async () => { await pages.load(); void unread.load() }
  const refresh = async () => { await pages.refresh(); void unread.load() }

  // A new channel, sort or tag starts the list over; typing in the search waits for a pause.
  watch(() => toValue(channelId), () => {
    clearTimeout(searchTimer)
    search.value = ''
    // Clearing a tag filter reloads through the filter watcher; load once.
    if (tagId.value !== null) tagId.value = null
    else void load()
  })
  watch([sort, tagId], () => { void load() })
  watch(search, () => {
    clearTimeout(searchTimer)
    searchTimer = setTimeout(() => { if (trimmed() !== pages.searchLoaded()) void load() }, SEARCH_DEBOUNCE_MS)
  })

  // Something in this forum changed (a post, a reply, a pin, a tag): look again soon.
  watch(() => chatStore.forumPostsSignal, signal => {
    if (signal?.channelId !== toValue(channelId)) return
    clearTimeout(refreshTimer)
    refreshTimer = setTimeout(() => { void refresh() }, REFRESH_DEBOUNCE_MS)
  })
  watch(() => chatStore.pinsSignal, signal => {
    if (signal?.channelId === toValue(channelId)) void refresh()
  })

  onMounted(() => { void load() })
  onBeforeUnmount(() => {
    clearTimeout(searchTimer)
    clearTimeout(refreshTimer)
  })

  return { ...pages, unread: unread.unread, markRead: unread.markRead, sort, tagId, search, load, refresh }
}
