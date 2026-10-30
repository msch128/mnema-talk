// The files waiting to be sent with the message being written. Adding a file
// stages it on the server in the background (a few at a time, with progress);
// sending then only lists their ids. One tray per composer slot (a channel, a
// thread), so the files stay with the conversation while another one is open.
import { computed } from 'vue'
import { caughtErrorMessage } from '../lib/api'
import { stageAttachment, unstageAttachment } from '../lib/attachmentsApi'
import { kindOf, MAX_ATTACHMENTS, PARALLEL_UPLOADS, planAdd, STAGED_TTL_MS, type TrayItem } from '../lib/attachmentTray'
import { confirm } from '../lib/confirm'
import { usePermissionsStore } from '../stores/permissions'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'
import { openViewer } from './useMediaViewer'
import { aborts, clearSlot, itemsOf, release } from './attachmentTrayState'

export interface TrayTarget {
  /** Where the tray lives: a channel id, or `thread:<id>`. */
  slot: string
  /** The channel the files upload to. */
  channelId: string
}

let waiters: Array<() => void> = []

const isPending = (item: TrayItem) => item.status === 'queued' || item.status === 'uploading'

function wake() {
  const ready = waiters
  waiters = []
  for (const resolve of ready) resolve()
}

async function upload(slot: string, item: TrayItem) {
  const ctl = new AbortController()
  aborts.set(item.key, ctl)
  item.status = 'uploading'
  item.progress = 0
  item.error = null
  try {
    const staged = await stageAttachment(item.channelId, item.file, {
      signal: ctl.signal,
      onProgress: (loaded, total) => { item.progress = total ? loaded / total : 0 }
    })
    if (!itemsOf(slot).some(i => i.key === item.key)) {
      // Removed while it was on its way: the staged file has no owner any more.
      void unstageAttachment(item.channelId, staged.id).catch(() => undefined)
      return
    }
    item.attachmentId = staged.id
    item.stagedAt = Date.now()
    item.progress = 1
    item.status = 'ready'
  } catch (err) {
    if (ctl.signal.aborted) return
    item.status = 'error'
    item.error = caughtErrorMessage(err, t('chat.uploadFailed'))
  } finally {
    aborts.delete(item.key)
    pump(slot)
    wake()
  }
}

/** Starts queued uploads while fewer than PARALLEL_UPLOADS run. */
function pump(slot: string) {
  const items = itemsOf(slot)
  let running = items.filter(i => i.status === 'uploading').length
  for (const item of items) {
    if (running >= PARALLEL_UPLOADS) return
    if (item.status !== 'queued') continue
    running++
    void upload(slot, item)
  }
}

const slotPending = (slot: string) => itemsOf(slot).some(isPending)

/** Resolves when no file of the tray is waiting for or in its upload. */
async function settle(slot: string) {
  while (slotPending(slot)) await new Promise<void>(resolve => waiters.push(resolve))
}

/**
 * Gets a tray ready to send: files staged long ago (the server would have
 * dropped them) upload again, running uploads are awaited, and the ids come
 * back in tray order. Null when a file failed; its card shows the error.
 */
async function prepareSlot(slot: string): Promise<string[] | null> {
  const items = itemsOf(slot)
  if (!items.length) return []
  const now = Date.now()
  for (const item of items) {
    if (item.status === 'ready' && item.stagedAt !== null && now - item.stagedAt > STAGED_TTL_MS) {
      item.status = 'queued'
      item.attachmentId = null
      item.stagedAt = null
    }
  }
  pump(slot)
  await settle(slot)
  const ids = items.map(i => (i.status === 'ready' ? i.attachmentId : null))
  if (ids.every((id): id is string => id !== null)) return ids
  useToastStore().error(t('chat.attachFailedSend'))
  return null
}

/** Tells the member which of the offered files did not make it into the tray, and why. */
function explainRefusals(plan: ReturnType<typeof planAdd>, maxMb: number | null) {
  const toasts = useToastStore()
  const first = plan.tooLarge[0]
  if (first) {
    toasts.error(t('chat.uploadTooLarge', { max: maxMb }), { detail: t('chat.uploadTooLargeDetail', { name: first.name, size: Math.ceil(first.size / (1024 * 1024)) }) })
  }
  if (plan.overCount.length) toasts.error(t('chat.attachLimit', { max: MAX_ATTACHMENTS }), { detail: t('chat.attachSkippedN', { count: plan.overCount.length }) })
  if (plan.overTotal.length) toasts.error(t('chat.attachTotalLimit'), { detail: t('chat.attachSkippedN', { count: plan.overTotal.length }) })
}

/** Opens the viewer on the pictures of a tray, at the one with this key. They have no message to show. */
function viewImages(items: readonly TrayItem[], key: string) {
  const images = items.filter(i => i.kind === 'image' && i.previewUrl)
  const index = images.findIndex(i => i.key === key)
  if (index < 0) return
  openViewer({
    items: images.map(i => ({ src: i.previewUrl as string, name: i.file.name, sizeBytes: i.file.size, mimeType: i.file.type })),
    index
  })
}

/** A tray pinned to the conversation it was taken from, for a send that may outlast a channel switch. */
export interface HeldTray {
  prepare: () => Promise<string[] | null>
  clear: () => void
}

export function useAttachmentTray(target: () => TrayTarget | null) {
  const permissions = usePermissionsStore()

  const items = computed(() => { const tg = target(); return tg ? itemsOf(tg.slot) : [] })
  const count = computed(() => items.value.length)
  const uploading = computed(() => items.value.some(isPending))
  const failed = computed(() => items.value.some(i => i.status === 'error'))

  function add(files: readonly File[]) {
    const tg = target()
    if (!tg || !files.length) return
    const held = itemsOf(tg.slot)
    const plan = planAdd(held.length, held.reduce((sum, i) => sum + i.file.size, 0), files, permissions.maxUploadMb)
    explainRefusals(plan, permissions.maxUploadMb)
    for (const file of plan.accept) {
      held.push({
        key: crypto.randomUUID(), file, channelId: tg.channelId, kind: kindOf(file),
        previewUrl: URL.createObjectURL(file), status: 'queued', progress: 0, attachmentId: null, stagedAt: null, error: null
      })
    }
    pump(tg.slot)
  }

  function find(key: string) {
    const tg = target()
    return tg ? itemsOf(tg.slot).find(i => i.key === key) : undefined
  }

  function remove(key: string) {
    const tg = target()
    const item = find(key)
    if (!tg || !item) return
    const list = itemsOf(tg.slot)
    list.splice(list.indexOf(item), 1)
    release(item)
    if (item.attachmentId) void unstageAttachment(item.channelId, item.attachmentId).catch(() => undefined)
    pump(tg.slot)
    wake()
  }

  function retry(key: string) {
    const tg = target()
    const item = find(key)
    if (!tg || !item || item.status !== 'error') return
    item.status = 'queued'
    item.error = null
    item.attachmentId = null
    pump(tg.slot)
  }

  /** Esc on an empty composer: after a confirmation, removes every file. */
  async function discardAll() {
    const tg = target()
    if (!tg || !count.value) return
    const ok = await confirm({
      title: t('chat.attachDiscardTitle', { count: count.value }), body: t('chat.attachDiscardBody'),
      confirmLabel: t('chat.attachDiscard'), danger: true
    })
    if (!ok || target()?.slot !== tg.slot) return
    for (const item of [...itemsOf(tg.slot)]) remove(item.key)
  }

  const view = (key: string) => viewImages(items.value, key)

  /** Escape in the composer: with nothing typed it offers to drop the tray. */
  function onKeydown(e: KeyboardEvent, text: string) {
    if (e.key !== 'Escape' || !count.value || text.trim()) return false
    e.preventDefault()
    void discardAll()
    return true
  }

  /** Pins the tray of the conversation that is open now. */
  function hold(): HeldTray | null {
    const tg = target()
    return tg ? { prepare: () => prepareSlot(tg.slot), clear: () => clearSlot(tg.slot) } : null
  }

  return { items, count, uploading, failed, add, remove, retry, discardAll, hold, view, onKeydown }
}

export type AttachmentTray = ReturnType<typeof useAttachmentTray>
