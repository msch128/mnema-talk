import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { effectScope, ref, type EffectScope } from 'vue'
import { requireValue } from '../test-fixtures.fixture'
import { stageAttachment, unstageAttachment } from '../lib/attachmentsApi'
import { pendingConfirm } from '../lib/confirm'
import { STAGED_TTL_MS } from '../lib/attachmentTray'
import { usePermissionsStore } from '../stores/permissions'
import { useToastStore } from '../stores/toast'
import { setLocale } from '../i18n'
import { useAttachmentTray } from './useAttachmentTray'
import { viewerState, closeViewer } from './useMediaViewer'

vi.mock('../lib/attachmentsApi', () => ({ stageAttachment: vi.fn(), unstageAttachment: vi.fn(async () => undefined) }))

const png = (name: string, size = 1) => {
  const f = new File(['x'], name, { type: 'image/png' })
  Object.defineProperty(f, 'size', { value: size })
  return f
}
const reply = (id: string) => ({ id, original_filename: id, mime_type: 'image/png', size_bytes: 1, url: `/api/media/${id}`, is_deleted: false })

type Deferred = { name: string; resolve: () => void; reject: (e: Error) => void; progress: (loaded: number, total: number) => void }
let pending: Deferred[]
let scope: EffectScope | undefined
const slot = ref('ch1')

/** Stages every file by hand: tests settle each upload when they choose. */
function deferUploads() {
  vi.mocked(stageAttachment).mockImplementation((_channel, file, opts) => new Promise((resolve, reject) => {
    pending.push({ name: file.name, resolve: () => resolve(reply(`id-${file.name}`)), reject, progress: opts?.onProgress ?? (() => undefined) })
  }))
}

function setup() {
  scope = effectScope()
  return requireValue(scope.run(() => useAttachmentTray(() => ({ slot: slot.value, channelId: 'ch1' }))))
}

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  pending = []
  slot.value = 'ch1'
  vi.mocked(stageAttachment).mockReset()
  vi.mocked(unstageAttachment).mockClear()
})

afterEach(() => {
  scope?.stop()
  pendingConfirm.value = null
  closeViewer()
  vi.useRealTimers()
})

describe('useAttachmentTray', () => {
  it('stages added files in the background, three at a time, with progress', async () => {
    deferUploads()
    const tray = setup()
    tray.add([png('a.png'), png('b.png'), png('c.png'), png('d.png')])
    expect(tray.items.value.map(i => i.status)).toEqual(['uploading', 'uploading', 'uploading', 'queued'])
    requireValue(pending[0]).progress(50, 100)
    expect(requireValue(tray.items.value[0]).progress).toBe(0.5)
    requireValue(pending[0]).resolve()
    await vi.waitFor(() => expect(tray.items.value.map(i => i.status)).toEqual(['ready', 'uploading', 'uploading', 'uploading']))
    expect(tray.uploading.value).toBe(true)
  })

  it('does not post anything: adding only stages', () => {
    deferUploads()
    const tray = setup()
    tray.add([png('a.png')])
    expect(stageAttachment).toHaveBeenCalledTimes(1)
    expect(vi.mocked(stageAttachment).mock.calls[0]?.[0]).toBe('ch1')
  })

  it('holds at most ten files and says so', () => {
    deferUploads()
    const tray = setup()
    tray.add(Array.from({ length: 11 }, (_, i) => png(`${i}.png`)))
    expect(tray.count.value).toBe(10)
    expect(useToastStore().toasts.at(-1)).toMatchObject({ type: 'error', text: 'A message carries at most 10 files.', detail: '1 file was not added.' })
    tray.add([png('more.png')])
    expect(tray.count.value).toBe(10)
  })

  it('refuses a file above the size limit and keeps the rest', () => {
    deferUploads()
    usePermissionsStore().info = { owner: false, permissions: 0, max_upload_mb: 1, role_ids: [], top_position: 0, catalog: [], channels: [] }
    const tray = setup()
    tray.add([png('big.png', 2 * 1024 * 1024), png('ok.png')])
    expect(tray.items.value.map(i => i.file.name)).toEqual(['ok.png'])
    expect(useToastStore().toasts.at(-1)).toMatchObject({ text: 'File is too large (max. 1 MB).', detail: 'big.png is 2 MB.' })
  })

  it('shows a failed upload on its card and uploads it again on retry', async () => {
    deferUploads()
    const tray = setup()
    tray.add([png('a.png')])
    requireValue(pending[0]).reject(new Error('boom'))
    await vi.waitFor(() => expect(requireValue(tray.items.value[0]).status).toBe('error'))
    expect(tray.failed.value).toBe(true)
    expect(requireValue(tray.items.value[0]).error).toBeTruthy()
    tray.retry(requireValue(tray.items.value[0]).key)
    expect(requireValue(tray.items.value[0]).status).toBe('uploading')
    requireValue(pending[1]).resolve()
    await vi.waitFor(() => expect(requireValue(tray.items.value[0]).status).toBe('ready'))
    expect(tray.failed.value).toBe(false)
  })

  it('removes a staged file from the server and a running one by aborting it', async () => {
    deferUploads()
    const tray = setup()
    tray.add([png('a.png'), png('b.png')])
    requireValue(pending[0]).resolve()
    await vi.waitFor(() => expect(requireValue(tray.items.value[0]).status).toBe('ready'))
    tray.remove(requireValue(tray.items.value[0]).key)
    expect(unstageAttachment).toHaveBeenCalledWith('ch1', 'id-a.png')
    const running = requireValue(tray.items.value[0])
    const signal = vi.mocked(stageAttachment).mock.calls[1]?.[2]?.signal
    tray.remove(running.key)
    expect(signal?.aborted).toBe(true)
    expect(tray.count.value).toBe(0)
  })

  it('prepare waits for running uploads and returns the ids in tray order', async () => {
    deferUploads()
    const tray = setup()
    tray.add([png('a.png'), png('b.png')])
    const held = requireValue(tray.hold())
    const ids = held.prepare()
    requireValue(pending[1]).resolve()
    requireValue(pending[0]).resolve()
    expect(await ids).toEqual(['id-a.png', 'id-b.png'])
  })

  it('prepare refuses to send while a file failed, and says why', async () => {
    deferUploads()
    const tray = setup()
    tray.add([png('a.png'), png('b.png')])
    const ids = requireValue(tray.hold()).prepare()
    requireValue(pending[0]).resolve()
    requireValue(pending[1]).reject(new Error('no'))
    expect(await ids).toBeNull()
    expect(useToastStore().toasts.at(-1)?.text).toBe('Some files could not be uploaded. Retry or remove them to send.')
  })

  it('uploads a file again when it was staged too long ago for the server to keep it', async () => {
    vi.useFakeTimers({ toFake: ['Date'] })
    deferUploads()
    const tray = setup()
    tray.add([png('a.png')])
    requireValue(pending[0]).resolve()
    await vi.waitFor(() => expect(requireValue(tray.items.value[0]).status).toBe('ready'))
    vi.setSystemTime(Date.now() + STAGED_TTL_MS + 1000)
    const ids = requireValue(tray.hold()).prepare()
    expect(stageAttachment).toHaveBeenCalledTimes(2)
    requireValue(pending[1]).resolve()
    expect(await ids).toEqual(['id-a.png'])
  })

  it('clears a held tray after a send even when another conversation is open', async () => {
    deferUploads()
    const tray = setup()
    tray.add([png('a.png')])
    const held = requireValue(tray.hold())
    slot.value = 'ch2'
    expect(tray.count.value).toBe(0)
    held.clear()
    slot.value = 'ch1'
    expect(tray.count.value).toBe(0)
  })

  it('keeps each conversation its own files', () => {
    deferUploads()
    const tray = setup()
    tray.add([png('a.png')])
    slot.value = 'thread:t1'
    expect(tray.count.value).toBe(0)
    tray.add([png('b.png'), png('c.png')])
    expect(tray.count.value).toBe(2)
    slot.value = 'ch1'
    expect(tray.items.value.map(i => i.file.name)).toEqual(['a.png'])
  })

  it('Escape on an empty composer asks before removing the files', async () => {
    deferUploads()
    const tray = setup()
    tray.add([png('a.png')])
    const typed = new KeyboardEvent('keydown', { key: 'Escape', cancelable: true })
    expect(tray.onKeydown(typed, 'still typing')).toBe(false)
    const empty = new KeyboardEvent('keydown', { key: 'Escape', cancelable: true })
    expect(tray.onKeydown(empty, ' ')).toBe(true)
    expect(empty.defaultPrevented).toBe(true)
    expect(pendingConfirm.value?.title).toBe('Remove the attached file?')
    requireValue(pendingConfirm.value).resolve(false)
    await Promise.resolve()
    expect(tray.count.value).toBe(1)
    tray.onKeydown(new KeyboardEvent('keydown', { key: 'Escape', cancelable: true }), '')
    requireValue(pendingConfirm.value).resolve(true)
    await vi.waitFor(() => expect(tray.count.value).toBe(0))
  })

  it('Escape does nothing without files', () => {
    const tray = setup()
    expect(tray.onKeydown(new KeyboardEvent('keydown', { key: 'Escape', cancelable: true }), '')).toBe(false)
  })

  it('opens the viewer on the pictures of the tray, without message details', () => {
    deferUploads()
    const tray = setup()
    tray.add([png('a.png'), new File(['x'], 'doc.pdf', { type: 'application/pdf' }), png('b.png')])
    tray.view(requireValue(tray.items.value[2]).key)
    expect(viewerState.open).toBe(true)
    expect(viewerState.items.map(i => i.name)).toEqual(['a.png', 'b.png'])
    expect(viewerState.index).toBe(1)
    expect(viewerState.message).toBeNull()
  })
})
