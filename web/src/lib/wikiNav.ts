// Moving around the wiki dialog. What an editor holds unsaved is asked about
// before anything leaves it, whichever way it leaves: a page in the tree, the
// close button, Escape.
import { ref, watch } from 'vue'
import { confirm } from './confirm'
import { canGoBackInApp, currentRoute, historyDepth, navigate, setLeaveGuard } from './router'
import { t } from '../i18n'
import { wikiPath, type WikiLocation } from './wikiPaths'

/** Set by the editor while it holds text that is not saved. */
export const wikiDirty = ref(false)
/** What the search box holds; the search screen reads it. */
export const wikiQuery = ref('')

/** Asks whether unsaved text may go; true when there is none or the member says so. */
export async function mayLeaveWiki(): Promise<boolean> {
  if (!wikiDirty.value) return true
  const ok = await confirm({ title: t('wiki.discardTitle'), body: t('wiki.discardBody'), confirmLabel: t('wiki.discard'), danger: true })
  if (ok) wikiDirty.value = false
  return ok
}

// Back and Forward ask too (the router puts the browser back while the question is open).
setLeaveGuard({ dirty: () => wikiDirty.value, ask: mayLeaveWiki })

/** Opens a place in the wiki (a page, its history, a section). */
export async function goWiki(at: WikiLocation, replace = false): Promise<void> {
  if (!(await mayLeaveWiki())) return
  navigate(wikiPath(at), { replace })
}

// The history entry the wiki was opened at. Every page the member visits in the
// wiki is an entry of its own, so closing goes back past all of them at once.
let entry: number | null = null
watch(currentRoute, (route, before) => {
  if (route.view !== 'wiki') entry = null
  else if (!before || before.view !== 'wiki') entry = historyDepth()
}, { immediate: true, flush: 'sync' })

/** Leaves the wiki for where the member came from, however many wiki pages they went through. */
export function closeWiki(): void {
  wikiDirty.value = false
  const first = entry
  if (first !== null && first > 0) window.history.go(first - historyDepth() - 1)
  else if (first === null && canGoBackInApp()) window.history.back()
  else navigate('/', { replace: true })
}

/** Asks the wiki to open from anywhere in the app (the sidebar entry). */
export function openWiki(slug?: string): void {
  navigate(wikiPath(slug ? { mode: 'page', slug } : { mode: 'home' }))
}
