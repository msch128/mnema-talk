import { ref } from 'vue'
import { api } from './api'
import { decodePushConfig, type PushConfig } from '../types/rest'

/**
 * Whether the in-page notification may show the message text. It follows the
 * member's "show message text" choice and the operator's PUSH_PREVIEWS, like
 * Web Push does. Until the answer is known the text stays hidden.
 */
export const notifyShowsText = ref(false)

// Counts every change of the setting, so an older answer never overwrites a newer one.
let generation = 0

export function applyPushConfig(config: Pick<PushConfig, 'preview' | 'preview_allowed'>) {
  generation++
  notifyShowsText.value = config.preview === true && config.preview_allowed === true
}

/** Hides the text again, e.g. when the account signs out, until the next account's answer is known. */
export function resetNotifyPreview() {
  generation++
  notifyShowsText.value = false
}

export async function loadNotifyPreview() {
  const mine = ++generation
  let shows = false
  try {
    const config = await api('/api/push/config', { decode: decodePushConfig })
    shows = config.preview === true && config.preview_allowed === true
  } catch { /* the text stays hidden */ }
  if (mine === generation) notifyShowsText.value = shows
}
