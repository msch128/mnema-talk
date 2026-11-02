// The state and the saving of the wiki editor: the text being written, whether
// it differs from what was read, the size, and what happens when somebody saved
// meanwhile (the server refuses the save and nothing is overwritten).
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { caughtErrorMessage, isApiError } from '../lib/api'
import { changeWikiPage, createWikiPage, fetchWikiPage, type WikiPage } from '../lib/wikiApi'
import { goWiki, wikiDirty } from '../lib/wikiNav'
import { t } from '../i18n'
import { useWikiStore } from '../stores/wiki'

export const MAX_BYTES = 50 * 1024

const encoder = new TextEncoder()

interface EditorFields { title: string; content: string; summary: string; slug: string; parent: string }

/** Creates the page (page null) or changes it on top of baseRev. */
function submitEditor(page: WikiPage | null, baseRev: number, f: EditorFields): Promise<WikiPage> {
  const summary = f.summary ? { summary: f.summary } : {}
  if (!page) {
    return createWikiPage({ title: f.title, content: f.content, ...(f.slug ? { slug: f.slug } : {}), ...(f.parent ? { parent_id: f.parent } : {}), ...summary })
  }
  return changeWikiPage(page.slug, { base_rev: baseRev, title: f.title, content: f.content, ...summary })
}

export function useWikiEditor(page: WikiPage | null, parentId: string | null = null) {
  const wiki = useWikiStore()
  const title = ref(page?.title ?? '')
  const content = ref(page?.content ?? '')
  const summary = ref('')
  const slug = ref('')
  const parent = ref(parentId ?? '')
  const baseRev = ref(page?.rev ?? 0)
  const error = ref('')
  const busy = ref(false)
  // The page as it is now when somebody saved after this editor was opened.
  const theirs = ref<WikiPage | null>(null)

  const bytes = computed(() => encoder.encode(content.value).length)
  const tooLong = computed(() => bytes.value > MAX_BYTES)
  const dirty = computed(() => title.value !== (page?.title ?? '') || content.value !== (page?.content ?? ''))
  const canSave = computed(() => !busy.value && !tooLong.value && title.value.trim() !== '' && (page ? dirty.value : true))

  watch(dirty, value => { wikiDirty.value = value }, { immediate: true })
  onBeforeUnmount(() => { wikiDirty.value = false })

  function leave(target: WikiPage) {
    wikiDirty.value = false
    void goWiki({ mode: 'page', slug: target.slug }, true)
  }

  /** Saves; true when it went through. A refused save keeps the text and opens the conflict view. */
  async function save(): Promise<boolean> {
    if (!canSave.value) return false
    busy.value = true
    error.value = ''
    try {
      const saved = await submitEditor(page, baseRev.value, { title: title.value, content: content.value, summary: summary.value.trim(), slug: slug.value.trim(), parent: parent.value })
      wiki.applyPage(saved)
      await wiki.loadTree()
      leave(saved)
      return true
    } catch (e) {
      await explain(e)
      return false
    } finally {
      busy.value = false
    }
  }

  async function explain(e: unknown) {
    if (page && isApiError(e) && e.status === 409 && /changed since you opened it/.test(e.message)) {
      try {
        theirs.value = await fetchWikiPage(page.slug)
        return
      } catch { /* shown as the plain failure below */ }
    }
    error.value = caughtErrorMessage(e, t('wiki.saveFailed'))
  }

  /** Writes this text on top of the newer version: a revision like any other, so theirs stays in the history. */
  function saveOnTop(): Promise<boolean> {
    if (!theirs.value) return Promise.resolve(false)
    baseRev.value = theirs.value.rev
    theirs.value = null
    return save()
  }

  function takeTheirs() {
    const latest = theirs.value
    if (!latest) return
    wiki.applyPage(latest)
    leave(latest)
  }

  return { title, content, summary, slug, parent, error, busy, theirs, bytes, tooLong, dirty, canSave, save, saveOnTop, takeTheirs }
}
