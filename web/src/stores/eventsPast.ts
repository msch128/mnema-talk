// Past events of the events dialog: the pages fetched with when=past (newest
// first), kept current by live updates.
import { ref, type Ref } from 'vue'
import { api } from '../lib/api'
import { arrayDecoder } from '../types/validation'
import { isCommunityeventsCommunityEvent, type CommunityeventsCommunityEvent as CommunityEvent } from '../types/rest'

const decodeEvents = arrayDecoder('community events', isCommunityeventsCommunityEvent)

const PAST_PAGE = 30

async function fetchPast(cursor: CommunityEvent | null): Promise<CommunityEvent[]> {
  const q = new URLSearchParams({ when: 'past', limit: String(PAST_PAGE) })
  if (cursor) {
    q.set('before', cursor.start_time)
    q.set('before_id', cursor.id)
  }
  return api(`/api/events?${q}`, { decode: decodeEvents })
}

export function usePastEvents(enabled: Readonly<Ref<boolean>>, error: Ref<string | null>) {
  const fetched = ref<CommunityEvent[]>([])
  const loading = ref(false)
  const loaded = ref(false)
  const hasMore = ref(true)

  /** The first page, or the next one when more is true. */
  async function load(more = false) {
    if (!enabled.value || loading.value || (more && !hasMore.value)) return
    loading.value = true
    try {
      const page = await fetchPast(more ? (fetched.value.at(-1) ?? null) : null)
      fetched.value = more ? [...fetched.value, ...page] : page
      hasMore.value = page.length === PAST_PAGE
      loaded.value = true
    } catch (err) {
      error.value = err instanceof Error ? err.message : 'Failed to load events'
    } finally {
      loading.value = false
    }
  }

  function replace(item: CommunityEvent) {
    const idx = fetched.value.findIndex(e => e.id === item.id)
    if (idx >= 0) fetched.value.splice(idx, 1, item)
  }

  function remove(id: string) {
    fetched.value = fetched.value.filter(e => e.id !== id)
  }

  function reset() {
    fetched.value = []
    loading.value = false
    loaded.value = false
    hasMore.value = true
  }

  return { fetched, loading, loaded, hasMore, load, replace, remove, reset }
}
