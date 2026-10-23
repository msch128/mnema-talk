import { createApp, ref } from 'vue'
import { i18nPlugin } from '../i18n'
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
    })
  }
  return mounting
}

/** Opens the search, optionally with a new query line (e.g. "in:general "). */
export function openSearch(prefill?: string, scope: SearchScope = 'channels') {
  searchScope.value = scope
  if (prefill !== undefined) searchText.value = prefill
  return ensureMounted().then(() => { searchOpen.value = true })
}

function isSearchShortcut(e: KeyboardEvent): boolean {
  if (e.altKey || e.shiftKey) return false
  if (e.key?.toLowerCase() !== 'k') return false
  return e.metaKey || e.ctrlKey
}

function onKeydown(e: KeyboardEvent) {
  if (!isSearchShortcut(e)) return
  e.preventDefault()
  ensureMounted().then(() => { searchOpen.value = !searchOpen.value })
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
