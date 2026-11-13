import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { effectScope, nextTick, type EffectScope } from 'vue'
import { createPinia, setActivePinia } from 'pinia'

const report = vi.hoisted(() => vi.fn())
const host = vi.hoisted(() => ({ reports: false }))
vi.mock('../../shared/lib/platform', async importOriginal => {
  const real = await importOriginal<typeof import('../../shared/lib/platform')>()
  return { ...real, platform: { ...real.platform, reportBusy: report, supportsBusyReport: () => host.reports } }
})

import { useDesktopBusy } from './useDesktopBusy'
import { useVoiceStore } from '../../stores/voice'
import { channelDraftKey, clearAllDrafts, setDraft } from '../../chat/lib/drafts'
import { clearAllTrays, trays } from '../../chat/composables/attachmentTrayState'
import type { TrayItem } from '../../chat/lib/attachmentTray'

const file = (status: TrayItem['status']): TrayItem => ({
  key: 'k', file: new File(['x'], 'a.png'), channelId: 'c1', kind: 'image', previewUrl: null,
  status, progress: 0, attachmentId: null, stagedAt: null, error: null
})

let scope: EffectScope
beforeEach(() => {
  setActivePinia(createPinia())
  localStorage.clear()
  clearAllDrafts()
  clearAllTrays()
  report.mockClear()
  host.reports = true
})
afterEach(() => scope.stop())

function start() {
  scope = effectScope()
  scope.run(() => useDesktopBusy())
}

describe('useDesktopBusy', () => {
  it('reports nothing to a host that did not announce busy', async () => {
    host.reports = false
    start()
    useVoiceStore().isConnected = true
    await nextTick()
    expect(report).not.toHaveBeenCalled()
  })

  it('reports an idle page right away', () => {
    start()
    expect(report).toHaveBeenCalledTimes(1)
    expect(report).toHaveBeenLastCalledWith({ draft: false, upload: false, talk: false })
  })

  it('reports unsent text of any channel and clears it when the draft is gone', async () => {
    start()
    setDraft('u1', channelDraftKey('c9'), { text: 'half a thought', reply: null })
    await nextTick()
    expect(report).toHaveBeenLastCalledWith({ draft: true, upload: false, talk: false })
    setDraft('u1', channelDraftKey('c9'), null)
    await nextTick()
    expect(report).toHaveBeenLastCalledWith({ draft: false, upload: false, talk: false })
  })

  it('reports a running upload until it is done', async () => {
    start()
    trays.set('c1', [file('uploading')])
    await nextTick()
    expect(report).toHaveBeenLastCalledWith({ draft: false, upload: true, talk: false })
    const [item] = trays.get('c1') as TrayItem[]
    if (item) item.status = 'ready'
    await nextTick()
    expect(report).toHaveBeenLastCalledWith({ draft: false, upload: false, talk: false })
  })

  it('reports being in a Talk', async () => {
    start()
    useVoiceStore().isConnected = true
    await nextTick()
    expect(report).toHaveBeenLastCalledWith({ draft: false, upload: false, talk: true })
  })

  it('reports again only when a flag changes', async () => {
    start()
    setDraft('u1', channelDraftKey('c1'), { text: 'a', reply: null })
    await nextTick()
    const calls = report.mock.calls.length
    setDraft('u1', channelDraftKey('c1'), { text: 'a longer draft', reply: null })
    setDraft('u1', channelDraftKey('c2'), { text: 'another', reply: null })
    trays.set('c1', [file('ready')])
    await nextTick()
    expect(report).toHaveBeenCalledTimes(calls)
  })
})
