import { describe, expect, it, vi } from 'vitest'
import type { TrayItem } from '../lib/attachmentTray'
import { aborts, anyUploadPending, clearAllTrays, clearSlot, itemsOf, release, trays } from './attachmentTrayState'

const item = (key: string): TrayItem => ({
  key, file: new File(['x'], `${key}.png`), channelId: 'ch1', kind: 'image', previewUrl: `blob:${key}`,
  status: 'uploading', progress: 0, attachmentId: null, stagedAt: null, error: null
})

describe('attachmentTrayState', () => {
  it('reports an upload while any tray has a file queued or uploading, and not for finished or failed ones', () => {
    clearAllTrays()
    expect(anyUploadPending()).toBe(false)
    trays.set('a', [{ ...item('done'), status: 'ready' }, { ...item('bad'), status: 'error' }])
    expect(anyUploadPending()).toBe(false)
    trays.set('b', [{ ...item('wait'), status: 'queued' }])
    expect(anyUploadPending()).toBe(true)
    trays.set('b', [item('run')])
    expect(anyUploadPending()).toBe(true)
    clearAllTrays()
    expect(anyUploadPending()).toBe(false)
  })

  it('makes an empty tray on first use', () => {
    expect(itemsOf('fresh')).toEqual([])
    expect(trays.has('fresh')).toBe(true)
  })

  it('release frees the preview and aborts the running upload', () => {
    const revoke = vi.spyOn(URL, 'revokeObjectURL')
    const ctl = new AbortController()
    aborts.set('a', ctl)
    release(item('a'))
    expect(revoke).toHaveBeenCalledWith('blob:a')
    expect(ctl.signal.aborted).toBe(true)
    expect(aborts.has('a')).toBe(false)
    revoke.mockRestore()
  })

  it('release copes with a file without preview', () => {
    expect(() => release({ ...item('b'), previewUrl: null })).not.toThrow()
  })

  it('clears one tray, or all of them', () => {
    itemsOf('s1').push(item('x'))
    itemsOf('s2').push(item('y'))
    clearSlot('s1')
    expect(itemsOf('s1')).toEqual([])
    expect(itemsOf('s2')).toHaveLength(1)
    clearAllTrays()
    expect(trays.size).toBe(0)
  })
})
