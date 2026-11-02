// What the wiki dialog holds: the page tree and the page on screen. The server
// decides everything (doc/wiki.md); this only keeps what was read.
import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { isApiError } from '../lib/api'
import { fetchWikiPage, fetchWikiTree, type WikiPage, type WikiPageSummary } from '../lib/wikiApi'
import { buildTree } from '../lib/wikiTree'
import { usePermissionsStore } from './permissions'

export type WikiPageState = 'idle' | 'loading' | 'ready' | 'missing' | 'denied' | 'failed'

/** What the tree keeps of a page. */
function summaryOf(p: WikiPage): WikiPageSummary {
  return { id: p.id, slug: p.slug, title: p.title, parent_id: p.parent_id, position: p.position, locked: p.locked, updated_at: p.updated_at, ...(p.deleted_at ? { deleted_at: p.deleted_at } : {}) }
}

export const useWikiStore = defineStore('wiki', () => {
  const permissions = usePermissionsStore()
  const pages = ref<WikiPageSummary[]>([])
  const treeLoaded = ref(false)
  const treeFailed = ref(false)
  const page = ref<WikiPage | null>(null)
  const pageState = ref<WikiPageState>('idle')
  let treeSeq = 0
  let pageSeq = 0

  const canView = computed(() => permissions.can('view_wiki'))
  const canEdit = computed(() => permissions.can('edit_wiki'))
  const canManage = computed(() => permissions.can('manage_wiki'))
  const live = computed(() => pages.value.filter(p => !p.deleted_at))
  const tree = computed(() => buildTree(live.value))
  const deleted = computed(() => pages.value.filter(p => p.deleted_at))
  const recent = computed(() => [...live.value].sort((a, b) => b.updated_at.localeCompare(a.updated_at)).slice(0, 8))

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
      pageState.value = isApiError(error) && error.status === 404 ? 'missing' : isApiError(error) && error.status === 403 ? 'denied' : 'failed'
      return null
    }
  }

  /** Takes a page the server just answered as the one on screen, and updates its place in the tree. */
  function applyPage(next: WikiPage): void {
    pageSeq++
    page.value = next
    pageState.value = 'ready'
    const summary = summaryOf(next)
    const at = pages.value.findIndex(p => p.id === next.id)
    pages.value = at < 0 ? [...pages.value, summary] : pages.value.map((p, i) => (i === at ? summary : p))
  }

  function clearPage(): void {
    pageSeq++
    page.value = null
    pageState.value = 'idle'
  }

  function reset(): void {
    treeSeq++
    pageSeq++
    pages.value = []
    treeLoaded.value = false
    treeFailed.value = false
    clearPage()
  }

  return { pages, treeLoaded, treeFailed, page, pageState, canView, canEdit, canManage, live, tree, deleted, recent, loadTree, loadPage, applyPage, clearPage, reset }
})
