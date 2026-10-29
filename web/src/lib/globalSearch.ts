import { createApp, ref } from 'vue'
import { i18nPlugin } from '../i18n'
import { parseSearchQuery, serializeSearchQuery } from './searchQuery'
import { tooltip } from '../directives/tooltip'

// Ctrl/⌘+K opens the message search from every view while signed in.
// App.vue is not involved: the chat store installs the shortcut after login,
// and the modal is mounted lazily in its own tiny Vue app (pinia is shared
// through the active instance).

export const searchOpen = ref(false)
/** The query line; kept while the session lasts so reopening shows the last search. */
export const searchText = ref('')
/** What the search looks through: the channels, or (dm) the member's direct messages. */
export type SearchScope = 'channels' | 'dm'
export const searchScope = ref<SearchScope>('channels')

let installed = false
let mounting: Promise<void> | null = null

function ensureMounted() {
  if (!mounting) {
    mounting = import('../components/GlobalSearch.vue').then(({ default: GlobalSearch }) => {
      const host = document.createElement('div')
      document.body.appendChild(host)
      createApp(GlobalSearch).use(i18nPlugin).directive('tooltip', tooltip).mount(host)
    }).catch((error: unknown) => {
      // Forget the failed load (a network blip, a chunk swapped by a deploy) so the next try fetches again.
      mounting = null
      throw error
    })
  }
  return mounting
}

/**
 * Merges a prefill into the kept query line: its filters replace the filters
 * of the same kind, its text (if any) replaces the text. "in:general " thus
 * adds or swaps the channel and keeps the rest, like reopening with Ctrl+K.
 */
function mergePrefill(kept: string, prefill: string): string {
  const old = parseSearchQuery(kept)
  const add = parseSearchQuery(prefill)
  const text = add.text || old.text
  const line = serializeSearchQuery({ text, filters: { ...old.filters, ...add.filters } })
  // A trailing space finishes the last filter, so it becomes a chip.
  return text ? line : `${line} `
}

/** Opens the search, optionally adding a prefill such as "in:general " to the last query. */
export function openSearch(prefill?: string, scope: SearchScope = 'channels') {
  searchScope.value = scope
  if (prefill !== undefined) searchText.value = mergePrefill(searchText.value, prefill)
  return ensureMounted().then(() => { searchOpen.value = true }, failed)
}

/** The lazy modal could not be loaded; the next Ctrl/Cmd+K tries again. */
function failed(error: unknown) {
  console.error('search: could not load the search', error)
}

function isSearchShortcut(e: KeyboardEvent): boolean {
  if (e.altKey || e.shiftKey) return false
  if (e.key?.toLowerCase() !== 'k') return false
  return e.metaKey || e.ctrlKey
}

function onKeydown(e: KeyboardEvent) {
  if (!isSearchShortcut(e)) return
  e.preventDefault()
  ensureMounted().then(() => { searchOpen.value = !searchOpen.value }, failed)
}

export function installGlobalSearch() {
  if (installed || typeof window === 'undefined') return
  installed = true
  window.addEventListener('keydown', onKeydown)
}

export function uninstallGlobalSearch() {
  if (!installed) return
  installed = false
  searchOpen.value = false
  searchText.value = ''
  searchScope.value = 'channels'
  window.removeEventListener('keydown', onKeydown)
}
