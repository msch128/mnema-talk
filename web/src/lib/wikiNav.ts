// Moving around the wiki dialog. What an editor holds unsaved is asked about
// before anything leaves it, whichever way it leaves: a page in the tree, the
// close button, Escape.
import { ref } from 'vue'
import { confirm } from './confirm'
import { canGoBackInApp, navigate } from './router'
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

/** Opens a place in the wiki (a page, its history, a section). */
export async function goWiki(at: WikiLocation, replace = false): Promise<void> {
  if (!(await mayLeaveWiki())) return
  navigate(wikiPath(at), { replace })
}

/** Leaves the wiki for where the member came from. */
export function closeWiki(): void {
  wikiDirty.value = false
  if (canGoBackInApp()) window.history.back()
  else navigate('/', { replace: true })
}

/** Asks the wiki to open from anywhere in the app (the sidebar entry). */
export function openWiki(slug?: string): void {
  navigate(wikiPath(slug ? { mode: 'page', slug } : { mode: 'home' }))
}
