// What the wiki dialog's screens share, kept apart from the navigation so a store can reach it.
import { ref } from 'vue'

/** Set by the editor while it holds text that is not saved. */
export const wikiDirty = ref(false)
/** What the search box holds; the search screen reads it. */
export const wikiQuery = ref('')

/** Sign-out: what the member typed in the search box or left unsaved is not the next member's. */
export function forgetWikiSession(): void {
  wikiQuery.value = ''
  wikiDirty.value = false
}
