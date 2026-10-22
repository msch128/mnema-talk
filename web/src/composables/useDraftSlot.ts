import type { Ref } from 'vue'
import { onBeforeUnmount, watch } from 'vue'
import { useAuthStore } from '../stores/auth'
import { getDraft, setDraft, type DraftReply } from '../lib/drafts'

const SAVE_DELAY_MS = 400

/**
 * Keeps a composer's text and reply target per slot (a channel or thread key):
 * the slot's draft is restored on first use and whenever the key changes, and
 * changes are saved shortly after typing stops. A null key keeps nothing.
 */
export function useDraftSlot(key: () => string | null, text: Ref<string>, reply: Ref<DraftReply | null>) {
  const auth = useAuthStore()
  let timer: ReturnType<typeof setTimeout> | undefined
  let pending: { uid: string; slot: string } | null = null

  function flush() {
    clearTimeout(timer)
    const target = pending
    pending = null
    // After sign-out nothing may be written back.
    if (!target || auth.user?.id !== target.uid) return
    const to = reply.value
    setDraft(target.uid, target.slot, { text: text.value, reply: to ? { id: to.id, display_name: to.display_name, username: to.username } : null })
  }

  function load() {
    const uid = auth.user?.id
    const slot = key()
    const draft = uid && slot ? getDraft(uid, slot) : null
    text.value = draft?.text ?? ''
    reply.value = draft?.reply ?? null
  }

  // Sync: the old slot is saved before the composer's state is replaced.
  watch(key, () => { flush(); load() }, { flush: 'sync' })
  watch([text, reply], () => {
    const uid = auth.user?.id
    const slot = key()
    if (!uid || !slot) return
    pending = { uid, slot }
    clearTimeout(timer)
    timer = setTimeout(flush, SAVE_DELAY_MS)
  }, { flush: 'sync' })
  window.addEventListener('pagehide', flush)
  onBeforeUnmount(() => { window.removeEventListener('pagehide', flush); flush() })
  load()
}
