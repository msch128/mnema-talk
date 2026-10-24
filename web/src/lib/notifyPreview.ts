import { ref } from 'vue'
import { api } from './api'
import { decodePushConfig, type PushConfig } from '../types/rest'

/**
 * Whether the in-page notification may show the message text. It follows the
 * member's "show message text" choice and the operator's PUSH_PREVIEWS, like
 * Web Push does. Until the answer is known the text stays hidden.
 */
export const notifyShowsText = ref(false)

export function applyPushConfig(config: Pick<PushConfig, 'preview' | 'preview_allowed'>) {
  notifyShowsText.value = config.preview === true && config.preview_allowed === true
}

export async function loadNotifyPreview() {
  try {
    applyPushConfig(await api('/api/push/config', { decode: decodePushConfig }))
  } catch {
    notifyShowsText.value = false
  }
}
