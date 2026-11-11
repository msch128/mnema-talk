// What the wiki dialog holds: the page tree and the page on screen. The server
// decides everything (doc/wiki.md); this only keeps what was read.
import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { isApiError } from '../shared/lib/api'
import { fetchWikiPage, fetchWikiTree, type WikiPage, type WikiPageSummary } from '../lib/wikiApi'
import { wikiViews } from '../lib/wikiTree'
import { forgetWikiSession } from '../lib/wikiState'
import { usePermissionsStore } from './permissions'

export type WikiPageState = 'idle' | 'loading' | 'ready' | 'missing' | 'denied' | 'failed'

/** What the tree keeps of a page. */
function summaryOf(p: WikiPage): WikiPageSummary {
  return { id: p.id, slug: p.slug, title: p.title, parent_id: p.parent_id, position: p.position, locked: p.locked, updated_at: p.updated_at, ...(p.deleted_at ? { deleted_at: p.deleted_at } : {}) }
}

/** How a failed read of a page is told to the screen. */
function failureState(error: unknown): WikiPageState {
  if (isApiError(error) && error.status === 404) return 'missing'
  return isApiError(error) && error.status === 403 ? 'denied' : 'failed'
}

/** The list with the page's summary put in its place (added when it is new). */
function withPage(list: WikiPageSummary[], summary: WikiPageSummary): WikiPageSummary[] {
  return list.some(p => p.id === summary.id) ? list.map(p => (p.id === summary.id ? summary : p)) : [...list, summary]
}

export const useWikiStore = defineStore('wiki', () => {
  const permissions = usePermissionsStore()
  const pages = ref<WikiPageSummary[]>([])
  const [treeLoaded, treeFailed] = [ref(false), ref(false)]
  const page = ref<WikiPage | null>(null)
  const pageState = ref<WikiPageState>('idle')
  let [treeSeq, pageSeq] = [0, 0]

  const canView = computed(() => permissions.can('view_wiki'))
  const canEdit = computed(() => permissions.can('edit_wiki'))
  const canManage = computed(() => permissions.can('manage_wiki') && permissions.can('edit_wiki')) // the server asks for both
  const { live, tree, deleted, recent } = wikiViews(pages)

  /** Reads the tree again; managers also get the deleted pages. */
  async function loadTree(): Promise<void> {
    const seq = ++treeSeq
    try {
      const list = await fetchWikiTree(canManage.value)
      if (seq !== treeSeq) return
      pages.value = list
      treeFailed.value = false
    } catch (error) {
      if (seq !== treeSeq) return
      treeFailed.value = true
      if (!isApiError(error)) console.warn('Failed to read the wiki:', error)
    } finally {
      if (seq === treeSeq) treeLoaded.value = true
    }
  }

  /** Reads a page; `pageState` says how it went. A stale answer is dropped. */
  async function loadPage(slug: string): Promise<WikiPage | null> {
    const seq = ++pageSeq
    pageState.value = 'loading'
    try {
      const read = await fetchWikiPage(slug)
      if (seq !== pageSeq) return null
      page.value = read
      pageState.value = 'ready'
      return read
    } catch (error) {
      if (seq !== pageSeq) return null
      page.value = null
      pageState.value = failureState(error)
      return null
    }
  }

  /**
   * Takes a page the server just answered, and updates its place in the tree.
   * It becomes the one on screen unless `show` is false: an answer that comes
   * after the member went elsewhere must not pull them back.
   */
  function applyPage(next: WikiPage, show = true): void {
    if (show) {
      pageSeq++
      page.value = next
      pageState.value = 'ready'
    }
    pages.value = withPage(pages.value, summaryOf(next))
  }

  function clearPage(): void {
    pageSeq++
    page.value = null
    pageState.value = 'idle'
  }

  function reset(): void {
    treeSeq++
    pages.value = []
    treeLoaded.value = treeFailed.value = false
    forgetWikiSession()
    clearPage()
  }

  const isOpen = (id: string): boolean => page.value?.id === id

  return { isOpen, pages, treeLoaded, treeFailed, page, pageState, canView, canEdit, canManage, live, tree, deleted, recent, loadTree, loadPage, applyPage, clearPage, reset }
})
