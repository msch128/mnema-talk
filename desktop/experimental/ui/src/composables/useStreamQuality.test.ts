import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'
import { useStreamQuality } from './useStreamQuality'
import { useVoiceStore } from '../stores/voice'
import { requireValue } from '../test-fixtures.fixture'
import type { ContextMenuItem } from '../components/menuTypes'
import type { MediaStat } from '../lib/mediaStats'

const report = (bytesSent = 0): MediaStat[] => [
  { id: 'codec', type: 'codec', mimeType: 'video/VP8' },
  { id: 'sender', type: 'outbound-rtp', kind: 'video', codecId: 'codec', timestamp: Date.now(), bytesSent, framesPerSecond: 30, powerEfficientEncoder: true, qualityLimitationReason: 'cpu' }
]
function submenu(items: ContextMenuItem[], id: string) {
  const item = requireValue(items.find(item => item.id === id))
  if (item.type !== 'submenu') throw new Error(`Expected submenu ${id}`)
  return item
}
function invoke(item: ContextMenuItem) {
  if (!('action' in item) || typeof item.action !== 'function') throw new Error('Expected actionable menu item')
  return item.action()
}
beforeEach(() => { setActivePinia(createPinia()); vi.useFakeTimers() })
afterEach(() => vi.useRealTimers())

describe('stream quality menu and sampling', () => {
  it('executes mode, resolution, cadence, mute and source controls through menu actions', () => {
    const quality = useStreamQuality()
    const voice = useVoiceStore()
    for (const mode of ['gaming', 'screen', 'custom']) {
      const item = requireValue(quality.menuItems().find(item => item.id === `mode-${mode}`))
      invoke(item)
      expect(quality.mode.value).toBe(mode)
    }
    const items = quality.menuItems()
    for (const item of submenu(items, 'resolution').items ?? []) invoke(item)
    expect(quality.quality.value.resolution).toBe('source')
    for (const item of submenu(items, 'fps').items ?? []) invoke(item)
    expect(quality.quality.value.fps).toBe(60)
    const mute = vi.spyOn(voice, 'toggleScreenAudioMute')
    invoke(requireValue(items.find(item => item.id === 'mute-sound')))
    expect(mute).toHaveBeenCalledOnce()
    const source = vi.fn()
    const sourceItems = quality.menuItems({ onChangeSource: source })
    const open = vi.spyOn(voice, 'openScreenShareModal')
    invoke(requireValue(sourceItems.find(item => item.id === 'stream-settings')))
    invoke(requireValue(sourceItems.find(item => item.id === 'change-source')))
    expect(open).toHaveBeenCalledOnce()
    expect(source).toHaveBeenCalledOnce()
  })
  it('samples each second, renders measured limitations and closes the sampler with the menu', async () => {
    const getStats = vi.fn(() => report(1000))
    const quality = useStreamQuality({ getStats, getCaptureSettings: () => ({ frameRate: 60 }) })
    const advanced = submenu(quality.menuItems(), 'advanced')
    advanced.onOpen?.()
    await Promise.resolve()
    expect(quality.sendStats.value).toMatchObject({ codec: 'VP8', efficientEncoder: true, limitation: 'cpu' })
    await vi.advanceTimersByTimeAsync(1000)
    expect(getStats).toHaveBeenCalledTimes(2)
    const diagnostics = submenu(quality.menuItems(), 'advanced').items ?? []
    expect(diagnostics.find(item => item.id === 'efficient-encoder')).toMatchObject({ value: 'Ja' })
    expect(diagnostics.find(item => item.id === 'capture-setting')).toMatchObject({ value: '60 fps' })
    advanced.onClose?.()
    await vi.advanceTimersByTimeAsync(2000)
    expect(getStats).toHaveBeenCalledTimes(2)
  })
  it('shows unknown stats for absent and failed browser reports', async () => {
    const quality = useStreamQuality()
    await quality.startStats()
    expect(quality.sendStats.value).toBeNull()
    quality.stopStats()
    const failed = useStreamQuality({ getStats: () => { throw new Error('closed sender') } })
    await failed.startStats()
    expect(failed.sendStats.value).toBeNull()
    failed.stopStats()
  })
  it('never publishes an in-flight sample after stop and handles false encoder efficiency', async () => {
    let finish: ((stats: MediaStat[]) => void) | undefined
    const quality = useStreamQuality({ getStats: () => new Promise<MediaStat[]>(resolve => { finish = resolve }) })
    const sampling = quality.startStats()
    quality.stopStats()
    requireValue(finish)(report())
    await sampling
    expect(quality.sendStats.value).toBeNull()
    const inefficient = useStreamQuality({ getStats: () => [{ id: 'o', type: 'outbound-rtp', kind: 'video', powerEfficientEncoder: false }] })
    await inefficient.startStats()
    const items = submenu(inefficient.menuItems(), 'advanced').items ?? []
    expect(items.find(item => item.id === 'efficient-encoder')).toMatchObject({ value: 'Nein' })
    inefficient.stopStats()
  })
  it('automatically stops the active timer when a component unmounts', async () => {
    const getStats = vi.fn(() => report())
    const wrapper = mount(defineComponent({ setup() {
      const quality = useStreamQuality({ getStats })
      void quality.startStats()
      return () => h('div')
    } }))
    await Promise.resolve()
    expect(getStats).toHaveBeenCalledOnce()
    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(2000)
    expect(getStats).toHaveBeenCalledOnce()
  })
})
