// The history of a wiki page: its revisions, newest first, one chosen revision
// with the change it made, and restoring it as a new revision.
import { computed, ref, watch } from 'vue'
import { caughtErrorMessage } from '../lib/api'
import { confirm } from '../lib/confirm'
import { fetchWikiRevision, fetchWikiRevisions, restoreWikiRevision, type WikiPage, type WikiRevision } from '../lib/wikiApi'
import { isStaleRevision } from '../lib/wikiErrors'
import { goWiki } from '../lib/wikiNav'
import { t } from '../i18n'
import { useToastStore } from '../stores/toast'
import { useWikiStore } from '../stores/wiki'

const PAGE = 50

export function useWikiHistory(page: () => WikiPage) {
  const wiki = useWikiStore()
  const toasts = useToastStore()
  const revisions = ref<WikiRevision[]>([])
  const more = ref(false)
  const loading = ref(true)
  const failed = ref(false)
  const chosen = ref<WikiRevision | null>(null)
  // The text of the revision before the chosen one; null while it loads, '' when there is none (the first, or pruned).
  const previous = ref<string | null>(null)
  // The revision before the chosen one could not be read (pruned or a failed read): no comparison is shown, not a false one.
  const noCompare = ref(false)
  const busy = ref(false)

  const latest = computed(() => revisions.value[0]?.rev ?? page().rev)
  const mayRestore = computed(() => wiki.canEdit && !page().deleted_at && (!page().locked || wiki.canManage))

  async function load(before?: number) {
    loading.value = true
    failed.value = false
    try {
      const got = await fetchWikiRevisions(page().slug, before)
      revisions.value = before ? [...revisions.value, ...got] : got
      more.value = got.length >= PAGE
      if (!before && got[0]) await choose(got[0].rev)
    } catch {
      failed.value = true
    } finally {
      loading.value = false
    }
  }

  // Only the last choice may show: an answer for an earlier click is dropped.
  let choice = 0

  async function choose(rev: number) {
    const mine = ++choice
    chosen.value = null
    previous.value = null
    noCompare.value = false
    try {
      const [current, before] = await Promise.all([
        fetchWikiRevision(page().slug, rev),
        rev > 1 ? fetchWikiRevision(page().slug, rev - 1).catch(() => null) : Promise.resolve(null)
      ])
      if (mine !== choice) return
      chosen.value = current
      noCompare.value = rev > 1 && before === null
      previous.value = before?.content ?? ''
    } catch (error) {
      if (mine !== choice) return
      toasts.error(caughtErrorMessage(error, t('wiki.revisionFailed')))
    }
  }

  async function restore(rev: number) {
    if (busy.value) return
    const ok = await confirm({ title: t('wiki.restoreTitle', { n: rev }), body: t('wiki.restoreBody'), confirmLabel: t('wiki.restore') })
    if (!ok) return
    busy.value = true
    try {
      const next = await restoreWikiRevision(page().slug, rev, { base_rev: page().rev })
      const shown = wiki.isOpen(next.id)
      wiki.applyPage(next, shown)
      toasts.success(t('wiki.restoredRevision', { n: rev }))
      if (shown) void goWiki({ mode: 'page', slug: next.slug }, true)
    } catch (error) {
      toasts.error(isStaleRevision(error) ? t('wiki.restoreStale') : caughtErrorMessage(error, t('wiki.actionFailed')))
      // The page the next try builds on is read again, or every retry would meet the same refusal.
      void wiki.loadPage(page().slug)
      void load()
    } finally {
      busy.value = false
    }
  }

  watch(() => page().slug, () => { void load() }, { immediate: true })

  return { revisions, more, loading, failed, chosen, previous, noCompare, busy, latest, mayRestore, load, choose, restore }
}
