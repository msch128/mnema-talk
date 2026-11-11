// Keeps what is written in the wiki editor in the tab's session storage while
// it is unsaved (wiki/lib/wikiDraft.ts), and asks the browser before the tab closes.
import { onBeforeUnmount, onMounted, watch, type Ref } from 'vue'
import type { WikiPage } from '../lib/wikiApi'
import { clearWikiDraft, loadWikiDraft, saveWikiDraft, type WikiDraft } from '../lib/wikiDraft'

const DRAFT_DELAY_MS = 400

/** The draft kept for this editor when it says more than the page does; a draft that equals the page is no draft. */
export function resumableDraft(key: string, page: WikiPage | null): WikiDraft | null {
  const kept = loadWikiDraft(key)
  if (!kept) return null
  const differs = page ? kept.title !== page.title || kept.content !== page.content : kept.title !== '' || kept.content !== ''
  return differs ? kept : null
}

/** Writes the fields to the session a moment after they change, and removes the draft when the editor goes saved or discarded (or on `drop`). */
export function keepWikiDraft(key: string, fields: { [K in keyof WikiDraft]: Ref<WikiDraft[K]> }, dirty: Ref<boolean>, wanted: () => boolean): { drop: () => void } {
  function keep() {
    if (dirty.value) {
      saveWikiDraft(key, {
        title: fields.title.value, content: fields.content.value, summary: fields.summary.value,
        slug: fields.slug.value, parent: fields.parent.value, baseRev: fields.baseRev.value
      })
    } else {
      clearWikiDraft(key)
    }
  }

  let timer: ReturnType<typeof setTimeout> | undefined
  watch(Object.values(fields), () => {
    clearTimeout(timer)
    timer = setTimeout(keep, DRAFT_DELAY_MS)
  })

  // Closing the tab asks first (the text is not saved), and a reload keeps what was typed in the last moments.
  function beforeUnload(event: BeforeUnloadEvent) {
    if (!dirty.value) return
    keep()
    event.preventDefault()
    event.returnValue = ''
  }
  onMounted(() => window.addEventListener('beforeunload', beforeUnload))
  function drop() {
    clearTimeout(timer)
    clearWikiDraft(key)
  }
  // The editor goes away saved or discarded (nothing is `wanted` any more), or because the member lost the
  // right to edit meanwhile: then the unsaved text stays, for the held-draft view and for later.
  onBeforeUnmount(() => {
    window.removeEventListener('beforeunload', beforeUnload)
    clearTimeout(timer)
    if (dirty.value && wanted()) keep()
    else drop()
  })
  return { drop }
}
