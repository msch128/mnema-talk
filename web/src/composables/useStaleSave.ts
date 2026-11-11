// What happens when the server refuses a save because somebody saved the page
// meanwhile: the newer page is read, the editor shows the difference, and the
// writer saves on top of it or drops their text for it. Nothing is overwritten.
import { ref, type Ref } from 'vue'
import { caughtErrorMessage } from '../shared/lib/api'
import { confirm } from '../shared/lib/confirm'
import { fetchWikiPage, type WikiPage } from '../lib/wikiApi'
import { isStaleRevision } from '../lib/wikiErrors'
import { t } from '../i18n'
import { useWikiStore } from '../stores/wiki'

interface StaleSaveParts {
  page: WikiPage | null
  baseRev: Ref<number>
  error: Ref<string>
  save: () => Promise<boolean>
  leave: (target: WikiPage) => void
}

export function useStaleSave({ page, baseRev, error, save, leave }: StaleSaveParts) {
  const wiki = useWikiStore()
  // The page as it is now when somebody saved after this editor was opened.
  const theirs = ref<WikiPage | null>(null)

  /** Says why a save failed: a stale one opens the conflict view, any other shows its message. */
  async function explain(e: unknown) {
    if (page && isStaleRevision(e)) {
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

  /** Drops this text for the newer version; asks first, as it cannot be undone. */
  async function takeTheirs() {
    const latest = theirs.value
    if (!latest) return
    if (!(await confirm({ title: t('wiki.discardTitle'), body: t('wiki.discardBody'), confirmLabel: t('wiki.discard'), danger: true }))) return
    wiki.applyPage(latest)
    leave(latest)
  }

  return { theirs, explain, saveOnTop, takeTheirs }
}
