// The sidebar's channel layout with immediate saves (like Discord): a move
// shows at once, is saved with PUT /api/admin/layout, is rolled back with an
// error toast if that fails, and can be undone from a toast for a few seconds.
//
// Saves never interleave. Every PUT carries the whole layout, so while one is
// in flight further moves only replace the queued layout: the latest one
// wins and nothing in between is lost. If a save fails, the layout falls back
// to the last one the server accepted and queued moves are dropped.
//
// Until the last save went through and the channel list was fetched again,
// the pending order is laid over the server data. A channels_changed refetch
// in between therefore brings new names or channels, but can't flash the old
// order back.
import { ref, shallowRef, computed, getCurrentScope, onScopeDispose } from 'vue'
import type { LayoutRequest } from '../types/domain'
import type { ChannelTree } from '../lib/channelTree'
import { api } from '../lib/api'
import { buildChannelTree } from '../lib/channelTree'
import { applyLayoutPayload, toLayoutPayload, samePayload } from '../lib/channelLayout'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'

export const UNDO_MS = 6000

export function useChannelLayout({ undoMs = UNDO_MS }: { undoMs?: number } = {}) {
  const chatStore = useChatStore()
  const toasts = useToastStore()

  const serverTree = computed(() => buildChannelTree(chatStore.categories, chatStore.uncategorized))
  // Payload of the order on screen while it isn't confirmed by a refetch yet.
  const pending = shallowRef<LayoutRequest | null>(null)
  const layout = computed(() => (pending.value ? applyLayoutPayload(serverTree.value, pending.value) : serverTree.value))
  const saving = ref(false)

  let confirmed: LayoutRequest | null = null // last layout the server has (rollback target)
  let queued: LayoutRequest | null = null // next payload to send
  let running = false
  let waiters: ((value: boolean) => void)[] = []
  let seq = 0
  let undoToast: number | null = null

  /**
   * Shows `next` (a tree) at once and saves it. Resolves to true once saved,
   * false if it was rolled back. options.toast: text of the success toast,
   * which offers "Undo"; without it the save is silent.
   */
  function commit(next: ChannelTree, { toast = '' }: { toast?: string } = {}): Promise<boolean> {
    const before = toLayoutPayload(layout.value)
    const payload = toLayoutPayload(next)
    if (samePayload(before, payload)) return Promise.resolve(true)
    if (!running) confirmed = before
    pending.value = payload
    queued = payload
    const id = ++seq
    const done = new Promise<boolean>(resolve => waiters.push(resolve))
    drain()
    return done.then(ok => {
      // Only the latest move offers an undo; it restores the layout before it.
      if (ok && toast && id === seq) offerUndo(toast, before)
      return ok
    })
  }

  async function drain() {
    if (running) return
    running = true
    saving.value = true
    let failure: unknown = null
    while (queued) {
      // Every move made up to now is part of this payload. Laid over the
      // latest server data, channels deleted meanwhile drop out instead of
      // failing the whole save.
      const payload = toLayoutPayload(applyLayoutPayload(serverTree.value, queued))
      const batch = waiters
      queued = null
      waiters = []
      try {
        await api('/api/admin/layout', { method: 'PUT', json: payload })
        confirmed = payload
        batch.forEach(resolve => resolve(true))
      } catch (e) {
        failure = e
        queued = null
        pending.value = confirmed
        ;[...batch, ...waiters].forEach(resolve => resolve(false))
        waiters = []
        break
      }
    }
    running = false
    saving.value = false
    if (failure) toasts.error(t('sidebar.layoutSaveFailed'), { detail: failure instanceof Error ? failure.message : '' })

    // The server broadcasts channels_changed as well; this fetch makes sure the
    // store has the saved order before the overlay goes away.
    await chatStore.fetchChannels()
    if (!running) pending.value = null
  }

  function offerUndo(text: string, before: LayoutRequest) {
    if (undoToast) toasts.dismiss(undoToast)
    undoToast = toasts.success(text, {
      duration: undoMs,
      action: { label: t('common.undo'), onClick: async () => { await undo(before) } }
    })
  }

  async function undo(before: LayoutRequest) {
    undoToast = null
    // Lay the old order over today's data: channels deleted meanwhile drop
    // out instead of failing the whole save.
    const ok = await commit(applyLayoutPayload(layout.value, before))
    if (ok) toasts.success(t('sidebar.layoutRestored'))
    return ok
  }

  function dispose() {
    if (undoToast) toasts.dismiss(undoToast)
    undoToast = null
  }

  if (getCurrentScope()) onScopeDispose(dispose)

  return { layout, saving, commit }
}
